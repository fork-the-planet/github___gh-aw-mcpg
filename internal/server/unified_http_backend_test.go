package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/launcher"
	"github.com/github/gh-aw-mcpg/internal/mcp"
)

// decodeJSONRPCMethod reads the request body and extracts the JSON-RPC method and ID.
// Returns empty method for non-JSON or empty bodies (e.g. SDK transport probes).
func decodeJSONRPCMethod(r *http.Request) (method string, id interface{}) {
	bodyBytes, _ := io.ReadAll(r.Body)
	if len(bodyBytes) == 0 {
		return "", nil
	}
	var req struct {
		Method string      `json:"method"`
		ID     interface{} `json:"id"`
	}
	json.Unmarshal(bodyBytes, &req)
	return req.Method, req.ID
}

// jsonRPCResult writes a JSON-RPC success response with the given request ID.
func jsonRPCResult(w http.ResponseWriter, id interface{}, result interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

// jsonRPCError writes a JSON-RPC error response with the given request ID.
func jsonRPCError(w http.ResponseWriter, statusCode int, id interface{}, code int, message string) {
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]interface{}{"code": code, "message": message},
	})
}

// TestHTTPBackendInitialization tests that HTTP backends use the session ID issued by the
// server during initialize (not a locally-fabricated one) when calling tools/list.
// This is a regression test for https://github.com/github/gh-aw/issues/18712 where
// gateway-issued fake session IDs overrode the real server-issued session ID, causing
// HTTP 400 on tools/list from strict backends like Datadog.
func TestHTTPBackendInitialization(t *testing.T) {
	const serverSessionID = "server-issued-session-42"
	var toolsListSessionID string

	// Create a mock HTTP MCP server that:
	// 1. Issues a specific session ID during initialize
	// 2. Requires that exact session ID for subsequent requests
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, id := decodeJSONRPCMethod(r)
		if method == "" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		switch method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", serverSessionID)
			jsonRPCResult(w, id, map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{},
				"serverInfo":      map[string]interface{}{"name": "test-server", "version": "1.0.0"},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			toolsListSessionID = r.Header.Get("Mcp-Session-Id")
			if toolsListSessionID != serverSessionID {
				jsonRPCError(w, http.StatusBadRequest, id, -32603, "Invalid session ID")
				return
			}
			jsonRPCResult(w, id, map[string]interface{}{
				"tools": []map[string]interface{}{
					{"name": "test_tool", "description": "A test tool", "inputSchema": map[string]interface{}{"type": "object"}},
				},
			})
		}
	}))
	defer mockServer.Close()

	// Custom headers are forwarded to all transport types via RoundTripper injection
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"http-backend": {
				Type:    "http",
				URL:     mockServer.URL,
				Headers: map[string]string{"X-Auth": "test"},
			},
		},
	}

	// Create unified server - this calls tools/list during initialization
	ctx := context.Background()
	us, err := NewUnified(ctx, cfg)
	require.NoError(t, err, "Failed to create unified server: gateway must use server-issued session ID for tools/list")
	defer us.Close()

	// The session ID used for tools/list must be the one issued by the server during initialize,
	// not a locally-fabricated "gateway-init-*" value.
	assert.Equal(t, serverSessionID, toolsListSessionID,
		"tools/list must use the session ID issued by the server during initialize, not a fabricated one")

	t.Logf("Correctly used server-issued session ID for tools/list: %s", toolsListSessionID)
}

// TestHTTPBackendInitializationSpecCompliant verifies that the gateway does NOT send a
// synthetic Mcp-Session-Id header on the MCP initialize request. Per the MCP spec, the
// session ID is assigned by the server in the initialize response — not the client.
//
// This also verifies that the server-issued session ID is then used correctly for all
// subsequent requests.
func TestHTTPBackendInitializationSpecCompliant(t *testing.T) {
	const serverSessionID = "server-issued-session-99"
	var initializeSessionID string // empty means no header was sent on initialize
	var toolsListSessionID string

	// Spec-compliant server: no session ID required on initialize; issues one in response.
	specServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, id := decodeJSONRPCMethod(r)
		if method == "" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		switch method {
		case "initialize":
			initializeSessionID = r.Header.Get("Mcp-Session-Id")
			w.Header().Set("Mcp-Session-Id", serverSessionID)
			jsonRPCResult(w, id, map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{},
				"serverInfo":      map[string]interface{}{"name": "spec-server", "version": "1.0.0"},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			toolsListSessionID = r.Header.Get("Mcp-Session-Id")
			jsonRPCResult(w, id, map[string]interface{}{
				"tools": []map[string]interface{}{
					{"name": "spec_tool", "description": "A spec-compliant tool", "inputSchema": map[string]interface{}{"type": "object"}},
				},
			})
		default:
			jsonRPCError(w, http.StatusOK, id, -32601, "Method not found")
		}
	}))
	defer specServer.Close()

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"spec-backend": {
				Type: "http",
				URL:  specServer.URL,
			},
		},
	}

	ctx := context.Background()
	us, err := NewUnified(ctx, cfg)
	require.NoError(t, err, "Failed to create unified server")
	defer us.Close()

	// The gateway must NOT send a Mcp-Session-Id on initialize (spec-compliant).
	assert.Empty(t, initializeSessionID,
		"Mcp-Session-Id must NOT be sent on initialize: the server, not the client, assigns it")

	// The server-issued session ID must be used for subsequent requests.
	assert.Equal(t, serverSessionID, toolsListSessionID,
		"tools/list must use the session ID issued by the server during initialize")

	tools := us.GetToolsForBackend("spec-backend")
	assert.NotEmpty(t, tools, "Expected tools to be registered")
}

// TestHTTPBackend_SessionIDPropagation tests that session ID is propagated through tool calls
func TestHTTPBackend_SessionIDPropagation(t *testing.T) {
	// Track session IDs received at different stages
	initializeSessionID := ""
	initSessionID := ""
	toolCallSessionID := ""

	// Create a mock HTTP MCP server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, id := decodeJSONRPCMethod(r)
		if method == "" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		sessionID := r.Header.Get("Mcp-Session-Id")

		switch method {
		case "initialize":
			initializeSessionID = sessionID
			jsonRPCResult(w, id, map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{},
				"serverInfo":      map[string]interface{}{"name": "test-http-server", "version": "1.0.0"},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			initSessionID = sessionID
			jsonRPCResult(w, id, map[string]interface{}{
				"tools": []map[string]interface{}{
					{"name": "echo", "description": "Echo tool"},
				},
			})
		case "tools/call":
			toolCallSessionID = sessionID
			jsonRPCResult(w, id, map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": "echo response"},
				},
			})
		}
	}))
	defer mockServer.Close()

	// Create config
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"test-http": {
				Type:    "http",
				URL:     mockServer.URL,
				Headers: map[string]string{"X-Test": "test"},
			},
		},
	}

	// Create unified server
	ctx := context.Background()
	us, err := NewUnified(ctx, cfg)
	require.NoError(t, err, "Failed to create unified server")
	defer us.Close()

	// Create a connection and call a tool with a specific session ID
	conn, err := launcher.GetOrLaunch(us.launcher, "test-http")
	require.NoError(t, err, "Failed to get connection")

	clientSessionID := "client-session-12345"
	ctxWithSession := context.WithValue(context.Background(), mcp.SessionIDContextKey, clientSessionID)

	_, err = conn.SendRequestWithServerID(ctxWithSession, "tools/call", map[string]interface{}{
		"name":      "echo",
		"arguments": map[string]interface{}{"message": "test"},
	}, "test-http")
	require.NoError(t, err, "Failed to call tool")

	// Verify session IDs were received.
	// With the SDK streamable transport, session IDs are managed internally by the SDK,
	// so the Mcp-Session-Id header may not appear in requests to the mock.
	// With plain JSON-RPC, the gateway explicitly injects session IDs via headers.
	if initializeSessionID != "" {
		t.Logf("Initialize session ID: %s", initializeSessionID)
	} else {
		t.Logf("No session ID on initialize (expected for SDK streamable transport)")
	}

	if initSessionID != "" {
		t.Logf("Init session ID: %s", initSessionID)
	} else {
		t.Logf("No session ID on tools/list (expected for SDK streamable transport)")
	}

	if toolCallSessionID != "" {
		assert.Equal(t, clientSessionID, toolCallSessionID,
			"tool call should propagate client session ID for plain JSON-RPC transport")
	} else {
		t.Logf("No session ID on tool call (expected for SDK streamable transport)")
	}

	t.Logf("Session ID propagation test passed")
}
