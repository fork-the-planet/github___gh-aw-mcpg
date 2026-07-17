// Package sanitize provides utilities for redacting sensitive information from logs.
//
// This package offers two complementary approaches to secret sanitization:
//
//  1. Pattern-based detection: SanitizeString() and SanitizeJSON() use regex patterns
//     to identify and redact secrets like API keys, tokens, and passwords.
//
//  2. Prefix redaction: RedactSecret() and RedactSecretMap() show only the first
//     4 characters of values, making them safe for logging without exposing full secrets.
//
// Usage Guidelines:
//
//   - Use RedactSecret()/RedactSecretMap() for auth headers and environment variables
//     where you want to preserve a hint of the value for debugging.
//
//   - Use SanitizeString()/SanitizeJSON() for full payload sanitization where secrets
//     may appear in various formats throughout the data.
//
// Example:
//
//	// For auth headers
//	log.Printf("Auth: %s", sanitize.RedactSecret(authHeader)) // "ghp_..." instead of full token
//
//	// For environment variables
//	log.Printf("Env: %v", sanitize.RedactSecretMap(envVars))
//
//	// For JSON payloads
//	sanitized := sanitize.SanitizeJSON(payload) // Replaces detected secrets with [REDACTED]
package sanitize

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// SecretPatterns contains regex patterns for detecting potential secrets
var SecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(token|key|secret|password|auth)[=:]\s*[^\s]{8,}`),
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{36,}`),                                  // GitHub PATs
	regexp.MustCompile(`github_pat_[a-zA-Z0-9]{22}_[a-zA-Z0-9]{59}`),            // GitHub fine-grained PATs
	regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9\-._~+/]+=*`),                    // Bearer tokens
	regexp.MustCompile(`(?i)authorization:\s*[a-zA-Z0-9\-._~+/]+=*`),            // Auth headers
	regexp.MustCompile(`[a-f0-9]{32,}`),                                         // Long hex strings (API keys)
	regexp.MustCompile(`(?i)(apikey|api_key|access_key)[=:]\s*[^\s]{8,}`),       // API keys
	regexp.MustCompile(`(?i)(client_secret|client_id)[=:]\s*[^\s]{8,}`),         // OAuth secrets
	regexp.MustCompile(`[a-zA-Z0-9_-]{20,}\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`), // JWT tokens
	// JSON-specific patterns for field:value pairs
	regexp.MustCompile(`(?i)"(token|password|passwd|pwd|apikey|api_key|api-key|secret|client_secret|api_secret|authorization|auth|key|private_key|public_key|credentials|credential|access_token|refresh_token|bearer_token)"\s*:\s*"[^"]{1,}"`),
}

// separatorRe matches the key/value separator (= or :) with optional trailing spaces.
// Pre-compiled at package level to avoid re-compilation on every SanitizeString call.
var separatorRe = regexp.MustCompile(`[=:]\s*`)

// MarshalAndSanitize marshals value to JSON and sanitizes the result to redact secrets.
// If marshaling fails, it returns a sanitized empty string rather than surfacing a
// logging-only error — callers should use this only in best-effort logging contexts.
func MarshalAndSanitize(value any) string {
	resultJSON, _ := json.Marshal(value)
	return SanitizeString(string(resultJSON))
}

// SanitizeString replaces potential secrets in a string with [REDACTED]
func SanitizeString(message string) string {
	result := message
	for _, pattern := range SecretPatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			// Keep the prefix (key name) but redact the value
			if strings.Contains(match, "=") || strings.Contains(match, ":") {
				parts := separatorRe.Split(match, 2)
				if len(parts) == 2 {
					return parts[0] + "=[REDACTED]"
				}
			}
			// For tokens without key=value format, redact entirely
			return "[REDACTED]"
		})
	}
	return result
}

// RedactSecret returns a sanitized version of the input string for safe logging.
// It shows only the first 4 characters followed by "..." to prevent exposing sensitive data.
// For strings with 4 or fewer characters, it returns only "...".
// For empty strings, it returns an empty string.
func RedactSecret(input string) string {
	if len(input) == 0 {
		return ""
	}
	const prefixLen = 4
	if len(input) <= prefixLen {
		return "..."
	}
	return input[:prefixLen] + "..."
}

// RedactSecretMap returns a sanitized version of environment variables
// where each value is truncated to first 4 characters followed by "..."
// This prevents sensitive information like API keys from being logged in full.
func RedactSecretMap(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	sanitized := make(map[string]string, len(env))
	for key, value := range env {
		sanitized[key] = RedactSecret(value)
	}
	return sanitized
}

// SanitizeJSON sanitizes a JSON payload by applying regex patterns to the entire string
// It takes raw bytes, applies regex sanitization in one pass, and returns sanitized bytes
func SanitizeJSON(payloadBytes []byte) json.RawMessage {
	return SanitizeJSONFromString(SanitizeString(string(payloadBytes)))
}

// SanitizeJSONFromString compacts an already-sanitized JSON string into a
// json.RawMessage. It skips the regex sanitization pass — callers that have
// already called SanitizeString on the payload string can use this to avoid
// running the 10 compiled regex patterns a second time.
func SanitizeJSONFromString(sanitized string) json.RawMessage {
	// Use json.Compact to validate and compact in one pass (avoids a full unmarshal+marshal cycle)
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(sanitized)); err != nil {
		wrapped := map[string]string{
			"_error": "invalid JSON",
			"_raw":   sanitized,
		}
		wrappedBytes, _ := json.Marshal(wrapped)
		return json.RawMessage(wrappedBytes)
	}
	return json.RawMessage(buf.Bytes())
}

// RedactURL returns a safe-to-log version of a URL by retaining only the scheme,
// host, and path. Userinfo (credentials), query parameters, and fragments are
// removed to prevent accidental leakage of secrets (e.g. api_key=..., token=...).
// If the input cannot be parsed as a URL, the literal string "<unparseable-url>" is
// returned instead so callers never log raw unverified input.
func RedactURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "<unparseable-url>"
	}
	safe := &url.URL{
		Scheme: u.Scheme,
		Host:   u.Host,
		Path:   u.Path,
	}
	return safe.String()
}

// SanitizeArgs returns a sanitized version of command arguments for safe logging.
// It specifically handles Docker-style environment variable arguments (-e VAR=VALUE)
// by truncating ALL values to prevent exposing sensitive data like API tokens.
// This approach prioritizes security over debugging convenience - we truncate all
// environment variable values rather than trying to selectively identify secrets.
// Other arguments are passed through unchanged.
func SanitizeArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	sanitized := make([]string, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Check if this is an environment variable value after a -e flag.
		// Format: -e VAR=VALUE
		if i > 0 && args[i-1] == "-e" {
			if varName, varValue, ok := strings.Cut(arg, "="); ok {
				sanitized[i] = varName + "=" + RedactSecret(varValue)
				continue
			}
		}
		sanitized[i] = arg
	}
	return sanitized
}
