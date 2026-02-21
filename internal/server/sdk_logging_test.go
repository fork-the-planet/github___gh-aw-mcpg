package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeJSONRPCErrorResponse returns a JSON-encoded JSON-RPC error response body.
func makeJSONRPCErrorResponse(id interface{}, code int, msg string) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": msg,
		},
	})
	return b
}

// makeJSONRPCSuccessResponse returns a JSON-encoded JSON-RPC success response body.
func makeJSONRPCSuccessResponse(id interface{}, result interface{}) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	return b
}

// makeJSONRPCRequest returns a JSON-encoded JSON-RPC request body.
func makeJSONRPCRequest(method string, id interface{}) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	})
	return b
}

// handlerCapture is a helper to track what an inner handler received.
type handlerCapture struct {
	called bool
	body   []byte
}

// makeInnerHandler creates an http.Handler that records the request body and returns the given response.
func makeInnerHandler(capture *handlerCapture, statusCode int, responseBody []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.called = true
		if r.Body != nil {
			capture.body, _ = io.ReadAll(r.Body)
		}
		w.WriteHeader(statusCode)
		if len(responseBody) > 0 {
			w.Write(responseBody) // response write errors are irrelevant in test helpers
		}
	})
}

// TestWithSDKLogging_PassesResponseThrough verifies that the outer handler forwards
// the inner handler's response status code and body unchanged.
func TestWithSDKLogging_PassesResponseThrough(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		requestBody    []byte
		responseStatus int
		responseBody   []byte
	}{
		{
			name:           "GET request with success response",
			method:         "GET",
			requestBody:    nil,
			responseStatus: http.StatusOK,
			responseBody:   makeJSONRPCSuccessResponse(1, map[string]interface{}{"ok": true}),
		},
		{
			name:           "POST request with JSON-RPC success response",
			method:         "POST",
			requestBody:    makeJSONRPCRequest("tools/list", 1),
			responseStatus: http.StatusOK,
			responseBody:   makeJSONRPCSuccessResponse(1, map[string]interface{}{"tools": []interface{}{}}),
		},
		{
			name:           "POST request with JSON-RPC error response",
			method:         "POST",
			requestBody:    makeJSONRPCRequest("tools/call", 2),
			responseStatus: http.StatusOK,
			responseBody:   makeJSONRPCErrorResponse(2, -32601, "method not found"),
		},
		{
			name:           "POST request with non-JSON response",
			method:         "POST",
			requestBody:    makeJSONRPCRequest("initialize", 3),
			responseStatus: http.StatusAccepted,
			responseBody:   []byte("event: message\ndata: {}\n\n"),
		},
		{
			name:           "POST request with empty response body",
			method:         "POST",
			requestBody:    makeJSONRPCRequest("notifications/initialized", nil),
			responseStatus: http.StatusAccepted,
			responseBody:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			capture := &handlerCapture{}
			inner := makeInnerHandler(capture, tt.responseStatus, tt.responseBody)
			wrapped := WithSDKLogging(inner, "test-mode")

			var reqBody io.Reader
			if tt.requestBody != nil {
				reqBody = bytes.NewBuffer(tt.requestBody)
			}
			req := httptest.NewRequest(tt.method, "/mcp", reqBody)
			w := httptest.NewRecorder()

			wrapped.ServeHTTP(w, req)

			require.True(capture.called, "inner handler must be called")
			assert.Equal(tt.responseStatus, w.Code, "response status code should pass through")
			if tt.responseBody != nil {
				assert.Equal(tt.responseBody, w.Body.Bytes(), "response body should pass through unchanged")
			} else {
				assert.Empty(w.Body.Bytes(), "response body should be empty")
			}
		})
	}
}

// TestWithSDKLogging_PreservesRequestBodyForInnerHandler verifies that after the
// outer handler reads the body for logging, it restores it so the inner handler
// can also read the complete body.
func TestWithSDKLogging_PreservesRequestBodyForInnerHandler(t *testing.T) {
	requestBody := makeJSONRPCRequest("tools/call", 42)

	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, makeJSONRPCSuccessResponse(42, "ok"))
	wrapped := WithSDKLogging(inner, "routed")

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBuffer(requestBody))
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	require.True(t, capture.called, "inner handler must be called")
	assert.Equal(t, requestBody, capture.body, "inner handler should receive the full original request body")
}

// TestWithSDKLogging_GetRequestBodyNotRead verifies that for GET requests the body
// is not read/consumed by the logging wrapper.
func TestWithSDKLogging_GetRequestBodyNotRead(t *testing.T) {
	someBody := []byte(`{"not": "jsonrpc"}`)

	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, makeJSONRPCSuccessResponse(1, nil))
	wrapped := WithSDKLogging(inner, "unified")

	req := httptest.NewRequest("GET", "/mcp", bytes.NewBuffer(someBody))
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	require.True(t, capture.called)
	// For GET requests the body should be passed through unmodified (readable by inner handler)
	assert.Equal(t, someBody, capture.body, "GET request body should not be consumed by logging wrapper")
}

// TestWithSDKLogging_InvalidJSONRequestBody verifies that an invalid JSON request body
// does not panic and still calls the inner handler.
func TestWithSDKLogging_InvalidJSONRequestBody(t *testing.T) {
	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, makeJSONRPCSuccessResponse(1, nil))
	wrapped := WithSDKLogging(inner, "test")

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("not-valid-json"))
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called, "inner handler must still be called with invalid JSON body")
}

// TestWithSDKLogging_ToolNotFoundError verifies that a JSON-RPC error with code -32602
// on a tools/call request does not panic and the response still passes through.
func TestWithSDKLogging_ToolNotFoundError_InvalidParams(t *testing.T) {
	responseBody := makeJSONRPCErrorResponse(1, -32602, "unknown tool: some___tool")
	requestBody := makeJSONRPCRequest("tools/call", 1)

	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, responseBody)
	wrapped := WithSDKLogging(inner, "routed")

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBuffer(requestBody))
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, responseBody, w.Body.Bytes())
}

// TestWithSDKLogging_ToolNotFoundError_MethodNotFound verifies that a JSON-RPC error
// with code -32601 on a tools/call request does not panic.
func TestWithSDKLogging_ToolNotFoundError_MethodNotFound(t *testing.T) {
	responseBody := makeJSONRPCErrorResponse(2, -32601, "method not found")
	requestBody := makeJSONRPCRequest("tools/call", 2)

	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, responseBody)
	wrapped := WithSDKLogging(inner, "unified")

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBuffer(requestBody))
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called)
}

// TestWithSDKLogging_ProtocolStateError_SessionInitialization verifies that a JSON-RPC
// error message containing "session initialization" is handled without panic.
func TestWithSDKLogging_ProtocolStateError_SessionInitialization(t *testing.T) {
	responseBody := makeJSONRPCErrorResponse(3, -32600, "request is invalid during session initialization")
	requestBody := makeJSONRPCRequest("tools/list", 3)

	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, responseBody)
	wrapped := WithSDKLogging(inner, "routed")

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBuffer(requestBody))
	req.Header.Set("Authorization", "test-api-key-abc123")
	req.Header.Set("Mcp-Session-Id", "mcp-sess-xyz")
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called)
}

// TestWithSDKLogging_ProtocolStateError_InvalidDuring verifies that a JSON-RPC error
// message containing "invalid during" is handled without panic.
func TestWithSDKLogging_ProtocolStateError_InvalidDuring(t *testing.T) {
	responseBody := makeJSONRPCErrorResponse(4, -32600, "request is invalid during normal operation")
	requestBody := makeJSONRPCRequest("initialize", 4)

	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, responseBody)
	wrapped := WithSDKLogging(inner, "unified")

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBuffer(requestBody))
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called)
}

// TestWithSDKLogging_GeneralJSONRPCError verifies that a JSON-RPC error with a code
// unrelated to tool-not-found does not panic and response passes through.
func TestWithSDKLogging_GeneralJSONRPCError(t *testing.T) {
	responseBody := makeJSONRPCErrorResponse(5, -32700, "parse error")
	requestBody := makeJSONRPCRequest("initialize", 5)

	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusBadRequest, responseBody)
	wrapped := WithSDKLogging(inner, "test")

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBuffer(requestBody))
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestWithSDKLogging_NonJSONResponse verifies that a non-JSON response (e.g., SSE stream)
// is handled without panic and passes through.
func TestWithSDKLogging_NonJSONResponse(t *testing.T) {
	sseBody := []byte("event: message\ndata: {\"jsonrpc\":\"2.0\"}\n\n")
	requestBody := makeJSONRPCRequest("tools/list", 6)

	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, sseBody)
	wrapped := WithSDKLogging(inner, "routed")

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBuffer(requestBody))
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called)
	assert.Equal(t, sseBody, w.Body.Bytes())
}

// TestWithSDKLogging_LargeNonJSONResponse verifies that a large non-JSON response
// (>500 bytes) is handled without panic (it gets truncated in logging but passes through fully).
func TestWithSDKLogging_LargeNonJSONResponse(t *testing.T) {
	// Build a non-JSON response larger than 500 bytes
	largeBody := []byte("event: message\ndata: " + strings.Repeat("x", 600) + "\n\n")
	requestBody := makeJSONRPCRequest("tools/list", 7)

	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, largeBody)
	wrapped := WithSDKLogging(inner, "unified")

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBuffer(requestBody))
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called)
	// Full body still passes through even though logging truncates it
	assert.Equal(t, largeBody, w.Body.Bytes())
}

// TestWithSDKLogging_EmptyResponseBody verifies that an empty response body is handled
// without panic.
func TestWithSDKLogging_EmptyResponseBody(t *testing.T) {
	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusAccepted, nil)
	wrapped := WithSDKLogging(inner, "test")

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBuffer(makeJSONRPCRequest("notifications/initialized", nil)))
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called)
	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Empty(t, w.Body.Bytes())
}

// TestWithSDKLogging_AuthorizationAndMcpSessionHeaders verifies that requests with
// Authorization and Mcp-Session-Id headers are handled without panic.
func TestWithSDKLogging_AuthorizationAndMcpSessionHeaders(t *testing.T) {
	responseBody := makeJSONRPCSuccessResponse(1, map[string]interface{}{"result": "ok"})

	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, responseBody)
	wrapped := WithSDKLogging(inner, "routed")

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBuffer(makeJSONRPCRequest("initialize", 1)))
	req.Header.Set("Authorization", "my-secret-api-key")
	req.Header.Set("Mcp-Session-Id", "session-abc-123")
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestWithSDKLogging_ModeString verifies that the mode string is accepted and does not
// affect request/response pass-through behavior.
func TestWithSDKLogging_ModeString(t *testing.T) {
	modes := []string{"routed", "unified", "test", "", "custom-mode"}

	for _, mode := range modes {
		t.Run("mode="+mode, func(t *testing.T) {
			responseBody := makeJSONRPCSuccessResponse(1, nil)
			capture := &handlerCapture{}
			inner := makeInnerHandler(capture, http.StatusOK, responseBody)
			wrapped := WithSDKLogging(inner, mode)

			req := httptest.NewRequest("GET", "/mcp", nil)
			w := httptest.NewRecorder()

			assert.NotPanics(t, func() {
				wrapped.ServeHTTP(w, req)
			})
			assert.True(t, capture.called)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

// TestWithSDKLogging_ToolNotFoundError_NonToolsCallMethod verifies that a -32602 error
// on a non-tools/call method is treated as a general error (not tool-not-found).
func TestWithSDKLogging_ToolNotFoundError_NonToolsCallMethod(t *testing.T) {
	// Code -32602 but for a different method (not tools/call)
	responseBody := makeJSONRPCErrorResponse(8, -32602, "invalid params")
	requestBody := makeJSONRPCRequest("initialize", 8)

	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, responseBody)
	wrapped := WithSDKLogging(inner, "test")

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBuffer(requestBody))
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called)
}

// TestWithSDKLogging_PostRequestWithNilBody verifies that a POST request with nil body
// does not panic and still calls the inner handler.
func TestWithSDKLogging_PostRequestWithNilBody(t *testing.T) {
	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, makeJSONRPCSuccessResponse(1, nil))
	wrapped := WithSDKLogging(inner, "test")

	req := httptest.NewRequest("POST", "/mcp", nil)
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called)
}

// TestWithSDKLogging_ReturnsHTTPHandler verifies that WithSDKLogging returns a valid
// http.Handler (not nil).
func TestWithSDKLogging_ReturnsHTTPHandler(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := WithSDKLogging(inner, "test")
	require.NotNil(t, wrapped, "WithSDKLogging must return a non-nil http.Handler")
}

// TestWithSDKLogging_JSONRPCSuccessWithResultNil verifies JSON-RPC success when result
// is null/nil.
func TestWithSDKLogging_JSONRPCSuccessWithResultNil(t *testing.T) {
	responseBody := makeJSONRPCSuccessResponse(9, nil)
	requestBody := makeJSONRPCRequest("notifications/cancelled", 9)

	capture := &handlerCapture{}
	inner := makeInnerHandler(capture, http.StatusOK, responseBody)
	wrapped := WithSDKLogging(inner, "test")

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBuffer(requestBody))
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})
	assert.True(t, capture.called)
	assert.Equal(t, responseBody, w.Body.Bytes())
}
