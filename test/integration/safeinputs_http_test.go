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

// TestSafeinputsHTTPBackend tests the gateway with a safeinputs-like HTTP backend
// that strictly enforces the Mcp-Session-Id header requirement.
// This simulates the real-world scenario described in the issue.
func TestSafeinputsHTTPBackend(t *testing.T) {
	// Create a mock HTTP server that simulates safeinputs MCP server behavior
	// It requires Mcp-Session-Id header on all requests
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

		// Strictly enforce Mcp-Session-Id header requirement
		if headers["Mcp-Session-Id"] == "" {
			w.WriteHeader(http.StatusBadRequest)
			response := map[string]interface{}{
				"jsonrpc": "2.0",
				"error": map[string]interface{}{
					"code":    -32600,
					"message": "Invalid Request: Missing Mcp-Session-Id header",
				},
				"id": rpcReq.ID,
			}
			json.NewEncoder(w).Encode(response)
			t.Logf("❌ Request #%d rejected: Missing Mcp-Session-Id header", requestCount)
			return
		}

		t.Logf("✓ Request #%d accepted with session ID: %s", requestCount, headers["Mcp-Session-Id"])

		// Return appropriate response based on method
		var response map[string]interface{}
		switch rpcReq.Method {
		case "initialize":
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
			"apiKey": "test-gateway-key"
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

	// Check stderr for any session ID related messages
	stderrStr := stderr.String()
	if strings.Contains(stderrStr, "Missing Mcp-Session-Id header") {
		t.Errorf("Gateway initialization failed with missing session ID error:\n%s", stderrStr)
	}

	// Verify that the gateway successfully initialized
	if strings.Contains(stderrStr, "Registered 1 tools from safeinputs") {
		t.Logf("✓ Gateway successfully initialized safeinputs backend")
	} else if strings.Contains(stderrStr, "Warning: failed to register tools from safeinputs") {
		t.Errorf("Gateway failed to register tools from safeinputs:\n%s", stderrStr)
	}

	// Verify request count and session IDs
	assert.False(t, requestCount == 0, "Expected at least one request to safeinputs server during initialization")
	t.Logf("✓ Received %d request(s) to safeinputs server", requestCount)

	// Verify all requests had session IDs
	for i, headers := range receivedHeaders {
		if headers["Mcp-Session-Id"] == "" {
			t.Errorf("Request #%d missing Mcp-Session-Id header", i+1)
		} else {
			t.Logf("Request #%d session ID: %s", i+1, headers["Mcp-Session-Id"])

			// Verify the session ID follows the expected pattern for initialization
			if strings.HasPrefix(headers["Mcp-Session-Id"], "awmg-init-") ||
				strings.HasPrefix(headers["Mcp-Session-Id"], "gateway-init-") {
				t.Logf("✓ Request #%d has correct gateway initialization session ID pattern", i+1)
			}
		}

		// Verify Authorization header was passed through
		if headers["Authorization"] != "safeinputs-secret-key" {
			t.Errorf("Request #%d has incorrect Authorization header: got %s, want safeinputs-secret-key",
				i+1, headers["Authorization"])
		}
	}

	// Final verification
	if len(receivedHeaders) > 0 && receivedHeaders[0]["Mcp-Session-Id"] != "" {
		t.Logf("✅ SUCCESS: Gateway correctly sends Mcp-Session-Id header to safeinputs HTTP backend")
		t.Logf("   Session ID pattern: %s", receivedHeaders[0]["Mcp-Session-Id"])
	}
}
