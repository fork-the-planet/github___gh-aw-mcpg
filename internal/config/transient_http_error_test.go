package config

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsTransientHTTPError verifies every status-code branch in isTransientHTTPError.
// The function returns true for HTTP 429 (TooManyRequests), 503 (ServiceUnavailable),
// and any 5xx status code, and false for all other codes.
func TestIsTransientHTTPError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		// True cases — rate limiting
		{
			name:       "429 Too Many Requests is transient",
			statusCode: http.StatusTooManyRequests,
			want:       true,
		},
		// True cases — service unavailable (also in 5xx range but named explicitly)
		{
			name:       "503 Service Unavailable is transient",
			statusCode: http.StatusServiceUnavailable,
			want:       true,
		},
		// True cases — full 5xx range
		{
			name:       "500 Internal Server Error is transient",
			statusCode: http.StatusInternalServerError,
			want:       true,
		},
		{
			name:       "501 Not Implemented is transient",
			statusCode: http.StatusNotImplemented,
			want:       true,
		},
		{
			name:       "502 Bad Gateway is transient",
			statusCode: http.StatusBadGateway,
			want:       true,
		},
		{
			name:       "504 Gateway Timeout is transient",
			statusCode: http.StatusGatewayTimeout,
			want:       true,
		},
		{
			name:       "599 (max 5xx) is transient",
			statusCode: 599,
			want:       true,
		},
		// False cases — successful responses
		{
			name:       "200 OK is not transient",
			statusCode: http.StatusOK,
			want:       false,
		},
		{
			name:       "201 Created is not transient",
			statusCode: http.StatusCreated,
			want:       false,
		},
		{
			name:       "204 No Content is not transient",
			statusCode: http.StatusNoContent,
			want:       false,
		},
		// False cases — redirects
		{
			name:       "301 Moved Permanently is not transient",
			statusCode: http.StatusMovedPermanently,
			want:       false,
		},
		// False cases — client errors (non-429)
		{
			name:       "400 Bad Request is not transient",
			statusCode: http.StatusBadRequest,
			want:       false,
		},
		{
			name:       "401 Unauthorized is not transient",
			statusCode: http.StatusUnauthorized,
			want:       false,
		},
		{
			name:       "403 Forbidden is not transient",
			statusCode: http.StatusForbidden,
			want:       false,
		},
		{
			name:       "404 Not Found is not transient",
			statusCode: http.StatusNotFound,
			want:       false,
		},
		{
			name:       "422 Unprocessable Entity is not transient",
			statusCode: http.StatusUnprocessableEntity,
			want:       false,
		},
		// Boundary: 499 is not transient (last 4xx)
		{
			name:       "499 is not transient",
			statusCode: 499,
			want:       false,
		},
		// Boundary: 600 is not transient (above 5xx)
		{
			name:       "600 is not transient",
			statusCode: 600,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isTransientHTTPError(tt.statusCode)
			assert.Equal(t, tt.want, got, "isTransientHTTPError(%d)", tt.statusCode)
		})
	}
}
