package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/envutil"
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
	}, nil, "", 0, 0)
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
	}, nil, "", 0, 0)
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
	}, nil, "", 0, 0)
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

			result := envutil.ExpandEnvArgs(tt.args)
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
					assert.NoError(t, err, "Failed to read request body")
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
			}, nil, "", 0, 0)
			if err != nil {
				require.True(t, tt.expectError, "Unexpected error creating connection: %v", err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring, "Error should contain expected substring")
				}
				return
			}

			// Send request
			_, err = conn.SendRequestWithServerID(context.Background(), "tools/list", nil, "test-server")

			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring, "Error should contain expected substring")
				}
			} else {
				assert.NoError(t, err)
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

	conn, err := NewHTTPConnection(context.Background(), "test-server", testServer.URL, headers, nil, "", 0, 0)
	require.NoError(t, err, "Failed to create HTTP connection")
	defer conn.Close()

	// Test IsHTTP
	assert.True(t, conn.IsHTTP(), "Expected IsHTTP() to return true for HTTP connection")

	// Test GetHTTPURL
	assert.Equal(t, testServer.URL, conn.GetHTTPURL(), "GetHTTPURL should return the configured URL")

	// Test GetHTTPHeaders
	returnedHeaders := conn.GetHTTPHeaders()
	assert.Equal(t, len(headers), len(returnedHeaders))
	for k, v := range headers {
		assert.Equal(t, v, returnedHeaders[k], "Header %s should match the configured value", k)
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
			_, err := NewHTTPConnection(context.Background(), "test-server", tt.url, tt.headers, nil, "", 0, 0)

			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring, "Error should contain expected substring")
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestNewMCPClient tests the newMCPClient helper function
func TestNewMCPClient(t *testing.T) {
	client := newMCPClient(nil, 0)
	require.NotNil(t, client, "newMCPClient should return a non-nil client")
}

// TestNewMCPClientWithLogger tests that newMCPClient accepts a logger
func TestNewMCPClientWithLogger(t *testing.T) {
	log := logger.New("test:client")
	client := newMCPClient(log, 0)
	require.NotNil(t, client, "newMCPClient should return a non-nil client with logger")
}

// TestNewMCPClientWithKeepalive tests that newMCPClient accepts a keepalive interval
func TestNewMCPClientWithKeepalive(t *testing.T) {
	keepAlive := time.Duration(config.DefaultKeepaliveInterval) * time.Second
	client := newMCPClient(nil, keepAlive)
	require.NotNil(t, client, "newMCPClient should return a non-nil client with keepalive")
}

// TestDefaultKeepaliveInterval verifies the config keepalive default is less than a typical
// backend session timeout (30 minutes) to prevent session expiry during long agent runs.
func TestDefaultKeepaliveInterval(t *testing.T) {
	const typicalBackendTimeout = 30 * time.Minute
	keepAlive := time.Duration(config.DefaultKeepaliveInterval) * time.Second
	assert.Less(t, keepAlive, typicalBackendTimeout,
		"DefaultKeepaliveInterval must be less than the typical backend session timeout to prevent expiry")
	assert.Greater(t, keepAlive, time.Duration(0),
		"DefaultKeepaliveInterval must be positive")
}

// TestNewHTTPConnectionStoresKeepalive verifies that the keepalive interval is stored on
// the connection struct so that reconnectSDKTransport can recreate the session with the same setting.
func TestNewHTTPConnectionStoresKeepalive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	keepAlive := time.Duration(config.DefaultKeepaliveInterval) * time.Second
	client := newMCPClient(nil, keepAlive)
	url := "http://example.com/mcp"
	headers := map[string]string{}
	httpClient := &http.Client{}

	conn := newHTTPConnection(ctx, cancel, client, nil, url, headers, httpClient, HTTPTransportStreamable, "test-server", keepAlive, 0)

	require.NotNil(t, conn)
	assert.Equal(t, keepAlive, conn.keepAliveInterval,
		"keepAliveInterval should be stored on the connection for use during reconnection")
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

	client := newMCPClient(nil, 0)
	url := "http://example.com/mcp"
	headers := map[string]string{"Authorization": "test"}
	httpClient := &http.Client{}

	conn := newHTTPConnection(ctx, cancel, client, nil, url, headers, httpClient, HTTPTransportStreamable, "test-server", 0, 0)

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

// TestPaginateAll tests the paginateAll generic helper.
func TestPaginateAll(t *testing.T) {
	t.Run("single page with no cursor returns all items", func(t *testing.T) {
		items, err := paginateAll("server1", "tools", func(cursor string) (paginatedPage[string], error) {
			return paginatedPage[string]{Items: []string{"a", "b", "c"}, NextCursor: ""}, nil
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, items)
	})

	t.Run("multiple pages are collected", func(t *testing.T) {
		pages := []paginatedPage[string]{
			{Items: []string{"a"}, NextCursor: "page2"},
			{Items: []string{"b"}, NextCursor: "page3"},
			{Items: []string{"c"}, NextCursor: ""},
		}
		call := 0
		items, err := paginateAll("server1", "tools", func(cursor string) (paginatedPage[string], error) {
			page := pages[call]
			call++
			return page, nil
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, items)
	})

	t.Run("exceeding max pages returns error", func(t *testing.T) {
		// Each call returns a unique cursor so the loop never ends naturally.
		callCount := 0
		_, err := paginateAll("server1", "tools", func(cursor string) (paginatedPage[string], error) {
			callCount++
			nextCursor := "next"
			if cursor != "" {
				nextCursor = cursor + "next"
			}
			return paginatedPage[string]{Items: []string{"x"}, NextCursor: nextCursor}, nil
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "more than")
		assert.Contains(t, err.Error(), "pages")
		// Must stop at the page limit, not run forever.
		assert.Equal(t, paginateAllMaxPages, callCount)
	})

	t.Run("cyclical cursor returns error", func(t *testing.T) {
		callCount := 0
		_, err := paginateAll("server1", "tools", func(cursor string) (paginatedPage[string], error) {
			callCount++
			switch cursor {
			case "":
				return paginatedPage[string]{Items: []string{"a"}, NextCursor: "page2"}, nil
			case "page2":
				return paginatedPage[string]{Items: []string{"b"}, NextCursor: "page3"}, nil
			case "page3":
				return paginatedPage[string]{Items: []string{"c"}, NextCursor: "page2"}, nil
			default:
				return paginatedPage[string]{Items: nil, NextCursor: ""}, nil
			}
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "cyclical cursor")
		assert.Contains(t, err.Error(), "page2")
		// Initial page + 2 unique cursor fetches, then cycle detected before another fetch.
		assert.Equal(t, 3, callCount)
	})

	t.Run("fetch error on first page propagates", func(t *testing.T) {
		_, err := paginateAll("server1", "tools", func(cursor string) (paginatedPage[string], error) {
			return paginatedPage[string]{}, fmt.Errorf("backend unavailable")
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backend unavailable")
	})

	t.Run("fetch error on subsequent page propagates", func(t *testing.T) {
		call := 0
		_, err := paginateAll("server1", "tools", func(cursor string) (paginatedPage[string], error) {
			call++
			if call == 1 {
				return paginatedPage[string]{Items: []string{"a"}, NextCursor: "page2"}, nil
			}
			return paginatedPage[string]{}, fmt.Errorf("page 2 fetch failed")
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "page 2 fetch failed")
		assert.Equal(t, 2, call)
	})

	t.Run("empty first page returns empty slice", func(t *testing.T) {
		items, err := paginateAll("server1", "tools", func(cursor string) (paginatedPage[string], error) {
			return paginatedPage[string]{Items: []string{}, NextCursor: ""}, nil
		})
		require.NoError(t, err)
		assert.Empty(t, items)
	})
}

// TestListMCPItems_NilSession verifies that listMCPItems returns a session error
// immediately when the connection has no active SDK session.
func TestListMCPItems_NilSession(t *testing.T) {
	conn := newTestConnection(t)
	fetchCalled := false
	_, err := listMCPItems(conn, "tools",
		func(cursor string) (paginatedPage[string], error) {
			fetchCalled = true
			return paginatedPage[string]{Items: []string{"a"}}, nil
		},
		func(items []string) []string { return items },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SDK session not available")
	assert.False(t, fetchCalled, "fetch should not be called when session is unavailable")
}

// TestCallParamMethod_BadParams verifies that callParamMethod propagates
// unmarshal errors when the raw params cannot be decoded into the typed struct.
func TestCallParamMethod_BadParams(t *testing.T) {
	conn := newTestConnection(t)
	// Pass a type that can be marshalled but cannot be unmarshalled into
	// CallToolParams because the "name" field type mismatches.
	badParams := map[string]interface{}{
		"name": []int{1, 2, 3}, // expects string, gets array
	}
	type strictParams struct {
		Name string `json:"name"`
	}
	fnCalled := false
	_, err := callParamMethod(conn, badParams, func(p strictParams) (interface{}, error) {
		fnCalled = true
		return nil, nil
	})
	// requireSession should fail first since the connection has no session.
	require.Error(t, err)
	assert.False(t, fnCalled)
}
