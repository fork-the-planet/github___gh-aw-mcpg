package server

import (
	"crypto/subtle"
	"net/http"

	"github.com/github/gh-aw-mcpg/internal/auth"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logAuth = logger.New("server:auth")

// applyIfConfigured wraps handler with middleware(key, handler) when key is non-empty.
// If key is empty the handler is returned unchanged.
func applyIfConfigured(key string, handler http.HandlerFunc, middleware func(string, http.HandlerFunc) http.HandlerFunc) http.HandlerFunc {
	if key != "" {
		logAuth.Print("Auth key configured, applying middleware")
		return middleware(key, handler)
	}
	logAuth.Print("No auth key configured, skipping middleware")
	return handler
}

// authMiddleware implements API key authentication per spec section 7.1
// Per spec: Authorization header MUST contain the API key directly.
//
// For header parsing logic, see internal/auth package which provides:
//   - ParseAuthHeader() for extracting API keys and agent IDs
//   - IsMalformedHeader() for malformed header detection
//
// This middleware validates credentials by directly comparing parsed API key
// values to the configured key.
func authMiddleware(apiKey string, next http.HandlerFunc) http.HandlerFunc {
	logAuth.Printf("Initialized auth middleware")
	return func(w http.ResponseWriter, r *http.Request) {
		logAuth.Printf("Authenticating request: method=%s, path=%s, remote=%s", r.Method, r.URL.Path, r.RemoteAddr)

		// Extract Authorization header
		authHeader := r.Header.Get("Authorization")

		if authHeader == "" {
			// Spec 7.1: Missing token returns 401
			rejectAuthRequest(w, r, http.StatusUnauthorized, "unauthorized", "missing Authorization header", "missing_auth_header")
			return
		}

		// Spec 7.2 item 3: Malformed Authorization headers (null bytes, non-printable
		// control characters) must return 400 Bad Request, not 401.
		if auth.IsMalformedHeader(authHeader) {
			rejectAuthRequest(w, r, http.StatusBadRequest, "bad_request", "malformed Authorization header", "malformed_auth_header")
			return
		}

		// Spec 7.1: Authorization header must contain API key directly.
		if subtle.ConstantTimeCompare([]byte(authHeader), []byte(apiKey)) != 1 {
			rejectAuthRequest(w, r, http.StatusUnauthorized, "unauthorized", "invalid API key", "invalid_api_key")
			return
		}

		logger.LogInfo("auth", "Authentication successful, remote=%s, path=%s", r.RemoteAddr, r.URL.Path)
		// Token is valid, proceed to handler
		next(w, r)
	}
}

func rejectAuthRequest(w http.ResponseWriter, r *http.Request, status int, code, msg, detail string) {
	logAuth.Printf("Rejecting auth request: status=%d, code=%s, detail=%s, path=%s, remote=%s", status, code, detail, r.URL.Path, r.RemoteAddr)
	rejectRequest(w, r, status, code, msg, "auth", "authentication_failed", detail)
}

// applyAuthIfConfigured applies authentication middleware if an API key is provided
// Returns the handler unchanged if apiKey is empty
func applyAuthIfConfigured(apiKey string, handler http.HandlerFunc) http.HandlerFunc {
	return applyIfConfigured(apiKey, handler, authMiddleware)
}
