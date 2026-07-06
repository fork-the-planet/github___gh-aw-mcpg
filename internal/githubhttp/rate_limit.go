package githubhttp

import (
	"strconv"
	"strings"
	"time"
)

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

// ParseRateLimitResetFromText attempts to extract a reset timestamp from
// GitHub rate-limit error text such as "API rate limit exceeded [rate reset in 42s]".
// Returns zero time when the value cannot be parsed or is 0 seconds.
func ParseRateLimitResetFromText(text string) time.Time {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, "rate reset in ")
	if idx < 0 {
		logHTTP.Print("ParseRateLimitResetFromText: no reset time pattern found in text")
		return time.Time{}
	}
	rest := text[idx+len("rate reset in "):]
	end := strings.IndexAny(rest, "s])")
	if end < 0 {
		return time.Time{}
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(rest[:end]), 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}
	}
	resetAt := time.Now().Add(time.Duration(secs) * time.Second)
	logHTTP.Printf("Parsed rate limit reset time from text: resetIn=%ds, resetAt=%s", secs, resetAt.UTC().Format(time.RFC3339))
	return resetAt
}
