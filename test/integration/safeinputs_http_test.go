package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"time"
)

// TestSafeinputsHTTPBackend tests the gateway with a spec-compliant plain-JSON-RPC backend
// that issues a Mcp-Session-Id in the initialize response and requires it on all
// subsequent requests.
//
// This validates the fix from the go-sdk module review: the gateway must NOT send a
// synthetic session ID on initialize (the server assigns it), but must correctly capture
// and forward the server-issued session ID for all subsequent requests.
func TestSafeinputsHTTPBackend(t *testing.T) {
	const serverIssuedSessionID = "safeinputs-session-42"

	// Create a mock HTTP server that simulates spec-compliant MCP server behavior:
	// - No session ID required on initialize
	// - Issues a session ID in the initialize response
	// - Requires that session ID on all subsequent requests
	var receivedHeaders []map[string]string
	var requestCount int

	safeinputsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		// Capture headers for verification
		headers := map[string]string{
			"Mcp-Session-Id": r.Header.Get("Mcp-Session-Id"),
			"Authorization":  r.Header.Get("Authorization"),
			"Content-Type":   r.Header.Get("Content-Type"),
		}
		receivedHeaders = append(receivedHeaders, headers)

		// Parse JSON-RPC request
		var rpcReq struct {
			JSONRPC string      `json:"jsonrpc"`
			ID      int         `json:"id"`
			Method  string      `json:"method"`
			Params  interface{} `json:"params"`
		}
		bodyBytes, _ := io.ReadAll(r.Body)
		json.Unmarshal(bodyBytes, &rpcReq)

		t.Logf("Request #%d: method=%s, Mcp-Session-Id=%s", requestCount, rpcReq.Method, headers["Mcp-Session-Id"])

		var response map[string]interface{}
		switch rpcReq.Method {
		case "initialize":
			// Spec-compliant: no session ID required on initialize; issue one in response.
			w.Header().Set("Mcp-Session-Id", serverIssuedSessionID)
			response = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      rpcReq.ID,
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]interface{}{},
					"serverInfo": map[string]interface{}{
						"name":    "safeinputs-server",
						"version": "1.0.0",
					},
				},
			}
		case "tools/list":
			// Require the server-issued session ID on all subsequent requests.
			if headers["Mcp-Session-Id"] != serverIssuedSessionID {
				w.WriteHeader(http.StatusBadRequest)
				response = map[string]interface{}{
					"jsonrpc": "2.0",
					"error": map[string]interface{}{
						"code":    -32600,
						"message": fmt.Sprintf("Invalid session ID: got %q, want %q", headers["Mcp-Session-Id"], serverIssuedSessionID),
					},
					"id": rpcReq.ID,
				}
				t.Logf("❌ Request #%d rejected: wrong session ID", requestCount)
				json.NewEncoder(w).Encode(response)
				return
			}
			response = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      rpcReq.ID,
				"result": map[string]interface{}{
					"tools": []map[string]interface{}{
						{
							"name":        "safe_echo",
							"description": "Safely echo input",
							"inputSchema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"message": map[string]interface{}{
										"type":        "string",
										"description": "Message to echo",
									},
								},
								"required": []string{"message"},
							},
						},
					},
				},
			}
		case "tools/call":
			response = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      rpcReq.ID,
				"result": map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": "Safe echo response",
						},
					},
				},
			}
		default:
			response = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      rpcReq.ID,
				"error": map[string]interface{}{
					"code":    -32601,
					"message": fmt.Sprintf("Method not found: %s", rpcReq.Method),
				},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer safeinputsServer.Close()

	t.Logf("Started mock safeinputs HTTP server at: %s", safeinputsServer.URL)

	// Create gateway configuration with the safeinputs HTTP backend
	configJSON := fmt.Sprintf(`{
		"mcpServers": {
			"safeinputs": {
				"type": "http",
				"url": "%s",
				"headers": {
					"Authorization": "safeinputs-secret-key"
				}
			}
		},
		"gateway": {
			"port": 3001,
			"domain": "localhost",
			"agentId": "test-gateway-key"
		}
	}`, safeinputsServer.URL)

	t.Logf("Gateway configuration:\n%s", configJSON)

	// Find the gateway binary
	binaryPath := findBinary(t)
	t.Logf("Using gateway binary: %s", binaryPath)

	// Start the gateway in routed mode with config from stdin
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath,
		"--config-stdin",
		"--listen", "127.0.0.1:3001",
		"--routed",
	)

	// Provide config via stdin
	cmd.Stdin = strings.NewReader(configJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start gateway: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	// Wait for the gateway HTTP server to be ready
	if !waitForServer(t, "http://127.0.0.1:3001/health", 20*time.Second) {
		t.Logf("STDOUT: %s", stdout.String())
		t.Logf("STDERR: %s", stderr.String())
		t.Fatal("Gateway did not start in time")
	}
	// Small delay to ensure stdout JSON is written
	time.Sleep(200 * time.Millisecond)

	// Parse the gateway output to get the actual port
	var gatewayConfig struct {
		MCPServers map[string]struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"mcpServers"`
	}

	stdoutStr := stdout.String()
	t.Logf("Gateway stdout:\n%s", stdoutStr)
	t.Logf("Gateway stderr:\n%s", stderr.String())

	if err := json.Unmarshal([]byte(stdoutStr), &gatewayConfig); err != nil {
		t.Fatalf("Failed to parse gateway output: %v\nStdout: %s", err, stdoutStr)
	}

	// Extract the gateway URL
	var gatewayURL string
	for _, server := range gatewayConfig.MCPServers {
		if server.Type == "http" {
			gatewayURL = server.URL
			break
		}
	}

	if gatewayURL == "" {
		t.Fatalf("Could not find gateway URL in output")
	}

	t.Logf("Gateway started at: %s", gatewayURL)

	stderrStr := stderr.String()

	// Verify that the gateway successfully initialized the safeinputs backend
	if strings.Contains(stderrStr, "Registered 1 tools from safeinputs") {
		t.Logf("✓ Gateway successfully initialized safeinputs backend")
	} else if strings.Contains(stderrStr, "Warning: failed to register tools from safeinputs") {
		t.Errorf("Gateway failed to register tools from safeinputs:\n%s", stderrStr)
	}

	// Verify request count and session ID handling.
	assert.NotZero(t, requestCount, "Expected at least one request to safeinputs server during initialization")
	t.Logf("✓ Received %d request(s) to safeinputs server", requestCount)

	// The gateway now tries Streamable HTTP and SSE transports before falling back to
	// plain JSON-RPC. The early probe requests (from SDK transports) may not carry a
	// Mcp-Session-Id header because the session has not been established yet.
	// What matters is that:
	//   1. Every request carries the Authorization header (custom-header injection works).
	//   2. The initialize request does NOT have a synthetic client-generated session ID
	//      (spec-compliant: the server, not the client, assigns the session ID).
	//   3. Requests after initialize use the server-issued session ID.
	initializeRequestSentSessionID := false
	serverIssuedIDUsedAfterInit := false
	for i, headers := range receivedHeaders {
		// Authorization header must be present on every request.
		if headers["Authorization"] != "safeinputs-secret-key" {
			t.Errorf("Request #%d has incorrect Authorization header: got %s, want safeinputs-secret-key",
				i+1, headers["Authorization"])
		}

		sessionID := headers["Mcp-Session-Id"]
		t.Logf("Request #%d session ID: %q", i+1, sessionID)

		// Check if this looks like a plain JSON-RPC initialize request
		// (the very first POST with a session ID is the initialize; earlier SDK probe
		// requests won't have a session ID at all).
		if sessionID != "" && sessionID != serverIssuedSessionID {
			// Any session ID that is NOT the server-issued one and is not empty was
			// synthesized by the client — that is the behaviour we're removing.
			initializeRequestSentSessionID = true
			t.Errorf("Request #%d carries a synthetic client-generated session ID %q; "+
				"only the server-issued ID %q should be used",
				i+1, sessionID, serverIssuedSessionID)
		}
		if sessionID == serverIssuedSessionID {
			serverIssuedIDUsedAfterInit = true
			t.Logf("✓ Request #%d correctly uses server-issued session ID", i+1)
		}
	}

	// Final verification
	assert.False(t, initializeRequestSentSessionID,
		"Gateway must not send a synthetic Mcp-Session-Id on initialize; the server assigns it")
	assert.True(t, serverIssuedIDUsedAfterInit,
		"Gateway must use the server-issued session ID for requests after initialize")
	t.Logf("✅ SUCCESS: Gateway correctly handles Mcp-Session-Id per MCP spec")
}
