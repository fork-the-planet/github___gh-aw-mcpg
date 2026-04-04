package server

import (
	"net/http"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logAuth = logger.New("server:auth")

// isMalformedAuthHeader returns true if the header value contains characters
// that are not valid in HTTP header values per RFC 7230: null bytes, control
// characters below 0x20 (except horizontal tab 0x09), or DEL (0x7F).
// Per spec 7.2 item 3, such headers must be rejected with HTTP 400.
func isMalformedAuthHeader(header string) bool {
	for _, c := range header {
		if c == 0x00 || (c < 0x20 && c != 0x09) || c == 0x7F {
			return true
		}
	}
	return false
}

// authMiddleware implements API key authentication per spec section 7.1
// Per spec: Authorization header MUST contain the API key directly (NOT Bearer scheme)
//
// For header parsing logic, see internal/auth package which provides:
//   - ParseAuthHeader() for extracting API keys and agent IDs
//   - ValidateAPIKey() for key validation
func authMiddleware(apiKey string, next http.HandlerFunc) http.HandlerFunc {
	logAuth.Printf("Initialized auth middleware")
	return func(w http.ResponseWriter, r *http.Request) {
		logAuth.Printf("Authenticating request: method=%s, path=%s, remote=%s", r.Method, r.URL.Path, r.RemoteAddr)

		// Extract Authorization header
		authHeader := r.Header.Get("Authorization")

		if authHeader == "" {
			// Spec 7.1: Missing token returns 401
			rejectRequest(w, r, http.StatusUnauthorized, "unauthorized", "missing Authorization header", "auth", "authentication_failed", "missing_auth_header")
			return
		}

		// Spec 7.2 item 3: Malformed Authorization headers (null bytes, non-printable
		// control characters) must return 400 Bad Request, not 401.
		if isMalformedAuthHeader(authHeader) {
			rejectRequest(w, r, http.StatusBadRequest, "bad_request", "malformed Authorization header", "auth", "authentication_failed", "malformed_auth_header")
			return
		}

		// Spec 7.1: Authorization header must contain API key directly (not Bearer scheme)
		if authHeader != apiKey {
			rejectRequest(w, r, http.StatusUnauthorized, "unauthorized", "invalid API key", "auth", "authentication_failed", "invalid_api_key")
			return
		}

		logger.LogInfo("auth", "Authentication successful, remote=%s, path=%s", r.RemoteAddr, r.URL.Path)
		// Token is valid, proceed to handler
		next(w, r)
	}
}

// applyAuthIfConfigured applies authentication middleware if an API key is provided
// Returns the handler unchanged if apiKey is empty
func applyAuthIfConfigured(apiKey string, handler http.HandlerFunc) http.HandlerFunc {
	if apiKey != "" {
		return authMiddleware(apiKey, handler)
	}
	return handler
}
