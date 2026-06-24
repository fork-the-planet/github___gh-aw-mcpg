package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Speed up all retry-related tests by disabling inter-attempt delays and
	// shortening the per-attempt HTTP timeout so timeout tests don't take 30 s.
	schemaFetchRetryDelay = 0
	schemaHTTPClientTimeout = 200 * time.Millisecond
}

// TestFetchSchema_SuccessfulFetch tests the happy path where schema is fetched successfully
func TestFetchSchema_SuccessfulFetch(t *testing.T) {
	// Create a minimal valid schema for testing
	validSchema := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"$id":     "https://example.com/schema.json",
		"type":    "object",
		"properties": map[string]interface{}{
			"test": map[string]interface{}{
				"type": "string",
			},
		},
	}

	schemaJSON, err := json.Marshal(validSchema)
	require.NoError(t, err)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(schemaJSON)
	}))
	defer server.Close()

	// Test fetching from the server
	result, err := fetchSchema(server.URL)

	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify the result is valid JSON
	var parsed map[string]interface{}
	err = json.Unmarshal(result, &parsed)
	assert.NoError(t, err)
}

// TestFetchSchema_HTTPError tests handling of HTTP error responses
func TestFetchSchema_HTTPError(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		wantErr      string
		wantRequests int // expected number of HTTP requests (1 = no retry, 3 = full retry)
	}{
		{
			name:         "404 Not Found",
			statusCode:   http.StatusNotFound,
			wantErr:      "failed to fetch schema: HTTP 404",
			wantRequests: 1, // permanent error, no retry
		},
		{
			name:         "500 Internal Server Error",
			statusCode:   http.StatusInternalServerError,
			wantErr:      "failed to fetch schema: HTTP 500",
			wantRequests: maxSchemaFetchRetries, // transient, retried
		},
		{
			name:         "403 Forbidden",
			statusCode:   http.StatusForbidden,
			wantErr:      "failed to fetch schema: HTTP 403",
			wantRequests: 1, // permanent error, no retry
		},
		{
			name:         "503 Service Unavailable",
			statusCode:   http.StatusServiceUnavailable,
			wantErr:      "failed to fetch schema: HTTP 503",
			wantRequests: maxSchemaFetchRetries, // transient, retried
		},
		{
			name:         "429 Too Many Requests",
			statusCode:   http.StatusTooManyRequests,
			wantErr:      "failed to fetch schema: HTTP 429",
			wantRequests: maxSchemaFetchRetries, // transient, retried
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount.Add(1)
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			result, err := fetchSchema(server.URL)

			assert.Error(t, err)
			assert.Nil(t, result)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.Equal(t, int32(tt.wantRequests), requestCount.Load(),
				"expected %d HTTP request(s) for status %d", tt.wantRequests, tt.statusCode)
		})
	}
}

// TestFetchSchema_NetworkError tests handling of network failures
func TestFetchSchema_NetworkError(t *testing.T) {
	// Start a server and immediately close it so connections are refused,
	// guaranteeing a network error without relying on external DNS resolution.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := server.URL + "/schema.json"
	server.Close()

	result, err := fetchSchema(closedURL)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "failed to fetch schema from")
}

// TestFetchSchema_Timeout tests handling of request timeouts
func TestFetchSchema_Timeout(t *testing.T) {
	// Create a server that delays longer than the configured client timeout.
	// The init() in this test file sets schemaHTTPClientTimeout = 200ms, so any
	// delay > 200ms will trigger a timeout on each attempt.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result, err := fetchSchema(server.URL)

	assert.Error(t, err)
	assert.Nil(t, result)
	// The error should indicate a timeout or context deadline exceeded
	assert.ErrorContains(t, err, "failed to fetch schema from")
}

// TestFetchSchema_InvalidJSON tests that invalid JSON response bytes are returned as-is.
// JSON parsing is the caller's responsibility: validateAgainstCustomSchema calls
// jsonschema.UnmarshalJSON on the result, and validateJSONSchema does the same via
// getOrCompileSchema. Those callers will produce an appropriate error for bad JSON.
func TestFetchSchema_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json {{{"))
	}))
	defer server.Close()

	result, err := fetchSchema(server.URL)

	// fetchSchema returns raw bytes; JSON validation is the caller's responsibility
	assert.NoError(t, err)
	assert.Equal(t, []byte("not valid json {{{"), result)
}

// TestFetchSchema_EmptyResponse tests that an empty response body is returned as-is
func TestFetchSchema_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Send empty body
	}))
	defer server.Close()

	result, err := fetchSchema(server.URL)

	// fetchSchema returns raw bytes; an empty response is returned without error
	assert.NoError(t, err)
	assert.Equal(t, []byte{}, result)
}

// TestFetchSchema_NoFixesNeeded tests that schemas without problematic patterns are unchanged
func TestFetchSchema_NoFixesNeeded(t *testing.T) {
	// Schema without negative lookahead patterns
	cleanSchema := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type": "string",
			},
			"age": map[string]interface{}{
				"type": "integer",
			},
		},
	}

	schemaJSON, err := json.Marshal(cleanSchema)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(schemaJSON)
	}))
	defer server.Close()

	result, err := fetchSchema(server.URL)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Parse the result
	var fixed map[string]interface{}
	err = json.Unmarshal(result, &fixed)
	require.NoError(t, err)

	// Verify basic structure is preserved
	assert.Equal(t, "http://json-schema.org/draft-07/schema#", fixed["$schema"])
	assert.Equal(t, "object", fixed["type"])

	properties, ok := fixed["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, properties, "name")
	assert.Contains(t, properties, "age")
}

// TestFetchSchema_NestedStructurePreserved tests that nested schema structures are preserved
func TestFetchSchema_NestedStructurePreserved(t *testing.T) {
	// Complex nested schema
	complexSchema := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"definitions": map[string]interface{}{
			"address": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"street": map[string]interface{}{"type": "string"},
					"city":   map[string]interface{}{"type": "string"},
				},
			},
		},
		"properties": map[string]interface{}{
			"person": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
					"address": map[string]interface{}{
						"$ref": "#/definitions/address",
					},
				},
			},
		},
	}

	schemaJSON, err := json.Marshal(complexSchema)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(schemaJSON)
	}))
	defer server.Close()

	result, err := fetchSchema(server.URL)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Parse and verify structure is preserved
	var fixed map[string]interface{}
	err = json.Unmarshal(result, &fixed)
	require.NoError(t, err)

	// Verify definitions are preserved
	definitions, ok := fixed["definitions"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, definitions, "address")

	// Verify properties are preserved
	properties, ok := fixed["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, properties, "person")

	// Verify nested structure
	person, ok := properties["person"].(map[string]interface{})
	require.True(t, ok)
	personProps, ok := person["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, personProps, "name")
	assert.Contains(t, personProps, "address")
}

// TestFetchSchema_MarshalError tests handling of marshal failures
func TestFetchSchema_MarshalError(t *testing.T) {
	// This test verifies that if we somehow get an unmarshalable schema,
	// we handle it gracefully. In practice, this is hard to trigger since
	// we're marshaling a map[string]interface{}, but it's good to test the error path.

	// For now, we test that a valid schema doesn't cause marshal errors
	validSchema := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
	}

	schemaJSON, err := json.Marshal(validSchema)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(schemaJSON)
	}))
	defer server.Close()

	result, err := fetchSchema(server.URL)

	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestFetchSchema_HTTPMethodUsed tests that GET method is used
func TestFetchSchema_HTTPMethodUsed(t *testing.T) {
	var requestMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMethod = r.Method
		schemaJSON := []byte(`{"$schema":"http://json-schema.org/draft-07/schema#"}`)
		w.WriteHeader(http.StatusOK)
		w.Write(schemaJSON)
	}))
	defer server.Close()

	_, err := fetchSchema(server.URL)

	assert.NoError(t, err)
	assert.Equal(t, "GET", requestMethod, "Should use GET method")
}

// TestFetchSchema_UserAgentAndHeaders tests HTTP request headers
func TestFetchSchema_UserAgentAndHeaders(t *testing.T) {
	var headers http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		schemaJSON := []byte(`{"$schema":"http://json-schema.org/draft-07/schema#"}`)
		w.WriteHeader(http.StatusOK)
		w.Write(schemaJSON)
	}))
	defer server.Close()

	_, err := fetchSchema(server.URL)

	assert.NoError(t, err)
	assert.NotNil(t, headers, "Should have captured request headers")
	// Verify Go's default User-Agent is present
	userAgent := headers.Get("User-Agent")
	assert.NotEmpty(t, userAgent, "Should have User-Agent header")
	assert.Contains(t, userAgent, "Go-http-client", "Should use Go HTTP client")
}

// TestFetchSchema_LargeSchema tests handling of large schema documents
func TestFetchSchema_LargeSchema(t *testing.T) {
	// Create a large schema with many properties
	largeSchema := map[string]interface{}{
		"$schema":    "http://json-schema.org/draft-07/schema#",
		"type":       "object",
		"properties": map[string]interface{}{},
	}

	// Add 100 properties to make it larger
	props := largeSchema["properties"].(map[string]interface{})
	for i := 0; i < 100; i++ {
		props[fmt.Sprintf("field%d", i)] = map[string]interface{}{
			"type":        "string",
			"description": fmt.Sprintf("This is field number %d with some description", i),
		}
	}

	schemaJSON, err := json.Marshal(largeSchema)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(schemaJSON)
	}))
	defer server.Close()

	result, err := fetchSchema(server.URL)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the large schema was processed correctly
	var fixed map[string]interface{}
	err = json.Unmarshal(result, &fixed)
	require.NoError(t, err)

	properties, ok := fixed["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Len(t, properties, 100, "Should preserve all 100 properties")
}

func TestFetchSchema_TooLargeSchema(t *testing.T) {
	originalTimeout := schemaHTTPClientTimeout
	schemaHTTPClientTimeout = 5 * time.Second
	t.Cleanup(func() {
		schemaHTTPClientTimeout = originalTimeout
	})

	payload := strings.Repeat("a", maxSchemaFetchBytes+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()

	result, err := fetchSchema(server.URL)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "schema response too large")
}

// TestFetchSchema_RetrySucceedsAfterTransientError verifies that a transient
// error on the first attempt is retried and the eventual success is returned.
func TestFetchSchema_RetrySucceedsAfterTransientError(t *testing.T) {
	validSchema := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
	}
	schemaJSON, err := json.Marshal(validSchema)
	require.NoError(t, err)

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		if n < 3 {
			// First two attempts return 429
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(schemaJSON)
	}))
	defer server.Close()

	result, err := fetchSchema(server.URL)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int32(3), requestCount.Load(), "should have made 3 requests (2 failures + 1 success)")
}

// TestFetchSchema_ExponentialBackoffDelays verifies that all retries are attempted
// when transient errors occur, and that the function eventually gives up after
// maxSchemaFetchRetries attempts.
func TestFetchSchema_ExponentialBackoffDelays(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := fetchSchema(server.URL)

	require.Error(t, err)
	assert.Equal(t, int32(maxSchemaFetchRetries), requestCount.Load(),
		"should make exactly maxSchemaFetchRetries requests before giving up")
	assert.ErrorContains(t, err, "failed to fetch schema: HTTP 429")
}
