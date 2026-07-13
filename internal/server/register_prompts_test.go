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
	"github.com/github/gh-aw-mcpg/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newStreamableBackendWithPromptsCapability creates an httptest.Server that speaks the
// streamable HTTP MCP protocol and declares prompts capability in its initialize response.
// The onPromptsList callback is invoked for each prompts/list request and receives the
// http.ResponseWriter and the JSON-RPC request-ID so the caller can write the desired
// response.
func newStreamableBackendWithPromptsCapability(
	t *testing.T,
	onPromptsList func(w http.ResponseWriter, reqID interface{}),
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		method, _ := req["method"].(string)
		switch method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "test-prompts-session")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities": map[string]interface{}{
						"prompts": map[string]interface{}{},
					},
					"serverInfo": map[string]interface{}{
						"name":    "prompts-capable-backend",
						"version": "1.0.0",
					},
				},
			})

		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)

		case "prompts/list":
			onPromptsList(w, req["id"])

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// connectStreamableBackend establishes a launcher connection to the given server
// and verifies that the resulting connection declares prompts capability.
func connectStreamableBackend(t *testing.T, srv *httptest.Server) *mcp.Connection {
	t.Helper()

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"prompts-server": {Type: "http", URL: srv.URL},
		},
	}

	l := launcher.New(context.Background(), cfg)
	t.Cleanup(func() { l.Close() })

	conn, err := launcher.GetOrLaunch(l, "prompts-server")
	require.NoError(t, err, "GetOrLaunch should succeed for streamable backend")
	require.NotNil(t, conn, "connection should not be nil")
	require.True(t, conn.BackendHasPromptsCapability(),
		"connection to a backend that returned capabilities.prompts should report prompts capability")
	return conn
}

// minimalPromptsTestServer constructs just enough of a UnifiedServer for tests that call
// registerPromptsFromBackend directly. Only the sdk.Server field is required during
// prompt registration; the launcher and other fields are only needed when the registered
// prompt handler is actually invoked.
func minimalPromptsTestServer(t *testing.T) *UnifiedServer {
	t.Helper()
	return &UnifiedServer{
		server: newSDKServer("test-prompts", logUnified),
	}
}

// TestRegisterPromptsFromBackend_NoCapability verifies that a backend without prompts
// capability causes registerPromptsFromBackend to return immediately without error.
// This exercises the !BackendHasPromptsCapability() early-return branch.
func TestRegisterPromptsFromBackend_NoCapability(t *testing.T) {
	// Plain-JSON-RPC backend: no Mcp-Session-Id header → no SDK session → BackendHasPromptsCapability() == false
	plainBackend := newMockBackend(t, "no-prompts-backend", []string{"some_tool"})
	defer plainBackend.Close()

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"no-prompts-server": {Type: "http", URL: plainBackend.URL},
		},
	}
	l := launcher.New(context.Background(), cfg)
	defer l.Close()

	conn, err := launcher.GetOrLaunch(l, "no-prompts-server")
	require.NoError(t, err)
	assert.False(t, conn.BackendHasPromptsCapability(),
		"plain-JSON-RPC connection should not declare prompts capability")

	us := minimalPromptsTestServer(t)
	err = us.registerPromptsFromBackend(context.Background(), "no-prompts-server", conn)
	assert.NoError(t, err, "should return nil when backend has no prompts capability")
}

// TestRegisterPromptsFromBackend_RequestError verifies graceful handling when the backend
// returns an HTTP error for prompts/list. The function should swallow the error (non-fatal)
// and return nil.
func TestRegisterPromptsFromBackend_RequestError(t *testing.T) {
	promptsListCalled := make(chan struct{}, 10)
	t.Cleanup(func() {
		assert.Len(t, promptsListCalled, 1, "expected prompts/list to be called exactly once")
	})

	srv := newStreamableBackendWithPromptsCapability(t, func(w http.ResponseWriter, _ interface{}) {
		promptsListCalled <- struct{}{}
		// Return HTTP 500 → SDK treats this as a request-level failure
		w.WriteHeader(http.StatusInternalServerError)
	})

	conn := connectStreamableBackend(t, srv)
	us := minimalPromptsTestServer(t)

	err := us.registerPromptsFromBackend(context.Background(), "prompts-server", conn)
	assert.NoError(t, err, "request error should be treated as a graceful skip, not a fatal error")
}

// TestRegisterPromptsFromBackend_EmptyPromptsList verifies that a backend with prompts
// capability that returns an empty prompts/list causes registerPromptsFromBackend to
// return nil without registering anything.
func TestRegisterPromptsFromBackend_EmptyPromptsList(t *testing.T) {
	promptsListCalled := make(chan struct{}, 10)
	t.Cleanup(func() {
		assert.Len(t, promptsListCalled, 1, "expected prompts/list to be called exactly once")
	})

	srv := newStreamableBackendWithPromptsCapability(t, func(w http.ResponseWriter, reqID interface{}) {
		promptsListCalled <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result": map[string]interface{}{
				"prompts": []interface{}{},
			},
		})
	})
	defer srv.Close()

	conn := connectStreamableBackend(t, srv)
	us := minimalPromptsTestServer(t)

	err := us.registerPromptsFromBackend(context.Background(), "prompts-server", conn)
	assert.NoError(t, err, "empty prompts list should return nil without registering anything")
}

// TestRegisterPromptsFromBackend_RegistersPrompts verifies that prompts returned by the
// backend are registered on the unified server. The function should return nil and the
// SDK server should have the prompt added.
func TestRegisterPromptsFromBackend_RegistersPrompts(t *testing.T) {
	promptsListCalled := make(chan struct{}, 10)
	t.Cleanup(func() {
		assert.Len(t, promptsListCalled, 1, "expected prompts/list to be called exactly once")
	})

	srv := newStreamableBackendWithPromptsCapability(t, func(w http.ResponseWriter, reqID interface{}) {
		promptsListCalled <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result": map[string]interface{}{
				"prompts": []map[string]interface{}{
					{
						"name":        "summarize",
						"description": "Summarizes the given text",
					},
					{
						"name":        "translate",
						"description": "Translates text to another language",
					},
				},
			},
		})
	})
	defer srv.Close()

	conn := connectStreamableBackend(t, srv)
	us := minimalPromptsTestServer(t)

	err := us.registerPromptsFromBackend(context.Background(), "prompts-server", conn)
	assert.NoError(t, err, "should return nil when prompts are successfully registered")
}
