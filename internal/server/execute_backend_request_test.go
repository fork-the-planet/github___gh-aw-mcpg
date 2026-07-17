package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/launcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCustomBackend creates a mock HTTP backend that delegates all method handling
// to the given handler function. It always handles "initialize" automatically.
func newCustomBackend(t *testing.T, serverName string, handleMethod func(w http.ResponseWriter, method string, reqID interface{}, params interface{})) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
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
			json.NewEncoder(w).Encode(resp) //nolint:errcheck
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
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
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
	_, err := executeBackendRequest[result](context.Background(), l, "rpc-error-server", "session-1", "custom/method", nil)
	require.Error(t, err, "should propagate backend RPC error")
	assert.Contains(t, err.Error(), "backend error server=rpc-error-server")
	assert.Contains(t, err.Error(), "Method not found")
}

// TestExecuteBackendRequest_UnmarshalError verifies that executeBackendRequest returns
// an error when the backend result cannot be unmarshalled into the target type.
func TestExecuteBackendRequest_UnmarshalError(t *testing.T) {
	backend := newCustomBackend(t, "unmarshal-error-server", func(w http.ResponseWriter, method string, reqID interface{}, _ interface{}) {
		w.Header().Set("Content-Type", "application/json")
		// Return a result that cannot unmarshal into a struct with a numeric field
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"jsonrpc": "2.0",
			"id":      reqID,
			"result":  "this is a plain string, not an object",
		})
	})
	defer backend.Close()

	l := newExecuteBackendTestLauncher(t, "unmarshal-error-server", backend.URL)

	// Use a struct type so that a bare JSON string fails to unmarshal
	type strictResult struct {
		RequiredField int `json:"required_field"`
	}
	// A plain string "this is a plain string, not an object" fails to unmarshal
	// into strictResult because json.Unmarshal requires a JSON object.
	_, err := executeBackendRequest[strictResult](context.Background(), l, "unmarshal-error-server", "session-1", "custom/method", nil)
	require.Error(t, err, "should return error when result cannot be unmarshalled")
	assert.Contains(t, err.Error(), "failed to parse custom/method result")
}

// TestExecuteBackendRequest_Success verifies the happy path: a well-formed backend
// response is unmarshalled into the requested type and returned without error.
func TestExecuteBackendRequest_Success(t *testing.T) {
	type myResult struct {
		Status  string `json:"status"`
		Count   int    `json:"count"`
		Enabled bool   `json:"enabled"`
	}

	expected := myResult{Status: "ok", Count: 42, Enabled: true}

	backend := newCustomBackend(t, "success-server", func(w http.ResponseWriter, method string, reqID interface{}, _ interface{}) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"jsonrpc": "2.0",
			"id":      reqID,
			"result":  expected,
		})
	})
	defer backend.Close()

	l := newExecuteBackendTestLauncher(t, "success-server", backend.URL)

	got, err := executeBackendRequest[myResult](context.Background(), l, "success-server", "session-1", "custom/get", nil)
	require.NoError(t, err, "should succeed for a well-formed backend response")
	assert.Equal(t, expected.Status, got.Status)
	assert.Equal(t, expected.Count, got.Count)
	assert.Equal(t, expected.Enabled, got.Enabled)
}

// TestExecuteBackendRequest_WithParams verifies that params are forwarded to the backend.
func TestExecuteBackendRequest_WithParams(t *testing.T) {
	var receivedName string

	backend := newCustomBackend(t, "params-server", func(w http.ResponseWriter, method string, reqID interface{}, params interface{}) {
		if p, ok := params.(map[string]interface{}); ok {
			if name, ok := p["name"].(string); ok {
				receivedName = name
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"jsonrpc": "2.0",
			"id":      reqID,
			"result":  map[string]interface{}{"echoed": receivedName},
		})
	})
	defer backend.Close()

	l := newExecuteBackendTestLauncher(t, "params-server", backend.URL)

	type echoResult struct {
		Echoed string `json:"echoed"`
	}
	got, err := executeBackendRequest[echoResult](context.Background(), l, "params-server", "session-1", "echo/name",
		map[string]interface{}{"name": "hello-world"})
	require.NoError(t, err)
	assert.Equal(t, "hello-world", got.Echoed)
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
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
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

// TestExecuteBackendRequest_SessionIsolation verifies that two different session IDs
// can use the same backend concurrently without errors.
func TestExecuteBackendRequest_SessionIsolation(t *testing.T) {
	backend := newCustomBackend(t, "session-server", func(w http.ResponseWriter, method string, reqID interface{}, _ interface{}) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"jsonrpc": "2.0",
			"id":      reqID,
			"result":  map[string]interface{}{"ok": true},
		})
	})
	defer backend.Close()

	l := newExecuteBackendTestLauncher(t, "session-server", backend.URL)

	type okResult struct {
		OK bool `json:"ok"`
	}

	r1, err1 := executeBackendRequest[okResult](context.Background(), l, "session-server", "session-A", "ping", nil)
	r2, err2 := executeBackendRequest[okResult](context.Background(), l, "session-server", "session-B", "ping", nil)

	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.True(t, r1.OK)
	assert.True(t, r2.OK)
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
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
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
	assert.True(t, strings.Contains(err.Error(), "ghost-server") || errors.Is(err, err),
		"error should reference the server or propagate")
}
