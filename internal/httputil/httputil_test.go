package httputil

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errorResponseWriter is a minimal http.ResponseWriter whose Write method
// returns a configurable error, allowing tests to exercise the write-failure
// and short-write branches of WriteJSONResponse.
type errorResponseWriter struct {
	headers    http.Header
	code       int
	writeErr   error // if non-nil, Write returns this error with n=0
	writeLimit int   // if > 0, Write returns at most this many bytes (simulates short write)
	written    int
}

func newErrorResponseWriter(writeErr error) *errorResponseWriter {
	return &errorResponseWriter{headers: make(http.Header), writeErr: writeErr}
}

func newShortResponseWriter(limit int) *errorResponseWriter {
	return &errorResponseWriter{headers: make(http.Header), writeLimit: limit}
}

func (m *errorResponseWriter) Header() http.Header  { return m.headers }
func (m *errorResponseWriter) WriteHeader(code int) { m.code = code }
func (m *errorResponseWriter) Write(p []byte) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	if m.writeLimit > 0 {
		n := len(p)
		if m.written+n > m.writeLimit {
			n = m.writeLimit - m.written
		}
		m.written += n
		return n, nil
	}
	m.written += len(p)
	return len(p), nil
}

func TestWriteJSONResponse(t *testing.T) {
	t.Run("sets content-type to application/json", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, map[string]string{"key": "value"})

		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	})

	t.Run("writes the provided status code", func(t *testing.T) {
		tests := []struct {
			name       string
			statusCode int
		}{
			{"200 OK", http.StatusOK},
			{"201 Created", http.StatusCreated},
			{"400 Bad Request", http.StatusBadRequest},
			{"404 Not Found", http.StatusNotFound},
			{"500 Internal Server Error", http.StatusInternalServerError},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				WriteJSONResponse(rec, tt.statusCode, nil)

				assert.Equal(t, tt.statusCode, rec.Code)
			})
		}
	})

	t.Run("encodes body as JSON", func(t *testing.T) {
		type payload struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, payload{Name: "test", Count: 42})

		var got payload
		err := json.NewDecoder(rec.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, "test", got.Name)
		assert.Equal(t, 42, got.Count)
	})

	t.Run("encodes map body as JSON", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, map[string]interface{}{
			"error":   "not found",
			"code":    404,
			"details": []string{"a", "b"},
		})

		var got map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, "not found", got["error"])
		assert.Equal(t, float64(404), got["code"])
		assert.Len(t, got["details"], 2)
	})

	t.Run("encodes nil body as JSON null", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, nil)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, "null", rec.Body.String())
	})

	t.Run("encodes empty struct as empty JSON object", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, struct{}{})

		assert.JSONEq(t, "{}", rec.Body.String())
	})

	t.Run("encodes slice body as JSON array", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, []string{"alpha", "beta"})

		var got []string
		err := json.NewDecoder(rec.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, []string{"alpha", "beta"}, got)
	})

	t.Run("encodes nested structs", func(t *testing.T) {
		type inner struct {
			ID int `json:"id"`
		}
		type outer struct {
			Items []inner `json:"items"`
		}
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, outer{Items: []inner{{ID: 1}, {ID: 2}}})

		var got outer
		err := json.NewDecoder(rec.Body).Decode(&got)
		require.NoError(t, err)
		require.Len(t, got.Items, 2)
		assert.Equal(t, 1, got.Items[0].ID)
		assert.Equal(t, 2, got.Items[1].ID)
	})

	t.Run("body with special characters", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, map[string]string{
			"msg": `hello "world" & <friends>`,
		})

		var got map[string]string
		err := json.NewDecoder(rec.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, `hello "world" & <friends>`, got["msg"])
	})

	t.Run("body with unicode", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, map[string]string{
			"greeting": "こんにちは 🌍",
		})

		var got map[string]string
		err := json.NewDecoder(rec.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, "こんにちは 🌍", got["greeting"])
	})

	t.Run("marshal failure writes headers but no body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		// Channels cannot be marshaled to JSON; json.Marshal returns an error.
		WriteJSONResponse(rec, http.StatusInternalServerError, make(chan int))

		// Content-Type and status code are committed before the marshal attempt,
		// so they are still present even when encoding fails.
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		// No body is written when encoding fails.
		assert.Empty(t, rec.Body.String())
	})

	t.Run("write error does not panic", func(t *testing.T) {
		w := newErrorResponseWriter(errors.New("write: broken pipe"))
		// WriteJSONResponse should handle the write error gracefully without panicking.
		require.NotPanics(t, func() {
			WriteJSONResponse(w, http.StatusOK, map[string]string{"key": "value"})
		})
		assert.Equal(t, "application/json", w.headers.Get("Content-Type"))
		assert.Equal(t, http.StatusOK, w.code)
		// No bytes were accepted by the writer.
		assert.Equal(t, 0, w.written)
	})

	t.Run("short write does not panic", func(t *testing.T) {
		// Allow only 1 byte to be written, forcing a short write.
		w := newShortResponseWriter(1)
		require.NotPanics(t, func() {
			WriteJSONResponse(w, http.StatusOK, map[string]string{"key": "value"})
		})
		assert.Equal(t, "application/json", w.headers.Get("Content-Type"))
		assert.Equal(t, http.StatusOK, w.code)
		// Only the limited number of bytes were accepted.
		assert.Equal(t, 1, w.written)
	})
}

// TestParseRateLimitResetHeader verifies the shared Unix-timestamp header parser.
func TestParseRateLimitResetHeader(t *testing.T) {
	t.Parallel()

	now := time.Now()
	future := now.Add(60 * time.Second)

	tests := []struct {
		name     string
		value    string
		wantZero bool
		wantTime time.Time
	}{
		{
			name:     "empty",
			value:    "",
			wantZero: true,
		},
		{
			name:     "invalid",
			value:    "not-a-number",
			wantZero: true,
		},
		{
			name:     "valid unix timestamp",
			value:    "1000000000",
			wantZero: false,
			wantTime: time.Unix(1000000000, 0),
		},
		{
			name:     "future timestamp",
			value:    strconv.FormatInt(future.Unix(), 10),
			wantZero: false,
		},
		{
			name:     "value with surrounding whitespace",
			value:    "  1000000000  ",
			wantZero: false,
			wantTime: time.Unix(1000000000, 0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseRateLimitResetHeader(tt.value)
			if tt.wantZero {
				assert.True(t, got.IsZero(), "expected zero time")
			} else {
				assert.False(t, got.IsZero(), "expected non-zero time")
				if !tt.wantTime.IsZero() {
					assert.Equal(t, tt.wantTime.Unix(), got.Unix())
				}
			}
		})
	}
}

// TestApplyGitHubAPIHeaders verifies that ApplyGitHubAPIHeaders sets the
// expected headers on an HTTP request.
func TestApplyGitHubAPIHeaders(t *testing.T) {
	t.Run("sets Authorization when authHeader is non-empty", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
		require.NoError(t, err)

		ApplyGitHubAPIHeaders(req, "token my-secret")

		assert.Equal(t, "token my-secret", req.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", req.Header.Get("Accept"))
		assert.Equal(t, GitHubUserAgent, req.Header.Get("User-Agent"))
	})

	t.Run("does not set Authorization when authHeader is empty", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
		require.NoError(t, err)

		ApplyGitHubAPIHeaders(req, "")

		assert.Empty(t, req.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", req.Header.Get("Accept"))
		assert.Equal(t, GitHubUserAgent, req.Header.Get("User-Agent"))
	})

	t.Run("works with Bearer token scheme", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
		require.NoError(t, err)

		ApplyGitHubAPIHeaders(req, "Bearer ghp_abc123")

		assert.Equal(t, "Bearer ghp_abc123", req.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", req.Header.Get("Accept"))
		assert.Equal(t, GitHubUserAgent, req.Header.Get("User-Agent"))
	})
}
