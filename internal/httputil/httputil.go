// Package httputil provides shared HTTP helper utilities used across multiple
// HTTP-facing packages (server, proxy, etc.).
package httputil

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logHTTP = logger.New("httputil:httputil")

// WriteJSONResponse sets the Content-Type header, writes the status code, and encodes
// body as JSON. It centralises the three-line pattern used across HTTP handlers.
func WriteJSONResponse(w http.ResponseWriter, statusCode int, body interface{}) {
	logHTTP.Printf("Writing JSON response: statusCode=%d", statusCode)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	data, err := json.Marshal(body)
	if err != nil {
		logHTTP.Printf("Failed to marshal JSON response body: %v", err)
		return
	}
	logHTTP.Printf("JSON response body size: %d bytes", len(data))
	n, err := w.Write(data)
	if err != nil {
		logHTTP.Printf("Failed to write JSON response body: wrote=%d expected=%d err=%v", n, len(data), err)
		return
	}
	if n != len(data) {
		logHTTP.Printf("Short write for JSON response body: wrote=%d expected=%d", n, len(data))
	}
}

// ParseRateLimitResetHeader parses the Unix-timestamp value of the
// X-RateLimit-Reset HTTP header into a time.Time.
// Returns zero time when the header value is absent or malformed.
func ParseRateLimitResetHeader(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	unix, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		logHTTP.Printf("Failed to parse X-RateLimit-Reset header value=%q: %v", value, err)
		return time.Time{}
	}
	reset := time.Unix(unix, 0)
	logHTTP.Printf("Parsed X-RateLimit-Reset: resetAt=%s", reset.UTC().Format(time.RFC3339))
	return reset
}

// GitHubUserAgent is the User-Agent header value sent on all GitHub API requests.
const GitHubUserAgent = "awmg/1.0"

// ApplyGitHubAPIHeaders sets the standard GitHub API request headers on req.
// authHeader should be the full Authorization header value (e.g. "token xyz" or
// "Bearer xyz"). When authHeader is empty no Authorization header is set, which
// is appropriate when the caller has already decided that no auth is available.
func ApplyGitHubAPIHeaders(req *http.Request, authHeader string) {
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", GitHubUserAgent)
}

// IsTransientHTTPError returns true for status codes that indicate a temporary
// server-side condition (rate-limiting or transient failure) worth retrying.
func IsTransientHTTPError(statusCode int) bool {
	transient := statusCode == http.StatusTooManyRequests ||
		statusCode == http.StatusServiceUnavailable ||
		(statusCode >= 500 && statusCode < 600)
	if transient {
		logHTTP.Printf("Transient HTTP error detected: statusCode=%d", statusCode)
	}
	return transient
}
