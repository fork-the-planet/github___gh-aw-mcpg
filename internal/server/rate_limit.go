package server

import (
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw-mcpg/internal/mcpresult"
)

// extractRateLimitErrorText extracts the text content from a raw tool result
// that has been identified as a rate-limit error. Returns the original backend
// message so agents see the actual upstream error rather than a synthetic one.
func extractRateLimitErrorText(result interface{}) string {
	m, ok := result.(map[string]interface{})
	if !ok {
		logCircuitBreaker.Print("extractRateLimitErrorText: result is not a map, using default message")
		return "rate limit exceeded"
	}
	if text := mcpresult.ExtractTextContent(m); text != "" {
		return text
	}
	logCircuitBreaker.Print("extractRateLimitErrorText: no text content found, using default message")
	return "rate limit exceeded"
}

// isRateLimitToolResult reports whether a raw tool call result indicates
// a rate-limit error from the GitHub MCP server. It inspects the `isError`
// flag and the text content for well-known rate-limit phrases.
//
// The GitHub MCP server returns rate-limit errors as:
//
//	{"content":[{"type":"text","text":"... 403 API rate limit exceeded ..."}],"isError":true}
func isRateLimitToolResult(result interface{}) (bool, time.Time) {
	m, ok := result.(map[string]interface{})
	if !ok {
		return false, time.Time{}
	}

	// Only inspect error results.
	isErr, _ := m["isError"].(bool)
	if !isErr {
		return false, time.Time{}
	}

	text := mcpresult.ExtractTextContent(m)
	if isRateLimitText(text) {
		resetAt := parseRateLimitResetFromText(text)
		logCircuitBreaker.Printf("Rate limit detected in tool result: hasResetAt=%v", !resetAt.IsZero())
		return true, resetAt
	}
	return false, time.Time{}
}

// isRateLimitText returns true when the message indicates a GitHub rate-limit error.
func isRateLimitText(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "rate limit exceeded") ||
		(strings.Contains(lower, "rate limit") && strings.Contains(lower, "403")) ||
		strings.Contains(lower, "api rate limit") ||
		strings.Contains(lower, "secondary rate limit") ||
		strings.Contains(lower, "too many requests")
}

// parseRateLimitResetFromText attempts to extract a reset timestamp from the
// rate-limit error text. The GitHub MCP server includes messages like
// "API rate limit exceeded [rate reset in 42s]".
// Returns zero time when the value cannot be parsed or is 0 seconds.
//
// See also: httputil.ParseRateLimitResetHeader in httputil/github_http.go, which
// parses the same timing information from the X-RateLimit-Reset HTTP response header
// instead of MCP tool result text bodies.
func parseRateLimitResetFromText(text string) time.Time {
	// Look for "[rate reset in Ns]" pattern.
	lower := strings.ToLower(text)
	idx := strings.Index(lower, "rate reset in ")
	if idx < 0 {
		logCircuitBreaker.Print("parseRateLimitResetFromText: no reset time pattern found in text")
		return time.Time{}
	}
	rest := text[idx+len("rate reset in "):]
	// Find the first non-digit character.
	end := strings.IndexAny(rest, "s])")
	if end < 0 {
		return time.Time{}
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(rest[:end]), 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}
	}
	resetAt := time.Now().Add(time.Duration(secs) * time.Second)
	logCircuitBreaker.Printf("Parsed rate limit reset time from text: resetIn=%ds, resetAt=%s", secs, resetAt.UTC().Format(time.RFC3339))
	return resetAt
}
