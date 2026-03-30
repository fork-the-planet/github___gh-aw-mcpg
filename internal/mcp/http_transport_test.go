package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/github/gh-aw-mcpg/internal/oidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// isHTTPConnectionError tests
// =============================================================================

func TestIsHTTPConnectionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error returns false",
			err:  nil,
			want: false,
		},
		{
			name: "plain error returns false",
			err:  fmt.Errorf("some generic error"),
			want: false,
		},
		{
			name: "OpError with dial op returns true",
			err:  &net.OpError{Op: "dial", Err: fmt.Errorf("connection refused")},
			want: true,
		},
		{
			name: "OpError with read op returns false",
			err:  &net.OpError{Op: "read", Err: fmt.Errorf("read failed")},
			want: false,
		},
		{
			name: "OpError with write op returns false",
			err:  &net.OpError{Op: "write", Err: fmt.Errorf("write failed")},
			want: false,
		},
		{
			name: "OpError with connect op returns false",
			err:  &net.OpError{Op: "connect", Err: fmt.Errorf("refused")},
			want: false,
		},
		{
			name: "wrapped OpError with dial op returns true",
			err:  fmt.Errorf("wrapped: %w", &net.OpError{Op: "dial", Err: fmt.Errorf("connection refused")}),
			want: true,
		},
		{
			name: "wrapped OpError with read op returns false",
			err:  fmt.Errorf("wrapped: %w", &net.OpError{Op: "read", Err: fmt.Errorf("EOF")}),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isHTTPConnectionError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// parseSSEResponse tests
// =============================================================================

func TestParseSSEResponse(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name:    "empty body",
			body:    "",
			wantErr: true,
		},
		{
			name:    "no data field - event only",
			body:    "event: message\n\n",
			wantErr: true,
		},
		{
			name:    "comment line only",
			body:    ": heartbeat\n",
			wantErr: true,
		},
		{
			name:    "retry line only",
			body:    "retry: 3000\n",
			wantErr: true,
		},
		{
			name: "valid data field",
			body: "event: message\ndata: {\"jsonrpc\":\"2.0\"}\n\n",
			want: `{"jsonrpc":"2.0"}`,
		},
		{
			name: "data field with leading whitespace line",
			body: "  data: {\"key\":\"value\"}\n",
			want: `{"key":"value"}`,
		},
		{
			name: "multiple lines, data present",
			body: "id: 1\nevent: message\ndata: {\"result\":true}\n\n",
			want: `{"result":true}`,
		},
		{
			name: "data field first",
			body: "data: {\"id\":1}\nevent: message\n",
			want: `{"id":1}`,
		},
		{
			name: "multiple data fields returns first",
			body: "data: {\"first\":true}\ndata: {\"second\":true}\n",
			want: `{"first":true}`,
		},
		{
			name: "realistic MCP SSE response",
			body: "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\"}}\n\n",
			want: `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`,
		},
		{
			// "data: \n" becomes "data:" after TrimSpace, which doesn't match "data: " prefix
			name:    "trailing space after colon - no match after TrimSpace",
			body:    "data: \n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSSEResponse([]byte(tt.body))
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "no data field found in SSE response")
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				assert.Equal(t, []byte(tt.want), got)
			}
		})
	}
}

// =============================================================================
// parseJSONRPCResponseWithSSE tests
// =============================================================================

func TestParseJSONRPCResponseWithSSE_ValidJSONStatus200(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"result":{"data":"ok"}}`)
	resp, err := parseJSONRPCResponseWithSSE(body, http.StatusOK, "test")

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.NotNil(t, resp.Result)
	assert.Nil(t, resp.Error)
}

func TestParseJSONRPCResponseWithSSE_ValidJSONNon200ReturnsSyntheticError(t *testing.T) {
	statusCodes := []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
	}

	for _, code := range statusCodes {
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			body := []byte(`{"jsonrpc":"2.0","id":1,"result":{"data":"ok"}}`)
			resp, err := parseJSONRPCResponseWithSSE(body, code, "test")

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NotNil(t, resp.Error, "should produce error for non-200 status")
			assert.Equal(t, -32603, resp.Error.Code)
			assert.Contains(t, resp.Error.Message, fmt.Sprintf("%d", code))
		})
	}
}

func TestParseJSONRPCResponseWithSSE_InvalidJSONStatus200NoSSE(t *testing.T) {
	body := []byte(`not valid json and no SSE format`)
	resp, err := parseJSONRPCResponseWithSSE(body, http.StatusOK, "test-context")

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "test-context")
}

func TestParseJSONRPCResponseWithSSE_InvalidJSONNon200ReturnsSyntheticError(t *testing.T) {
	body := []byte(`not valid json`)
	resp, err := parseJSONRPCResponseWithSSE(body, http.StatusUnauthorized, "test")

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32603, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "401")
	assert.Contains(t, resp.Error.Message, "Unauthorized")
}

func TestParseJSONRPCResponseWithSSE_SSEFormatStatus200(t *testing.T) {
	body := []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\"}}\n\n")
	resp, err := parseJSONRPCResponseWithSSE(body, http.StatusOK, "SSE response")

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.NotNil(t, resp.Result)
	assert.Nil(t, resp.Error)
}

func TestParseJSONRPCResponseWithSSE_SSEFormatNon200ReturnsSyntheticError(t *testing.T) {
	body := []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")
	resp, err := parseJSONRPCResponseWithSSE(body, http.StatusInternalServerError, "SSE response")

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32603, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "500")
}

func TestParseJSONRPCResponseWithSSE_SSEWithInvalidJSONDataStatus200(t *testing.T) {
	body := []byte("event: message\ndata: not-valid-json\n\n")
	resp, err := parseJSONRPCResponseWithSSE(body, http.StatusOK, "bad SSE data")

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestParseJSONRPCResponseWithSSE_SSEWithInvalidJSONDataNon200(t *testing.T) {
	body := []byte("event: message\ndata: not-valid-json\n\n")
	resp, err := parseJSONRPCResponseWithSSE(body, http.StatusBadGateway, "bad SSE data")

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32603, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "502")
}

func TestParseJSONRPCResponseWithSSE_JSONRPCErrorInBodyStatus200(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`)
	resp, err := parseJSONRPCResponseWithSSE(body, http.StatusOK, "error response")

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32600, resp.Error.Code)
	assert.Equal(t, "Invalid Request", resp.Error.Message)
}

func TestParseJSONRPCResponseWithSSE_EmptyBodyStatus200(t *testing.T) {
	body := []byte("")
	resp, err := parseJSONRPCResponseWithSSE(body, http.StatusOK, "empty")

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestParseJSONRPCResponseWithSSE_EmptyBodyNon200ReturnsSyntheticError(t *testing.T) {
	body := []byte("")
	resp, err := parseJSONRPCResponseWithSSE(body, http.StatusNotFound, "empty 404")

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32603, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "404")
	assert.Contains(t, resp.Error.Message, "Not Found")
}

func TestParseJSONRPCResponseWithSSE_ErrorContainsContextDesc(t *testing.T) {
	body := []byte(`completely unparseable content`)
	_, err := parseJSONRPCResponseWithSSE(body, http.StatusOK, "my-unique-context-description")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "my-unique-context-description")
}

func TestParseJSONRPCResponseWithSSE_ErrorContainsBodyPreview(t *testing.T) {
	body := []byte(`this is bad json that is not SSE formatted`)
	_, err := parseJSONRPCResponseWithSSE(body, http.StatusOK, "test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Response body:")
}

func TestParseJSONRPCResponseWithSSE_SyntheticErrorContainsStatusText(t *testing.T) {
	body := []byte(`plain text body`)
	resp, err := parseJSONRPCResponseWithSSE(body, http.StatusForbidden, "test")

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32603, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "403")
	assert.Contains(t, resp.Error.Message, "Forbidden")
}

func TestParseJSONRPCResponseWithSSE_SyntheticErrorDataContainsBody(t *testing.T) {
	originalBody := `{"error":"original error body"}`
	resp, err := parseJSONRPCResponseWithSSE([]byte(originalBody), http.StatusBadRequest, "test")

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	require.NotNil(t, resp.Error.Data)
	assert.Contains(t, string(resp.Error.Data), "original error body")
}

// =============================================================================
// createJSONRPCRequest tests
// =============================================================================

func TestCreateJSONRPCRequest(t *testing.T) {
	tests := []struct {
		name      string
		requestID uint64
		method    string
		params    interface{}
	}{
		{
			name:      "simple request with nil params",
			requestID: 1,
			method:    "tools/list",
			params:    nil,
		},
		{
			name:      "request with map params",
			requestID: 42,
			method:    "tools/call",
			params:    map[string]interface{}{"name": "my-tool", "arguments": map[string]interface{}{}},
		},
		{
			name:      "request with string params",
			requestID: 100,
			method:    "initialize",
			params:    "string-param",
		},
		{
			name:      "zero request ID",
			requestID: 0,
			method:    "ping",
			params:    nil,
		},
		{
			name:      "large request ID",
			requestID: ^uint64(0), // max uint64
			method:    "test/method",
			params:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := createJSONRPCRequest(tt.requestID, tt.method, tt.params)

			require.NotNil(t, req)
			assert.Equal(t, "2.0", req["jsonrpc"])
			assert.Equal(t, tt.requestID, req["id"])
			assert.Equal(t, tt.method, req["method"])
			assert.Equal(t, tt.params, req["params"])
		})
	}
}

func TestCreateJSONRPCRequest_HasAllRequiredFields(t *testing.T) {
	req := createJSONRPCRequest(1, "test/method", nil)

	_, hasJSONRPC := req["jsonrpc"]
	_, hasID := req["id"]
	_, hasMethod := req["method"]
	_, hasParams := req["params"]

	assert.True(t, hasJSONRPC, "should have jsonrpc field")
	assert.True(t, hasID, "should have id field")
	assert.True(t, hasMethod, "should have method field")
	assert.True(t, hasParams, "should have params field")
}

func TestCreateJSONRPCRequest_IsSerializable(t *testing.T) {
	req := createJSONRPCRequest(7, "tools/call", map[string]interface{}{"name": "tool"})

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "2.0", parsed["jsonrpc"])
	assert.Equal(t, float64(7), parsed["id"]) // JSON numbers are float64
	assert.Equal(t, "tools/call", parsed["method"])
}

// =============================================================================
// ensureToolCallArguments tests
// =============================================================================

func TestEnsureToolCallArguments(t *testing.T) {
	tests := []struct {
		name          string
		params        interface{}
		wantSameValue bool // result should equal input exactly
		wantArgsValue interface{}
	}{
		{
			name:          "nil params returned as-is",
			params:        nil,
			wantSameValue: true,
		},
		{
			name:          "string params returned as-is",
			params:        "string-params",
			wantSameValue: true,
		},
		{
			name:          "int params returned as-is",
			params:        42,
			wantSameValue: true,
		},
		{
			name:          "slice params returned as-is",
			params:        []string{"a", "b"},
			wantSameValue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ensureToolCallArguments(tt.params)
			if tt.wantSameValue {
				assert.Equal(t, tt.params, result, "non-map params should be returned unchanged")
			}
		})
	}
}

func TestEnsureToolCallArguments_MapWithExistingArguments(t *testing.T) {
	existingArgs := map[string]interface{}{"key": "value", "count": 5}
	params := map[string]interface{}{
		"name":      "my-tool",
		"arguments": existingArgs,
	}

	result := ensureToolCallArguments(params)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, existingArgs, resultMap["arguments"], "existing arguments should not be modified")
}

func TestEnsureToolCallArguments_MapWithoutArguments(t *testing.T) {
	params := map[string]interface{}{
		"name": "my-tool",
	}

	result := ensureToolCallArguments(params)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	args, hasArgs := resultMap["arguments"]
	require.True(t, hasArgs, "arguments key should be added")
	assert.Equal(t, map[string]interface{}{}, args, "added arguments should be empty map")
}

func TestEnsureToolCallArguments_MapWithNilArguments(t *testing.T) {
	params := map[string]interface{}{
		"name":      "my-tool",
		"arguments": nil,
	}

	result := ensureToolCallArguments(params)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	args, hasArgs := resultMap["arguments"]
	require.True(t, hasArgs, "arguments key should be present")
	assert.Equal(t, map[string]interface{}{}, args, "nil arguments should be replaced with empty map")
}

func TestEnsureToolCallArguments_EmptyMap(t *testing.T) {
	params := map[string]interface{}{}

	result := ensureToolCallArguments(params)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	args, hasArgs := resultMap["arguments"]
	require.True(t, hasArgs, "arguments key should be added to empty map")
	assert.Equal(t, map[string]interface{}{}, args)
}

func TestEnsureToolCallArguments_PreservesOtherFields(t *testing.T) {
	params := map[string]interface{}{
		"name":   "my-tool",
		"extra":  "extra-value",
		"number": 42,
	}

	result := ensureToolCallArguments(params)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "my-tool", resultMap["name"], "name should be preserved")
	assert.Equal(t, "extra-value", resultMap["extra"], "extra should be preserved")
	assert.Equal(t, 42, resultMap["number"], "number should be preserved")
	assert.Equal(t, map[string]interface{}{}, resultMap["arguments"], "arguments should be added")
}

func TestEnsureToolCallArguments_MutatesOriginalMap(t *testing.T) {
	// ensureToolCallArguments modifies the map in place (maps are reference types in Go)
	original := map[string]interface{}{
		"name": "my-tool",
	}

	ensureToolCallArguments(original)

	// The original map should now have "arguments" key added
	args, hasArgs := original["arguments"]
	assert.True(t, hasArgs, "original map should be mutated to include arguments key")
	assert.Equal(t, map[string]interface{}{}, args, "added arguments should be empty map")
}

// =============================================================================
// setupHTTPRequest tests
// =============================================================================

func TestSetupHTTPRequest_ValidURL(t *testing.T) {
	req, err := setupHTTPRequest(context.Background(), "http://localhost:8080/mcp", []byte(`{"jsonrpc":"2.0"}`), nil)

	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "http://localhost:8080/mcp", req.URL.String())
}

func TestSetupHTTPRequest_StandardHeaders(t *testing.T) {
	req, err := setupHTTPRequest(context.Background(), "http://localhost:8080", []byte(`{}`), nil)

	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, "application/json, text/event-stream", req.Header.Get("Accept"))
}

func TestSetupHTTPRequest_CustomHeaders(t *testing.T) {
	headers := map[string]string{
		"Authorization": "Bearer my-token",
		"X-API-Key":     "api-key-123",
	}

	req, err := setupHTTPRequest(context.Background(), "http://localhost:8080", []byte(`{}`), headers)

	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "Bearer my-token", req.Header.Get("Authorization"))
	assert.Equal(t, "api-key-123", req.Header.Get("X-API-Key"))
	// Standard headers should still be set
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

func TestSetupHTTPRequest_InvalidURL(t *testing.T) {
	req, err := setupHTTPRequest(context.Background(), "://invalid-url", []byte(`{}`), nil)

	require.Error(t, err)
	assert.Nil(t, req)
	assert.Contains(t, err.Error(), "failed to create HTTP request")
}

func TestSetupHTTPRequest_EmptyBody(t *testing.T) {
	req, err := setupHTTPRequest(context.Background(), "http://localhost:8080", []byte{}, nil)

	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

func TestSetupHTTPRequest_UsesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req, err := setupHTTPRequest(ctx, "http://localhost:8080", []byte(`{}`), nil)

	// setupHTTPRequest should succeed - context cancellation is detected when the request executes
	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, context.Canceled, req.Context().Err())
}

func TestSetupHTTPRequest_CustomHeaderOverridesContentType(t *testing.T) {
	headers := map[string]string{
		"Content-Type": "application/x-custom",
	}

	req, err := setupHTTPRequest(context.Background(), "http://localhost:8080", []byte(`{}`), headers)

	require.NoError(t, err)
	require.NotNil(t, req)
	// Custom Content-Type should override the default
	assert.Equal(t, "application/x-custom", req.Header.Get("Content-Type"))
}

func TestSetupHTTPRequest_MultipleCustomHeaders(t *testing.T) {
	headers := map[string]string{
		"X-Header-1": "value1",
		"X-Header-2": "value2",
		"X-Header-3": "value3",
	}

	req, err := setupHTTPRequest(context.Background(), "http://localhost:8080", []byte(`{}`), headers)

	require.NoError(t, err)
	require.NotNil(t, req)
	for key, expectedVal := range headers {
		assert.Equal(t, expectedVal, req.Header.Get(key), "header %s should match", key)
	}
}

// =============================================================================
// sendHTTPRequest integration tests (using httptest)
// =============================================================================

func TestSendHTTPRequest_EnsuresToolCallArguments(t *testing.T) {
	var receivedParams map[string]interface{}

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		method, _ := body["method"].(string)

		if method == "initialize" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "test-session")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      body["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo":      map[string]interface{}{"name": "test"},
				},
			})
			return
		}

		if method == "tools/call" {
			if p, ok := body["params"].(map[string]interface{}); ok {
				receivedParams = p
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"jsonrpc": "2.0",
			"id":      body["id"],
			"result":  map[string]interface{}{},
		})
	}))
	defer testServer.Close()

	conn, err := NewHTTPConnection(context.Background(), "test-server", testServer.URL, map[string]string{
		"Authorization": "test-token",
	}, nil, "")
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// Send tools/call without arguments field - should be added automatically
	params := map[string]interface{}{"name": "my-tool"}
	_, err = conn.sendHTTPRequest(context.Background(), "tools/call", params)
	require.NoError(t, err)

	require.NotNil(t, receivedParams, "server should have received tools/call params")
	_, hasArgs := receivedParams["arguments"]
	assert.True(t, hasArgs, "arguments field should be added for tools/call")
}

func TestSendHTTPRequest_SessionIDFromContext(t *testing.T) {
	var receivedSessionID string

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		method, _ := body["method"].(string)

		if method == "initialize" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "server-session")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      body["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo":      map[string]interface{}{"name": "test"},
				},
			})
			return
		}

		receivedSessionID = r.Header.Get("Mcp-Session-Id")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"jsonrpc": "2.0",
			"id":      body["id"],
			"result":  map[string]interface{}{},
		})
	}))
	defer testServer.Close()

	conn, err := NewHTTPConnection(context.Background(), "test-server", testServer.URL, map[string]string{
		"Authorization": "test-token",
	}, nil, "")
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// Session ID from context should take priority over stored session
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "context-session-id")
	_, err = conn.sendHTTPRequest(ctx, "tools/list", nil)
	require.NoError(t, err)

	assert.Equal(t, "context-session-id", receivedSessionID, "context session ID should take priority")
}

func TestSendHTTPRequest_SessionIDFromConnection(t *testing.T) {
	var receivedSessionIDs []string

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		method, _ := body["method"].(string)

		if method == "initialize" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "stored-session-456")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      body["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo":      map[string]interface{}{"name": "test"},
				},
			})
			return
		}

		receivedSessionIDs = append(receivedSessionIDs, r.Header.Get("Mcp-Session-Id"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"jsonrpc": "2.0",
			"id":      body["id"],
			"result":  map[string]interface{}{},
		})
	}))
	defer testServer.Close()

	// Use plain JSON transport directly: this test exercises the session-ID propagation
	// logic in sendHTTPRequest, which is specific to the plain JSON-RPC code path.
	conn := newPlainJSONConn(t, testServer.URL, map[string]string{"Authorization": "test-token"})
	require.NotNil(t, conn)
	defer conn.Close()

	// No session ID in context - should use stored session from initialization
	_, err := conn.sendHTTPRequest(context.Background(), "tools/list", nil)
	require.NoError(t, err)

	require.Len(t, receivedSessionIDs, 1)
	assert.Equal(t, "stored-session-456", receivedSessionIDs[0], "should use stored session ID")
}

func TestSendHTTPRequest_NonToolsCallMethodDoesNotAddArguments(t *testing.T) {
	var receivedParams map[string]interface{}

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		method, _ := body["method"].(string)

		if method == "initialize" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "test-session")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      body["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo":      map[string]interface{}{"name": "test"},
				},
			})
			return
		}

		if method == "tools/list" {
			if p, ok := body["params"].(map[string]interface{}); ok {
				receivedParams = p
			} else {
				receivedParams = nil
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"jsonrpc": "2.0",
			"id":      body["id"],
			"result":  map[string]interface{}{},
		})
	}))
	defer testServer.Close()

	conn, err := NewHTTPConnection(context.Background(), "test-server", testServer.URL, map[string]string{
		"Authorization": "test-token",
	}, nil, "")
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// Send tools/list with a map but no arguments - should NOT add arguments
	params := map[string]interface{}{"cursor": "next-page"}
	_, err = conn.sendHTTPRequest(context.Background(), "tools/list", params)
	require.NoError(t, err)

	if receivedParams != nil {
		_, hasArgs := receivedParams["arguments"]
		assert.False(t, hasArgs, "arguments should NOT be added for non tools/call methods")
	}
}

// =============================================================================
// isSessionNotFoundError tests
// =============================================================================

func TestIsSessionNotFoundError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error returns false", err: nil, want: false},
		{name: "unrelated error returns false", err: fmt.Errorf("internal server error"), want: false},
		{name: "exact match returns true", err: fmt.Errorf("session not found"), want: true},
		{name: "uppercase returns true", err: fmt.Errorf("Session Not Found"), want: true},
		{name: "embedded in longer message returns true", err: fmt.Errorf("Streamable HTTP error: Error POSTing to endpoint: session not found"), want: true},
		{name: "session expired message returns false", err: fmt.Errorf("session expired"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isSessionNotFoundError(tt.err))
		})
	}
}

// =============================================================================
// isSessionNotFoundHTTPResponse tests
// =============================================================================

func TestIsSessionNotFoundHTTPResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       []byte
		want       bool
	}{
		{name: "200 OK returns false", statusCode: http.StatusOK, body: []byte("session not found"), want: false},
		{name: "500 returns false", statusCode: http.StatusInternalServerError, body: []byte("session not found"), want: false},
		{name: "404 with unrelated body returns false", statusCode: http.StatusNotFound, body: []byte("resource not found"), want: false},
		{name: "404 with session not found body returns true", statusCode: http.StatusNotFound, body: []byte("session not found"), want: true},
		{name: "404 with uppercase body returns true", statusCode: http.StatusNotFound, body: []byte("Session Not Found"), want: true},
		{name: "404 with session not found embedded in JSON returns true", statusCode: http.StatusNotFound, body: []byte(`{"error":"session not found"}`), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isSessionNotFoundHTTPResponse(tt.statusCode, tt.body))
		})
	}
}

// =============================================================================
// Session reconnect tests (plain JSON-RPC)
// =============================================================================

// TestSendHTTPRequest_ReconnectsOnSessionNotFound verifies that when the backend returns
// a 404 "session not found" response, sendHTTPRequest reconnects and retries the request.
func TestSendHTTPRequest_ReconnectsOnSessionNotFound(t *testing.T) {
	requestCount := 0
	var receivedSessionIDs []string

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		method, _ := body["method"].(string)

		switch method {
		case "initialize":
			requestCount++
			sessionID := fmt.Sprintf("session-%d", requestCount)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", sessionID)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      body["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo":      map[string]interface{}{"name": "test"},
				},
			})

		case "tools/list":
			sessionID := r.Header.Get("Mcp-Session-Id")
			receivedSessionIDs = append(receivedSessionIDs, sessionID)

			// Simulate first tool call failing with session-not-found (session-1 expired)
			if sessionID == "session-1" {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, "session not found")
				return
			}

			// Subsequent calls with the new session succeed
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      body["id"],
				"result":  map[string]interface{}{"tools": []interface{}{}},
			})
		}
	}))
	defer testServer.Close()

	conn, err := NewHTTPConnection(context.Background(), "test-server", testServer.URL, map[string]string{
		"Authorization": "test-token",
	}, nil, "")
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// The initial session is "session-1". The first tools/list call should trigger a
	// reconnect (because the server returns 404 session-not-found for session-1),
	// get a new "session-2", and then succeed on retry.
	resp, err := conn.sendHTTPRequest(context.Background(), "tools/list", nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "response should not contain an error after reconnect")

	// Verify the reconnect happened: session-1 failed, session-2 succeeded.
	require.Len(t, receivedSessionIDs, 2, "expected two tool calls: initial (failed) + retry (succeeded)")
	assert.Equal(t, "session-1", receivedSessionIDs[0], "first attempt should use the initial session")
	assert.Equal(t, "session-2", receivedSessionIDs[1], "retry should use the reconnected session")
	assert.Equal(t, "session-2", conn.httpSessionID, "connection should store the new session ID")
}

// TestSendHTTPRequest_ReconnectFailure verifies that when reconnection itself fails,
// the original session-not-found response is returned to the caller.
func TestSendHTTPRequest_ReconnectFailure(t *testing.T) {
	firstInitDone := false

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		method, _ := body["method"].(string)

		switch method {
		case "initialize":
			if !firstInitDone {
				firstInitDone = true
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Mcp-Session-Id", "session-original")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
					"jsonrpc": "2.0",
					"id":      body["id"],
					"result": map[string]interface{}{
						"protocolVersion": "2024-11-05",
						"serverInfo":      map[string]interface{}{"name": "test"},
					},
				})
			} else {
				// Reconnect attempt also fails
				w.WriteHeader(http.StatusInternalServerError)
			}

		case "tools/list":
			// Always return session not found
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "session not found")
		}
	}))
	defer testServer.Close()

	conn, err := NewHTTPConnection(context.Background(), "test-server", testServer.URL, map[string]string{
		"Authorization": "test-token",
	}, nil, "")
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// The tools/list call gets session-not-found, reconnect fails (500 on initialize),
	// so the original session-not-found response is passed through.
	resp, err := conn.sendHTTPRequest(context.Background(), "tools/list", nil)
	require.NoError(t, err, "should not return a Go error, but a JSON-RPC error response")
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error, "response should contain an error when session-not-found and reconnect failed")
}

// TestSendHTTPRequest_NoReconnectOnOtherErrors verifies that non-session errors
// (e.g. 500 internal server error) do not trigger a reconnect attempt.
func TestSendHTTPRequest_NoReconnectOnOtherErrors(t *testing.T) {
	initCount := 0

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		method, _ := body["method"].(string)

		if method == "initialize" {
			initCount++
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "session-1")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"id":      body["id"],
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo":      map[string]interface{}{"name": "test"},
				},
			})
			return
		}

		// Return 500 – should not trigger reconnect
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal server error")
	}))
	defer testServer.Close()

	// Use plain JSON transport directly: this test verifies the no-reconnect behaviour
	// on 500 errors, which is specific to the plain JSON-RPC sendHTTPRequest path.
	conn := newPlainJSONConn(t, testServer.URL, map[string]string{"Authorization": "test-token"})
	require.NotNil(t, conn)
	defer conn.Close()

	_, err := conn.sendHTTPRequest(context.Background(), "tools/list", nil)
	require.NoError(t, err)

	// initCount should be 1 (initial only) – no reconnect was attempted.
	assert.Equal(t, 1, initCount, "should not reconnect on non-session-not-found errors")
}

// =============================================================================
// headerInjectingRoundTripper / buildHTTPClientWithHeaders tests
// =============================================================================

// TestHeaderInjectingRoundTripper verifies that every request made through the
// custom RoundTripper receives the configured headers.
func TestHeaderInjectingRoundTripper(t *testing.T) {
	received := make(map[string]string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received["Authorization"] = r.Header.Get("Authorization")
		received["X-Custom"] = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &headerInjectingRoundTripper{
		base: http.DefaultTransport,
		headers: map[string]string{
			"Authorization": "Basic dXNlcjpwYXNz",
			"X-Custom":      "hello",
		},
	}
	client := &http.Client{Transport: rt}

	req, err := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "Basic dXNlcjpwYXNz", received["Authorization"])
	assert.Equal(t, "hello", received["X-Custom"])
}

// TestBuildHTTPClientWithHeaders_Empty verifies that an empty headers map returns
// the same client (pointer equality).
func TestBuildHTTPClientWithHeaders_Empty(t *testing.T) {
	base := &http.Client{}
	result := buildHTTPClientWithHeaders(base, nil)
	assert.Same(t, base, result, "empty headers should return the original client unchanged")

	result2 := buildHTTPClientWithHeaders(base, map[string]string{})
	assert.Same(t, base, result2, "empty map should return the original client unchanged")
}

// TestBuildHTTPClientWithHeaders_NonEmpty verifies that a non-empty headers map
// returns a new client whose transport injects the headers.
func TestBuildHTTPClientWithHeaders_NonEmpty(t *testing.T) {
	received := make(map[string]string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received["Authorization"] = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	base := &http.Client{Transport: http.DefaultTransport}
	injected := buildHTTPClientWithHeaders(base, map[string]string{
		"Authorization": "Bearer token123",
	})
	assert.NotSame(t, base, injected, "non-empty headers should return a new client")

	req, err := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
	require.NoError(t, err)

	resp, err := injected.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "Bearer token123", received["Authorization"])
}

// =============================================================================
// oidcRoundTripper tests
// =============================================================================

// makeTestJWT builds a minimal JWT for testing purposes.
func makeTestJWT(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims := map[string]interface{}{
		"exp": exp,
		"iss": "https://token.actions.githubusercontent.com",
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		panic("makeTestJWT: unexpected json.Marshal error: " + err.Error())
	}
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	return fmt.Sprintf("%s.%s.dummy", header, payload)
}

// newTestOIDCServer creates a test OIDC server that returns the provided token.
func newTestOIDCServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"value": token})
	}))
}

// TestOIDCRoundTripper_SetsAuthorizationHeader verifies that the OIDC round tripper
// injects an Authorization: Bearer header with the acquired token.
func TestOIDCRoundTripper_SetsAuthorizationHeader(t *testing.T) {
	jwtToken := makeTestJWT(time.Now().Add(10 * time.Minute).Unix())
	oidcServer := newTestOIDCServer(t, jwtToken)
	defer oidcServer.Close()

	var receivedAuth string
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	provider := oidc.NewProvider(oidcServer.URL, "request-token")
	client := buildHTTPClientWithOIDC(&http.Client{}, provider, "https://example.com")

	req, err := http.NewRequestWithContext(context.Background(), "GET", targetServer.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "Bearer "+jwtToken, receivedAuth, "OIDC token should be injected as Authorization: Bearer")
}

// TestOIDCRoundTripper_StaticHeadersCoexist verifies that static headers are preserved
// while the OIDC token overrides the Authorization header.
// This test mirrors the layering order used in NewHTTPConnection:
// static headers are applied first (outer), OIDC Authorization is applied last (inner).
func TestOIDCRoundTripper_StaticHeadersCoexist(t *testing.T) {
	jwtToken := makeTestJWT(time.Now().Add(10 * time.Minute).Unix())
	oidcServer := newTestOIDCServer(t, jwtToken)
	defer oidcServer.Close()

	receivedHeaders := make(map[string]string)
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders["Authorization"] = r.Header.Get("Authorization")
		receivedHeaders["X-Custom-Header"] = r.Header.Get("X-Custom-Header")
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	// Build a client using the same layering as NewHTTPConnection:
	//
	//	headerInjectingRoundTripper (outer — sets static headers first)
	//	  └─ oidcRoundTripper       (inner — overrides Authorization with OIDC token)
	//	       └─ http.DefaultTransport
	base := &http.Client{}
	provider := oidc.NewProvider(oidcServer.URL, "request-token")
	withOIDC := buildHTTPClientWithOIDC(base, provider, "https://example.com")
	client := buildHTTPClientWithHeaders(withOIDC, map[string]string{
		"Authorization":   "Bearer static-token",
		"X-Custom-Header": "custom-value",
	})

	req, err := http.NewRequestWithContext(context.Background(), "GET", targetServer.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "Bearer "+jwtToken, receivedHeaders["Authorization"],
		"OIDC token should override the static Authorization header")
	assert.Equal(t, "custom-value", receivedHeaders["X-Custom-Header"],
		"Static custom header should be preserved")
}

// TestOIDCRoundTripper_ErrorPropagation verifies that OIDC token acquisition errors
// are propagated as transport errors.
func TestOIDCRoundTripper_ErrorPropagation(t *testing.T) {
	// Use an unreachable URL to trigger a token acquisition failure
	provider := oidc.NewProvider("http://127.0.0.1:1", "request-token")

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	client := buildHTTPClientWithOIDC(&http.Client{}, provider, "https://example.com")

	req, err := http.NewRequestWithContext(context.Background(), "GET", targetServer.URL, nil)
	require.NoError(t, err)

	_, err = client.Do(req)
	require.Error(t, err, "Should return an error when OIDC token acquisition fails")
	assert.Contains(t, err.Error(), "OIDC token acquisition failed")
}
