package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/launcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeJSONResponse(t *testing.T, w http.ResponseWriter, payload interface{}) {
	t.Helper()
	assert.NoError(t, json.NewEncoder(w).Encode(payload))
}

// newCustomBackend creates a mock HTTP backend that delegates all method handling
// to the given handler function. It always handles "initialize" automatically.
func newCustomBackend(t *testing.T, serverName string, handleMethod func(w http.ResponseWriter, method string, reqID interface{}, params interface{})) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusInternalServerError)
			return
		}
		if len(body) == 0 {
			http.Error(w, "empty request body", http.StatusBadRequest)
			return
		}

		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON-RPC request", http.StatusBadRequest)
			return
		}
		method, _ := req["method"].(string)
		if method == "initialize" {
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]interface{}{},
					"serverInfo":      map[string]interface{}{"name": serverName, "version": "1.0"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			// The SDK stores the negotiated session from initialize responses and
			// reuses it on later requests, so tests need to provide one here.
			w.Header().Set("Mcp-Session-Id", "test-session-abc")
			writeJSONResponse(t, w, resp)
			return
		}
		if method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		handleMethod(w, method, req["id"], req["params"])
	}))
}

// newExecuteBackendTestLauncher creates a launcher connected to the given backend.
func newExecuteBackendTestLauncher(t *testing.T, serverID, backendURL string) *launcher.Launcher {
	t.Helper()
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			serverID: {Type: "http", URL: backendURL},
		},
	}
	l := launcher.New(context.Background(), cfg)
	t.Cleanup(func() { l.Close() })
	return l
}

// TestExecuteBackendRequest_LauncherError verifies that executeBackendRequest returns
// an error when the launcher cannot connect to the requested server.
func TestExecuteBackendRequest_LauncherError(t *testing.T) {
	// Create a launcher with no servers registered — any serverID will fail
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{},
	}
	l := launcher.New(context.Background(), cfg)
	defer l.Close()

	type result struct{ Value string }
	_, err := executeBackendRequest[result](context.Background(), l, "nonexistent-server", "session-1", "tools/list", nil)
	require.Error(t, err, "should return error when server does not exist")
	assert.Contains(t, err.Error(), "failed to connect to backend nonexistent-server")
}

// TestExecuteBackendRequest_BackendRPCError verifies that executeBackendRequest returns
// an error when the backend returns a JSON-RPC error object.
func TestExecuteBackendRequest_BackendRPCError(t *testing.T) {
	backend := newCustomBackend(t, "rpc-error-server", func(w http.ResponseWriter, method string, reqID interface{}, _ interface{}) {
		w.Header().Set("Content-Type", "application/json")
		writeJSONResponse(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"error": map[string]interface{}{
				"code":    -32601,
				"message": "Method not found",
			},
		})
	})
	defer backend.Close()

	l := newExecuteBackendTestLauncher(t, "rpc-error-server", backend.URL)

	type result struct{ Value string }
	_, err := executeBackendRequest[result](context.Background(), l, "rpc-error-server", "session-1", "tools/list", nil)
	require.Error(t, err, "should propagate backend RPC error")
	assert.Contains(t, err.Error(), "Method not found")
}

// TestExecuteBackendRequest_TransportError verifies that executeBackendRequest
// returns transport errors from SendRequestWithServerID directly.
func TestExecuteBackendRequest_TransportError(t *testing.T) {
	backend := newCustomBackend(t, "transport-error-server", func(w http.ResponseWriter, method string, reqID interface{}, _ interface{}) {
		w.Header().Set("Content-Type", "application/json")
		writeJSONResponse(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result":  map[string]interface{}{"ok": true},
		})
	})
	defer backend.Close()

	l := newExecuteBackendTestLauncher(t, "transport-error-server", backend.URL)

	type result struct{ OK bool }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := executeBackendRequest[result](ctx, l, "transport-error-server", "session-1", "tools/list", nil)
	require.Error(t, err, "should return transport error for cancelled context")
	assert.ErrorIs(t, err, context.Canceled)
}

// TestExecuteBackendRequest_UnmarshalError verifies that executeBackendRequest returns
// an error when the backend result cannot be unmarshalled into the target type.
func TestExecuteBackendRequest_UnmarshalError(t *testing.T) {
	backend := newCustomBackend(t, "unmarshal-error-server", func(w http.ResponseWriter, method string, reqID interface{}, _ interface{}) {
		w.Header().Set("Content-Type", "application/json")
		// Return a valid JSON-RPC tools/call response whose result shape is
		// incompatible with strictResult so executeBackendRequest's unmarshal step fails.
		writeJSONResponse(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{{"type": "text", "text": "value"}},
			},
		})
	})
	defer backend.Close()

	l := newExecuteBackendTestLauncher(t, "unmarshal-error-server", backend.URL)

	type strictResult string
	_, err := executeBackendRequest[strictResult](context.Background(), l, "unmarshal-error-server", "session-1", "tools/call",
		map[string]interface{}{"name": "any_tool", "arguments": map[string]interface{}{}})
	require.Error(t, err, "should return error when result cannot be unmarshalled")
	assert.Contains(t, err.Error(), "failed to parse tools/call result")
}

// TestExecuteBackendRequest_Success verifies the happy path: a well-formed backend
// response is unmarshalled into the requested type and returned without error.
func TestExecuteBackendRequest_Success(t *testing.T) {
	type myResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}

	backend := newCustomBackend(t, "success-server", func(w http.ResponseWriter, method string, reqID interface{}, _ interface{}) {
		w.Header().Set("Content-Type", "application/json")
		writeJSONResponse(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result": map[string]interface{}{
				"tools": []map[string]interface{}{{"name": "demo_tool"}},
			},
		})
	})
	defer backend.Close()

	l := newExecuteBackendTestLauncher(t, "success-server", backend.URL)

	got, err := executeBackendRequest[myResult](context.Background(), l, "success-server", "session-1", "tools/list", nil)
	require.NoError(t, err, "should succeed for a well-formed backend response")
	require.Len(t, got.Tools, 1)
	assert.Equal(t, "demo_tool", got.Tools[0].Name)
}

// TestExecuteBackendRequest_WithParams verifies that params are forwarded to the backend.
func TestExecuteBackendRequest_WithParams(t *testing.T) {
	var receivedName string

	backend := newCustomBackend(t, "params-server", func(w http.ResponseWriter, method string, reqID interface{}, params interface{}) {
		if p, ok := params.(map[string]interface{}); ok {
			if args, ok := p["arguments"].(map[string]interface{}); ok {
				if name, ok := args["name"].(string); ok {
					receivedName = name
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		writeJSONResponse(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{{"type": "text", "text": receivedName}},
			},
		})
	})
	defer backend.Close()

	l := newExecuteBackendTestLauncher(t, "params-server", backend.URL)

	type toolCallResult struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	got, err := executeBackendRequest[toolCallResult](context.Background(), l, "params-server", "session-1", "tools/call",
		map[string]interface{}{"name": "echo", "arguments": map[string]interface{}{"name": "hello-world"}})
	require.NoError(t, err)
	require.Len(t, got.Content, 1)
	assert.Equal(t, "hello-world", got.Content[0].Text)
	assert.Equal(t, "hello-world", receivedName, "params should be forwarded to backend")
}

// TestExecuteBackendRequest_InterfaceType verifies that executeBackendRequest works
// with interface{} as the type parameter (as used by executeBackendToolCall).
func TestExecuteBackendRequest_InterfaceType(t *testing.T) {
	payload := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "result text"},
		},
	}

	backend := newCustomBackend(t, "interface-server", func(w http.ResponseWriter, method string, reqID interface{}, _ interface{}) {
		w.Header().Set("Content-Type", "application/json")
		writeJSONResponse(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result":  payload,
		})
	})
	defer backend.Close()

	l := newExecuteBackendTestLauncher(t, "interface-server", backend.URL)

	got, err := executeBackendRequest[interface{}](context.Background(), l, "interface-server", "session-1", "tools/call",
		map[string]interface{}{"name": "my_tool", "arguments": map[string]interface{}{}})
	require.NoError(t, err, "interface{} type parameter should work")
	assert.NotNil(t, got)
}

// TestExecuteBackendRequest_SessionIsolation verifies that multiple sessions can
// use the same backend successfully.
func TestExecuteBackendRequest_SessionIsolation(t *testing.T) {
	backend := newCustomBackend(t, "session-server", func(w http.ResponseWriter, method string, reqID interface{}, _ interface{}) {
		w.Header().Set("Content-Type", "application/json")
		writeJSONResponse(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result": map[string]interface{}{
				"tools": []map[string]interface{}{{"name": "ping"}},
			},
		})
	})
	defer backend.Close()

	l := newExecuteBackendTestLauncher(t, "session-server", backend.URL)

	type toolsResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}

	r1, err1 := executeBackendRequest[toolsResult](context.Background(), l, "session-server", "session-A", "tools/list", nil)
	r2, err2 := executeBackendRequest[toolsResult](context.Background(), l, "session-server", "session-B", "tools/list", nil)

	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Len(t, r1.Tools, 1)
	assert.Len(t, r2.Tools, 1)
}

// TestExecuteBackendToolCall_DelegatesToExecuteBackendRequest verifies that
// executeBackendToolCall correctly wraps executeBackendRequest with "tools/call" method.
func TestExecuteBackendToolCall_DelegatesToExecuteBackendRequest(t *testing.T) {
	var receivedMethod string
	var receivedToolName string

	backend := newCustomBackend(t, "toolcall-server", func(w http.ResponseWriter, method string, reqID interface{}, params interface{}) {
		receivedMethod = method
		if p, ok := params.(map[string]interface{}); ok {
			if name, ok := p["name"].(string); ok {
				receivedToolName = name
			}
		}
		w.Header().Set("Content-Type", "application/json")
		writeJSONResponse(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result": map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "tool output"},
				},
			},
		})
	})
	defer backend.Close()

	l := newExecuteBackendTestLauncher(t, "toolcall-server", backend.URL)

	got, err := executeBackendToolCall(context.Background(), l, "toolcall-server", "session-1", "my_tool",
		map[string]interface{}{"param": "value"})
	require.NoError(t, err, "executeBackendToolCall should succeed")
	assert.NotNil(t, got)
	assert.Equal(t, "tools/call", receivedMethod, "method should be tools/call")
	assert.Equal(t, "my_tool", receivedToolName, "tool name should be forwarded")
}

// TestExecuteBackendToolCall_PropagatesLauncherError verifies that launch errors from
// executeBackendRequest bubble up through executeBackendToolCall.
func TestExecuteBackendToolCall_PropagatesLauncherError(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{},
	}
	l := launcher.New(context.Background(), cfg)
	defer l.Close()

	_, err := executeBackendToolCall(context.Background(), l, "ghost-server", "session-1", "any_tool", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, launcher.ErrServerNotFound)
}
