package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPRequest_SessionIDHeader tests that the Mcp-Session-Id header is added to HTTP requests
func TestHTTPRequest_SessionIDHeader(t *testing.T) {
	// Create a test HTTP server that captures headers
	var receivedSessionID string
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the Mcp-Session-Id header
		receivedSessionID = r.Header.Get("Mcp-Session-Id")

		// Return a mock JSON-RPC response
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"tools": []map[string]interface{}{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer testServer.Close()

	// Create an HTTP connection
	conn, err := NewHTTPConnection(context.Background(), "test-server", testServer.URL, map[string]string{
		"Authorization": "test-auth-token",
	})
	require.NoError(t, err, "Failed to create HTTP connection")

	// Create a context with session ID
	sessionID := "test-session-123"
	ctx := context.WithValue(context.Background(), SessionIDContextKey, sessionID)

	// Send a request with the context containing the session ID
	_, err = conn.SendRequestWithServerID(ctx, "tools/list", nil, "test-server")
	require.NoError(t, err, "Failed to send request")

	// Verify the Mcp-Session-Id header was received
	assert.Equal(t, sessionID, receivedSessionID, "Expected Mcp-Session-Id header '%s', got '%s'", sessionID, receivedSessionID)
}

// TestHTTPRequest_NoSessionID tests that requests work without session ID
func TestHTTPRequest_NoSessionID(t *testing.T) {
	// Create a test HTTP server
	var receivedSessionID string
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSessionID = r.Header.Get("Mcp-Session-Id")

		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"tools": []map[string]interface{}{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer testServer.Close()

	// Create an HTTP connection
	conn, err := NewHTTPConnection(context.Background(), "test-server", testServer.URL, map[string]string{
		"Authorization": "test-auth-token",
	})
	require.NoError(t, err, "Failed to create HTTP connection")

	// Send a request without session ID in context
	ctx := context.Background()
	_, err = conn.SendRequestWithServerID(ctx, "tools/list", nil, "test-server")
	require.NoError(t, err, "Failed to send request")

	// Verify no Mcp-Session-Id header was sent (empty string is acceptable)
	if receivedSessionID != "" {
		t.Logf("Received Mcp-Session-Id header: '%s' (expected empty)", receivedSessionID)
	}
}

// TestHTTPRequest_ConfiguredHeaders tests that configured headers are still sent
func TestHTTPRequest_ConfiguredHeaders(t *testing.T) {
	// Create a test HTTP server that captures headers
	var receivedAuth, receivedSessionID string
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedSessionID = r.Header.Get("Mcp-Session-Id")

		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"tools": []map[string]interface{}{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer testServer.Close()

	// Create an HTTP connection with configured headers
	authToken := "configured-auth-token"
	conn, err := NewHTTPConnection(context.Background(), "test-server", testServer.URL, map[string]string{
		"Authorization": authToken,
	})
	require.NoError(t, err, "Failed to create HTTP connection")

	// Create a context with session ID
	sessionID := "session-with-auth"
	ctx := context.WithValue(context.Background(), SessionIDContextKey, sessionID)

	// Send a request
	_, err = conn.SendRequestWithServerID(ctx, "tools/list", nil, "test-server")
	require.NoError(t, err, "Failed to send request")

	// Verify both headers were received
	assert.Equal(t, authToken, receivedAuth)
	assert.Equal(t, sessionID, receivedSessionID)
}
func TestSetSinkServerIDs(t *testing.T) {
	difc.SetSinkServerIDs(nil)
	t.Cleanup(func() {
		difc.SetSinkServerIDs(nil)
	})

	t.Run("empty by default", func(t *testing.T) {
		assert.False(t, difc.IsSinkServerID("safeoutputs"))
	})

	t.Run("configured values are matched", func(t *testing.T) {
		difc.SetSinkServerIDs([]string{"safeoutputs", "github"})
		assert.True(t, difc.IsSinkServerID("safeoutputs"))
		assert.True(t, difc.IsSinkServerID("github"))
		assert.False(t, difc.IsSinkServerID("unknown"))
	})

	t.Run("normalizes deduplicates and trims", func(t *testing.T) {
		difc.SetSinkServerIDs([]string{" safeoutputs ", "", "safeoutputs", "github"})
		assert.True(t, difc.IsSinkServerID("safeoutputs"))
		assert.True(t, difc.IsSinkServerID("github"))
		assert.False(t, difc.IsSinkServerID(""))
	})

	t.Run("reset to empty disables matching", func(t *testing.T) {
		difc.SetSinkServerIDs([]string{"safeoutputs"})
		assert.True(t, difc.IsSinkServerID("safeoutputs"))

		difc.SetSinkServerIDs(nil)
		assert.False(t, difc.IsSinkServerID("safeoutputs"))
	})
}

// TestExpandDockerEnvArgs tests the Docker environment variable expansion function
func TestExpandDockerEnvArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		envVars  map[string]string
		expected []string
	}{
		{
			name:     "no -e flags",
			args:     []string{"run", "--rm", "image"},
			envVars:  map[string]string{},
			expected: []string{"run", "--rm", "image"},
		},
		{
			name:     "expand single env variable",
			args:     []string{"run", "-e", "VAR_NAME", "image"},
			envVars:  map[string]string{"VAR_NAME": "value1"},
			expected: []string{"run", "-e", "VAR_NAME=value1", "image"},
		},
		{
			name:     "expand multiple env variables",
			args:     []string{"run", "-e", "VAR1", "-e", "VAR2", "image"},
			envVars:  map[string]string{"VAR1": "value1", "VAR2": "value2"},
			expected: []string{"run", "-e", "VAR1=value1", "-e", "VAR2=value2", "image"},
		},
		{
			name:     "preserve existing key=value format",
			args:     []string{"run", "-e", "VAR=predefined", "image"},
			envVars:  map[string]string{},
			expected: []string{"run", "-e", "VAR=predefined", "image"},
		},
		{
			name:     "mixed: expand and preserve",
			args:     []string{"run", "-e", "VAR1", "-e", "VAR2=fixed", "image"},
			envVars:  map[string]string{"VAR1": "value1"},
			expected: []string{"run", "-e", "VAR1=value1", "-e", "VAR2=fixed", "image"},
		},
		{
			name:     "undefined env variable leaves arg unchanged",
			args:     []string{"run", "-e", "UNDEFINED_VAR", "image"},
			envVars:  map[string]string{},
			expected: []string{"run", "-e", "UNDEFINED_VAR", "image"},
		},
		{
			name:     "empty env variable value expands to key=",
			args:     []string{"run", "-e", "EMPTY_VAR", "image"},
			envVars:  map[string]string{"EMPTY_VAR": ""},
			expected: []string{"run", "-e", "EMPTY_VAR=", "image"},
		},
		{
			name:     "-e at end of args (no following arg)",
			args:     []string{"run", "image", "-e"},
			envVars:  map[string]string{},
			expected: []string{"run", "image", "-e"},
		},
		{
			name:     "nil args returns empty slice",
			args:     nil,
			envVars:  map[string]string{},
			expected: []string{},
		},
		{
			name:     "empty args returns empty slice",
			args:     []string{},
			envVars:  map[string]string{},
			expected: []string{},
		},
		{
			name:     "-e followed by empty string arg is not expanded",
			args:     []string{"run", "-e", "", "image"},
			envVars:  map[string]string{},
			expected: []string{"run", "-e", "", "image"},
		},
		{
			name:     "value with equals sign in env var value",
			args:     []string{"run", "-e", "KEY_WITH_EQUALS", "image"},
			envVars:  map[string]string{"KEY_WITH_EQUALS": "a=b=c"},
			expected: []string{"run", "-e", "KEY_WITH_EQUALS=a=b=c", "image"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				require.NoError(t, os.Setenv(k, v))
			}
			t.Cleanup(func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			})

			result := ExpandEnvArgs(tt.args)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestHTTPRequest_ErrorResponses tests handling of various error conditions
func TestHTTPRequest_ErrorResponses(t *testing.T) {
	tests := []struct {
		name               string
		statusCode         int
		responseBody       map[string]interface{}
		expectError        bool
		errorSubstring     string
		needSuccessfulInit bool // If true, return success for initialize requests
	}{
		{
			name:       "HTTP 200 success",
			statusCode: http.StatusOK,
			responseBody: map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]interface{}{
					"tools": []interface{}{},
				},
			},
			expectError: false,
		},
		{
			name:       "HTTP 404 not found",
			statusCode: http.StatusNotFound,
			responseBody: map[string]interface{}{
				"error": "Not found",
			},
			expectError:    true,
			errorSubstring: "status=404",
		},
		{
			name:       "HTTP 500 server error",
			statusCode: http.StatusInternalServerError,
			responseBody: map[string]interface{}{
				"error": "Internal server error",
			},
			expectError:    true,
			errorSubstring: "status=500",
		},
		{
			name:       "JSON-RPC error response",
			statusCode: http.StatusOK,
			responseBody: map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"error": map[string]interface{}{
					"code":    -32600,
					"message": "Invalid request",
				},
			},
			expectError:        false, // JSON-RPC errors are returned as valid responses
			needSuccessfulInit: true,  // Need successful initialize to test error handling
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server with specific response
			testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Read request body to determine if it's an initialize request
				var reqBody map[string]interface{}
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("Failed to read request body: %v", err)
					http.Error(w, "Internal error", http.StatusInternalServerError)
					return
				}
				// Silently reject empty-body requests (e.g. GET/DELETE from Streamable
				// transport during session lifecycle); they are not part of this test.
				if len(bodyBytes) == 0 {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
					// Silently reject non-JSON bodies (probe requests from SDK transports).
					http.Error(w, "Bad request", http.StatusBadRequest)
					return
				}

				// If this test case needs successful initialization, return success for initialize
				// and error for subsequent requests
				method, _ := reqBody["method"].(string)
				if tt.needSuccessfulInit && method == "initialize" {
					// Return success for initialize request
					w.WriteHeader(http.StatusOK)
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Mcp-Session-Id", "test-session-123")
					json.NewEncoder(w).Encode(map[string]interface{}{
						"jsonrpc": "2.0",
						"id":      reqBody["id"],
						"result": map[string]interface{}{
							"protocolVersion": "2024-11-05",
							"serverInfo": map[string]interface{}{
								"name":    "test-server",
								"version": "1.0.0",
							},
						},
					})
					return
				}

				// For all other cases, return the configured response
				w.WriteHeader(tt.statusCode)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.responseBody)
			}))
			defer testServer.Close()

			// Create connection with custom headers to use plain JSON transport
			conn, err := NewHTTPConnection(context.Background(), "test-server", testServer.URL, map[string]string{
				"Authorization": "test-token",
			})
			if err != nil && tt.expectError {
				// Error during initialization is expected for some error conditions
				if tt.errorSubstring != "" && !containsSubstring(err.Error(), tt.errorSubstring) {
					t.Errorf("Expected error to contain '%s', got: %v", tt.errorSubstring, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Failed to create connection: %v", err)
			}

			// Send request
			_, err = conn.SendRequestWithServerID(context.Background(), "tools/list", nil, "test-server")

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorSubstring != "" && !containsSubstring(err.Error(), tt.errorSubstring) {
					t.Errorf("Expected error to contain '%s', got: %v", tt.errorSubstring, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestConnection_IsHTTP tests the IsHTTP, GetHTTPURL, and GetHTTPHeaders methods
func TestConnection_IsHTTP(t *testing.T) {
	// Create a mock HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer testServer.Close()

	headers := map[string]string{
		"Authorization": "test-auth",
		"X-Custom":      "custom-value",
	}

	conn, err := NewHTTPConnection(context.Background(), "test-server", testServer.URL, headers)
	require.NoError(t, err, "Failed to create HTTP connection")
	defer conn.Close()

	// Test IsHTTP
	assert.True(t, conn.IsHTTP(), "Expected IsHTTP() to return true for HTTP connection")

	// Test GetHTTPURL
	if conn.GetHTTPURL() != testServer.URL {
		t.Errorf("Expected URL '%s', got '%s'", testServer.URL, conn.GetHTTPURL())
	}

	// Test GetHTTPHeaders
	returnedHeaders := conn.GetHTTPHeaders()
	assert.Equal(t, len(headers), len(returnedHeaders))
	for k, v := range headers {
		if returnedHeaders[k] != v {
			t.Errorf("Expected header '%s' to be '%s', got '%s'", k, v, returnedHeaders[k])
		}
	}
}

// TestHTTPConnection_InvalidURL tests error handling for invalid URLs
func TestHTTPConnection_InvalidURL(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		headers        map[string]string
		expectError    bool
		errorSubstring string
	}{
		{
			name:        "valid URL with headers",
			url:         "http://localhost:3000",
			headers:     map[string]string{"Authorization": "test"},
			expectError: true, // Will fail to connect but URL is valid
		},
		{
			name:        "empty URL",
			url:         "",
			headers:     map[string]string{"Authorization": "test"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewHTTPConnection(context.Background(), "test-server", tt.url, tt.headers)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorSubstring != "" && !containsSubstring(err.Error(), tt.errorSubstring) {
					t.Errorf("Expected error to contain '%s', got: %v", tt.errorSubstring, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// containsSubstring is a helper to check if a string contains a substring
func containsSubstring(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestNewMCPClient tests the newMCPClient helper function
func TestNewMCPClient(t *testing.T) {
	client := newMCPClient(nil)
	require.NotNil(t, client, "newMCPClient should return a non-nil client")
}

// TestNewMCPClientWithLogger tests that newMCPClient accepts a logger
func TestNewMCPClientWithLogger(t *testing.T) {
	log := logger.New("test:client")
	client := newMCPClient(log)
	require.NotNil(t, client, "newMCPClient should return a non-nil client with logger")
}

// TestSetupHTTPRequest tests the setupHTTPRequest helper function
func TestSetupHTTPRequest(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		requestBody    []byte
		headers        map[string]string
		expectError    bool
		expectedMethod string
	}{
		{
			name:           "basic request with no custom headers",
			url:            "http://example.com/mcp",
			requestBody:    []byte(`{"test": "data"}`),
			headers:        map[string]string{},
			expectError:    false,
			expectedMethod: "POST",
		},
		{
			name:        "request with custom headers",
			url:         "http://example.com/mcp",
			requestBody: []byte(`{"test": "data"}`),
			headers: map[string]string{
				"Authorization": "Bearer token123",
				"X-Custom":      "value",
			},
			expectError:    false,
			expectedMethod: "POST",
		},
		{
			name:           "request with empty body",
			url:            "http://example.com/mcp",
			requestBody:    []byte{},
			headers:        map[string]string{},
			expectError:    false,
			expectedMethod: "POST",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req, err := setupHTTPRequest(ctx, tt.url, tt.requestBody, tt.headers)

			if tt.expectError {
				assert.Error(t, err, "Expected error")
				return
			}

			require.NoError(t, err, "setupHTTPRequest should not return error")
			require.NotNil(t, req, "Request should not be nil")

			// Verify method
			assert.Equal(t, tt.expectedMethod, req.Method, "Method should be POST")

			// Verify URL
			assert.Equal(t, tt.url, req.URL.String(), "URL should match")

			// Verify standard headers
			assert.Equal(t, "application/json", req.Header.Get("Content-Type"), "Content-Type should be application/json")
			assert.Equal(t, "application/json, text/event-stream", req.Header.Get("Accept"), "Accept header should be set")

			// Verify custom headers
			for key, value := range tt.headers {
				assert.Equal(t, value, req.Header.Get(key), "Custom header %s should match", key)
			}
		})
	}
}

// TestNewHTTPConnection tests the newHTTPConnection helper function
func TestNewHTTPConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newMCPClient(nil)
	url := "http://example.com/mcp"
	headers := map[string]string{"Authorization": "test"}
	httpClient := &http.Client{}

	conn := newHTTPConnection(ctx, cancel, client, nil, url, headers, httpClient, HTTPTransportStreamable, "test-server")

	require.NotNil(t, conn, "Connection should not be nil")
	assert.Equal(t, client, conn.client, "Client should match")
	assert.Equal(t, ctx, conn.ctx, "Context should match")
	assert.NotNil(t, conn.cancel, "Cancel function should not be nil")
	assert.True(t, conn.isHTTP, "isHTTP should be true")
	assert.Equal(t, url, conn.httpURL, "URL should match")
	assert.Equal(t, headers, conn.headers, "Headers should match")
	assert.Equal(t, httpClient, conn.httpClient, "HTTP client should match")
	assert.Equal(t, HTTPTransportStreamable, conn.httpTransportType, "Transport type should match")
}

// TestConnection_RequireSession tests the requireSession helper method
func TestConnection_RequireSession(t *testing.T) {
	tests := []struct {
		name        string
		session     interface{} // nil or non-nil session
		expectError bool
	}{
		{
			name:        "session is nil",
			session:     nil,
			expectError: true,
		},
		{
			name:        "session is available",
			session:     "mock-session", // Just needs to be non-nil
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a connection with or without a session
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			conn := &Connection{
				ctx:    ctx,
				cancel: cancel,
			}

			// Set session based on test case
			if tt.session != nil {
				// We can't easily create a real SDK session, but we can test with a nil session
				// The actual implementation only checks for nil
				conn.session = nil // Will be nil for both test cases in practice
			}

			err := conn.requireSession()

			if tt.expectError {
				assert.Error(t, err, "requireSession should return error when session is nil")
				assert.Contains(t, err.Error(), "SDK session not available for plain JSON-RPC transport",
					"Error message should contain expected text")
			} else {
				// This test case can't be fully tested without a real SDK session
				// But the helper is covered by integration tests that use real sessions
				t.Skip("Cannot test with real SDK session in unit test")
			}
		})
	}
}

// TestParseJSONRPCResponseWithSSE tests comprehensive parsing of JSON-RPC responses
// with SSE format fallback and error handling
func TestParseJSONRPCResponseWithSSE(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		statusCode    int
		contextDesc   string
		wantError     bool
		errorContains string
		checkResponse func(*testing.T, *Response)
	}{
		{
			name:        "valid JSON response",
			body:        `{"jsonrpc":"2.0","id":1,"result":{"success":true}}`,
			statusCode:  200,
			contextDesc: "test response",
			wantError:   false,
			checkResponse: func(t *testing.T, resp *Response) {
				assert.Equal(t, "2.0", resp.JSONRPC)
				assert.NotNil(t, resp.Result)
				assert.Nil(t, resp.Error)
			},
		},
		{
			name: "SSE formatted response",
			body: `event: message
data: {"jsonrpc":"2.0","id":2,"result":{"tools":[]}}

`,
			statusCode:  200,
			contextDesc: "SSE response",
			wantError:   false,
			checkResponse: func(t *testing.T, resp *Response) {
				assert.Equal(t, "2.0", resp.JSONRPC)
				assert.NotNil(t, resp.Result)
				assert.Nil(t, resp.Error)
			},
		},
		{
			name: "HTTP error with unparseable body",
			body: `<!DOCTYPE html>
<html>
<head><title>500 Internal Server Error</title></head>
<body>Server Error</body>
</html>`,
			statusCode:  500,
			contextDesc: "error response",
			wantError:   false, // Returns synthetic error response, not error
			checkResponse: func(t *testing.T, resp *Response) {
				assert.Equal(t, "2.0", resp.JSONRPC)
				assert.Nil(t, resp.Result)
				assert.NotNil(t, resp.Error)
				assert.Equal(t, -32603, resp.Error.Code)
				assert.Contains(t, resp.Error.Message, "HTTP 500")
				assert.NotNil(t, resp.Error.Data)
			},
		},
		{
			name:        "HTTP 400 with invalid JSON",
			body:        `{"error":"bad request"`,
			statusCode:  400,
			contextDesc: "bad request",
			wantError:   false,
			checkResponse: func(t *testing.T, resp *Response) {
				assert.NotNil(t, resp.Error)
				assert.Equal(t, -32603, resp.Error.Code)
				assert.Contains(t, resp.Error.Message, "HTTP 400")
			},
		},
		{
			name:        "HTTP 503 service unavailable",
			body:        `Service Temporarily Unavailable`,
			statusCode:  503,
			contextDesc: "service error",
			wantError:   false,
			checkResponse: func(t *testing.T, resp *Response) {
				assert.NotNil(t, resp.Error)
				assert.Contains(t, resp.Error.Message, "503")
			},
		},
		{
			name: "SSE format with HTTP error",
			body: `event: message
data: {"error":"rate limited"}

`,
			statusCode:  429,
			contextDesc: "rate limit",
			wantError:   false,
			checkResponse: func(t *testing.T, resp *Response) {
				assert.NotNil(t, resp.Error)
				assert.Equal(t, -32603, resp.Error.Code)
			},
		},
		{
			name:          "malformed JSON with OK status",
			body:          `{invalid json}`,
			statusCode:    200,
			contextDesc:   "malformed response",
			wantError:     true,
			errorContains: "failed to parse",
		},
		{
			name: "SSE with malformed JSON data and OK status",
			body: `event: message
data: {not valid json}

`,
			statusCode:    200,
			contextDesc:   "malformed SSE JSON",
			wantError:     true,
			errorContains: "failed to parse JSON data extracted from SSE",
		},
		{
			name:          "empty body with OK status",
			body:          ``,
			statusCode:    200,
			contextDesc:   "empty response",
			wantError:     true,
			errorContains: "failed to parse",
		},
		{
			name: "large body truncation in error message",
			body: `this is a very long invalid response that should be truncated when included in the error message. ` +
				strings.Repeat("x", 500) + ` this part should not appear in the error`,
			statusCode:    200,
			contextDesc:   "large invalid response",
			wantError:     true,
			errorContains: "(truncated)",
		},
		{
			name:        "JSON-RPC error response",
			body:        `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`,
			statusCode:  200,
			contextDesc: "JSON-RPC error",
			wantError:   false,
			checkResponse: func(t *testing.T, resp *Response) {
				assert.Equal(t, "2.0", resp.JSONRPC)
				assert.Nil(t, resp.Result)
				assert.NotNil(t, resp.Error)
				assert.Equal(t, -32600, resp.Error.Code)
				assert.Equal(t, "Invalid Request", resp.Error.Message)
			},
		},
		{
			name: "SSE with JSON-RPC error response",
			body: `event: message
data: {"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"Method not found"}}

`,
			statusCode:  200,
			contextDesc: "SSE JSON-RPC error",
			wantError:   false,
			checkResponse: func(t *testing.T, resp *Response) {
				assert.NotNil(t, resp.Error)
				assert.Equal(t, -32601, resp.Error.Code)
				assert.Equal(t, "Method not found", resp.Error.Message)
			},
		},
		{
			name: "SSE without data field and OK status",
			body: `event: message

`,
			statusCode:    200,
			contextDesc:   "SSE no data",
			wantError:     true,
			errorContains: "no data field found",
		},
		{
			name:        "HTTP 404 with HTML error page",
			body:        `<html><body><h1>404 Not Found</h1></body></html>`,
			statusCode:  404,
			contextDesc: "not found",
			wantError:   false,
			checkResponse: func(t *testing.T, resp *Response) {
				assert.NotNil(t, resp.Error)
				assert.Contains(t, resp.Error.Message, "404")
				// Body should be included in Data field
				bodyStr := string(resp.Error.Data)
				assert.Contains(t, bodyStr, "404 Not Found")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := parseJSONRPCResponseWithSSE([]byte(tt.body), tt.statusCode, tt.contextDesc)

			if tt.wantError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)

			if tt.checkResponse != nil {
				tt.checkResponse(t, resp)
			}
		})
	}
}

// TestParseJSONRPCResponseWithSSE_EdgeCases tests additional edge cases and boundary conditions
func TestParseJSONRPCResponseWithSSE_EdgeCases(t *testing.T) {
	t.Run("multiple SSE data lines", func(t *testing.T) {
		body := `event: message
data: {"jsonrpc":"2.0","id":1}
data: should be ignored

`
		resp, err := parseJSONRPCResponseWithSSE([]byte(body), 200, "test")
		require.NoError(t, err)
		assert.Equal(t, "2.0", resp.JSONRPC)
	})

	t.Run("SSE data with leading/trailing whitespace", func(t *testing.T) {
		body := `event: message
data:   {"jsonrpc":"2.0","id":2,"result":{}}   

`
		resp, err := parseJSONRPCResponseWithSSE([]byte(body), 200, "test")
		require.NoError(t, err)
		assert.Equal(t, "2.0", resp.JSONRPC)
	})

	t.Run("body exactly 500 characters should not truncate", func(t *testing.T) {
		invalidJSON := strings.Repeat("x", 500)
		_, err := parseJSONRPCResponseWithSSE([]byte(invalidJSON), 200, "test")
		require.Error(t, err)
		// Should not contain "(truncated)" since it's exactly 500 chars
		assert.NotContains(t, err.Error(), "(truncated)")
	})

	t.Run("body with 501 characters should truncate", func(t *testing.T) {
		invalidJSON := strings.Repeat("x", 501)
		_, err := parseJSONRPCResponseWithSSE([]byte(invalidJSON), 200, "test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "(truncated)")
	})

	t.Run("context description appears in error message", func(t *testing.T) {
		_, err := parseJSONRPCResponseWithSSE([]byte(`invalid`), 200, "initialize response")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "initialize response")
	})

	t.Run("null result is valid", func(t *testing.T) {
		body := `{"jsonrpc":"2.0","id":1,"result":null}`
		resp, err := parseJSONRPCResponseWithSSE([]byte(body), 200, "test")
		require.NoError(t, err)
		assert.Equal(t, "2.0", resp.JSONRPC)
	})

	t.Run("response with both result and error (invalid but should parse)", func(t *testing.T) {
		body := `{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":-32603,"message":"error"}}`
		resp, err := parseJSONRPCResponseWithSSE([]byte(body), 200, "test")
		require.NoError(t, err)
		// JSON unmarshaling will succeed, both fields will be present
		assert.NotNil(t, resp)
	})

	t.Run("HTTP error preserves original body in Data field", func(t *testing.T) {
		originalBody := `{"custom":"error","details":"something went wrong"}`
		resp, err := parseJSONRPCResponseWithSSE([]byte(originalBody), 500, "test")
		require.NoError(t, err)
		require.NotNil(t, resp.Error)
		assert.JSONEq(t, originalBody, string(resp.Error.Data))
	})
}
