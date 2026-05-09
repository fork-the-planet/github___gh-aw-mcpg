package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStreamableHandler simulates a subset of the SDK's StreamableHTTPHandler behavior:
//   - POST initialize (no session ID) → 200 OK + Mcp-Session-Id header
//   - POST notifications/initialized (with session ID) → 202 Accepted
//   - POST tools/call (with valid session ID) → 200 OK with tool result
//   - POST tools/call (no session ID or mismatched) → 200 OK with
//     "invalid during session initialization" JSON-RPC error
type mockStreamableHandler struct {
	sessions map[string]bool // tracks established sessions
}

func newMockStreamableHandler() *mockStreamableHandler {
	return &mockStreamableHandler{sessions: make(map[string]bool)}
}

func (h *mockStreamableHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := peekRequestBody(r)

	var rpcReq struct {
		ID     interface{} `json:"id"`
		Method string      `json:"method"`
	}
	_ = json.Unmarshal(bodyBytes, &rpcReq)

	sessionID := r.Header.Get("Mcp-Session-Id")

	switch rpcReq.Method {
	case "initialize":
		// Return a new session ID
		newSessionID := "test-session-abc123"
		w.Header().Set("Mcp-Session-Id", newSessionID)
		h.sessions[newSessionID] = false // not yet initialized
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      rpcReq.ID,
			"result": map[string]interface{}{
				"protocolVersion": "2025-11-25",
				"capabilities":    map[string]interface{}{},
				"serverInfo":      map[string]interface{}{"name": "mock", "version": "1.0"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)

	case "notifications/initialized":
		if _, ok := h.sessions[sessionID]; ok {
			h.sessions[sessionID] = true
		}
		w.WriteHeader(http.StatusAccepted)

	case "tools/call":
		initialized, exists := h.sessions[sessionID]
		if sessionID == "" || !exists || !initialized {
			// Return the SDK-style error
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      rpcReq.ID,
				"error": map[string]interface{}{
					"code":    -32600,
					"message": `method "tools/call" is invalid during session initialization`,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		// Success
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      rpcReq.ID,
			"result": map[string]interface{}{
				"content": []interface{}{},
				"isError": false,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)

	default:
		http.Error(w, "unsupported method", http.StatusBadRequest)
	}
}

// TestWrapWithSessionAutoInit_PassthroughNonPOST verifies that non-POST requests are
// forwarded unchanged.
func TestWrapWithSessionAutoInit_PassthroughNonPOST(t *testing.T) {
	mock := newMockStreamableHandler()
	handler := WrapWithSessionAutoInit(mock)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "test-api-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// The mock returns 400 for unsupported methods; the middleware should not intercept.
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestWrapWithSessionAutoInit_PassthroughWithSessionID verifies that requests that
// already have an Mcp-Session-Id are forwarded unchanged.
func TestWrapWithSessionAutoInit_PassthroughWithSessionID(t *testing.T) {
	mock := newMockStreamableHandler()
	// Pre-register and initialize a session
	mock.sessions["existing-session"] = true
	handler := WrapWithSessionAutoInit(mock)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"test","arguments":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "test-api-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", "existing-session")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Nil(t, resp["error"], "tools/call with valid session should succeed")
}

// TestWrapWithSessionAutoInit_PassthroughNonToolsCall verifies that POST requests with
// methods other than tools/call are forwarded unchanged even without a session.
func TestWrapWithSessionAutoInit_PassthroughNonToolsCall(t *testing.T) {
	mock := newMockStreamableHandler()
	handler := WrapWithSessionAutoInit(mock)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "test-api-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// The mock returns a session ID for initialize; middleware should not interfere.
	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("Mcp-Session-Id"))
}

// TestWrapWithSessionAutoInit_AutoInitForToolsCall verifies the Gemini compatibility fix:
// a tools/call POST without Mcp-Session-Id triggers automatic session initialization
// before the request is forwarded.
func TestWrapWithSessionAutoInit_AutoInitForToolsCall(t *testing.T) {
	mock := newMockStreamableHandler()
	handler := WrapWithSessionAutoInit(mock)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"test","arguments":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "test-api-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	// No Mcp-Session-Id — simulates Gemini CLI v0.37.x behaviour
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Nil(t, resp["error"], "tools/call should succeed after auto-init")
	assert.NotNil(t, resp["result"], "tools/call should return a result")
}

// TestWrapWithSessionAutoInit_AutoInitPreservesAuthHeader verifies that the auto-init
// requests copy the Authorization header from the original request.
func TestWrapWithSessionAutoInit_AutoInitPreservesAuthHeader(t *testing.T) {
	var initAuthHeader string
	handlerFunc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := peekRequestBody(r)
		var rpcReq struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(bodyBytes, &rpcReq)

		if rpcReq.Method == "initialize" {
			initAuthHeader = r.Header.Get("Authorization")
			w.Header().Set("Mcp-Session-Id", "captured-session")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"mock","version":"1.0"}}}`))
			return
		}
		if rpcReq.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		// tools/call succeeds once initialized
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":false}}`))
	})

	handler := WrapWithSessionAutoInit(handlerFunc)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"test","arguments":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "my-secret-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, "my-secret-key", initAuthHeader,
		"auto-init initialize request should carry the original Authorization header")
}

// TestWrapWithSessionAutoInit_FallsBackOnAutoInitFailure verifies that if auto-init
// fails (e.g. initialize returns no session ID), the original request is forwarded
// unchanged so the SDK can return its normal error.
func TestWrapWithSessionAutoInit_FallsBackOnAutoInitFailure(t *testing.T) {
	// This handler never returns a session ID for initialize.
	handlerFunc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := peekRequestBody(r)
		var rpcReq struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(bodyBytes, &rpcReq)

		if rpcReq.Method == "initialize" {
			// No Mcp-Session-Id header — auto-init will fail.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{}}}`))
			return
		}
		// Any other method (the fallback tools/call) returns the SDK error.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"method \"tools/call\" is invalid during session initialization"}}`))
	})

	handler := WrapWithSessionAutoInit(handlerFunc)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"test","arguments":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "test-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should fall through to the original handler; SDK returns its usual error.
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "session initialization",
		"fallback response should contain the SDK error message")
}

// TestPerformSessionAutoInit_MissingSessionID verifies that performSessionAutoInit
// returns an error when the initialize response does not include an Mcp-Session-Id
// header, regardless of the HTTP status code.
func TestPerformSessionAutoInit_MissingSessionID(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := peekRequestBody(r)
		var rpcReq struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(bodyBytes, &rpcReq)
		if rpcReq.Method == "initialize" {
			// No Mcp-Session-Id header: auto-init must fail.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{}}}`))
			return
		}
		http.Error(w, "unexpected method", http.StatusBadRequest)
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "test-api-key")
	_, err := performSessionAutoInit(req, handler)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing Mcp-Session-Id")
}

// TestPerformSessionAutoInit_NonOKStatusWithSessionID verifies that performSessionAutoInit
// returns an error when the initialize response returns a non-200 status code even if
// the Mcp-Session-Id header is present. The non-200 status check comes after the
// session ID check in the implementation.
func TestPerformSessionAutoInit_NonOKStatusWithSessionID(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := peekRequestBody(r)
		var rpcReq struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(bodyBytes, &rpcReq)
		if rpcReq.Method == "initialize" {
			// Set the session ID header BUT return a non-200 status to exercise the
			// "initialize returned unexpected status" error branch.
			w.Header().Set("Mcp-Session-Id", "session-from-error-response")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"internal error"}}`))
			return
		}
		http.Error(w, "unexpected method", http.StatusBadRequest)
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "test-api-key")
	_, err := performSessionAutoInit(req, handler)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status 500")
}

// TestPerformSessionAutoInit_Success verifies the happy path: performSessionAutoInit
// returns the session ID established by the backend and sends both initialize and
// notifications/initialized requests.
func TestPerformSessionAutoInit_Success(t *testing.T) {
	var receivedMethods []string
	var receivedSessionIDs []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := peekRequestBody(r)
		var rpcReq struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(bodyBytes, &rpcReq)
		receivedMethods = append(receivedMethods, rpcReq.Method)
		receivedSessionIDs = append(receivedSessionIDs, r.Header.Get("Mcp-Session-Id"))

		switch rpcReq.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "happy-path-session")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		default:
			http.Error(w, "unexpected method", http.StatusBadRequest)
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	sessionID, err := performSessionAutoInit(req, handler)

	require.NoError(t, err)
	assert.Equal(t, "happy-path-session", sessionID)

	// Both initialize and notifications/initialized must have been sent.
	require.Equal(t, []string{"initialize", "notifications/initialized"}, receivedMethods)

	// The notifications/initialized request must carry the established session ID.
	require.Len(t, receivedSessionIDs, 2)
	assert.Equal(t, "happy-path-session", receivedSessionIDs[1])
}

// TestPerformSessionAutoInit_AuthHeaderCopied verifies that performSessionAutoInit
// copies the Authorization header from the original request into the auto-init
// initialize request so that authentication is preserved.
func TestPerformSessionAutoInit_AuthHeaderCopied(t *testing.T) {
	var capturedInitAuth string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := peekRequestBody(r)
		var rpcReq struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(bodyBytes, &rpcReq)

		switch rpcReq.Method {
		case "initialize":
			capturedInitAuth = r.Header.Get("Authorization")
			w.Header().Set("Mcp-Session-Id", "auth-test-session")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		default:
			http.Error(w, "unexpected method", http.StatusBadRequest)
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "my-secret-api-key")
	_, err := performSessionAutoInit(req, handler)

	require.NoError(t, err)
	assert.Equal(t, "my-secret-api-key", capturedInitAuth,
		"Authorization header must be forwarded to the auto-init initialize request")
}

func TestCopyAutoInitHeaders(t *testing.T) {
	tests := []struct {
		name       string
		src        http.Header
		wantCT     string
		wantAccept string
		wantAuth   string
	}{
		{
			name: "with authorization",
			src: http.Header{
				"Authorization": {"Bearer token123"},
				"X-Custom":      {"ignored"},
			},
			wantCT:     "application/json",
			wantAccept: "application/json, text/event-stream",
			wantAuth:   "Bearer token123",
		},
		{
			name:       "without authorization",
			src:        http.Header{},
			wantCT:     "application/json",
			wantAccept: "application/json, text/event-stream",
			wantAuth:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := http.Header{}
			copyAutoInitHeaders(dst, tt.src)

			assert.Equal(t, tt.wantCT, dst.Get("Content-Type"))
			assert.Equal(t, tt.wantAccept, dst.Get("Accept"))
			assert.Equal(t, tt.wantAuth, dst.Get("Authorization"))
			// Custom headers should not be copied.
			assert.Empty(t, dst.Get("X-Custom"))
		})
	}
}
