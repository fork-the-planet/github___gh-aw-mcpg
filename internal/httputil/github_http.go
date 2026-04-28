package httputil

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

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
