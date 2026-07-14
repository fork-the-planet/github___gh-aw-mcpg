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
		return middleware(key, handler)
	}
	return handler
}

// applyIfConfiguredWithLog logs whether a middleware is active before applying it.
func applyIfConfiguredWithLog(key string, handler http.HandlerFunc, middleware func(string, http.HandlerFunc) http.HandlerFunc, logFn func(...any), enabledMsg, disabledMsg string) http.HandlerFunc {
	if key != "" {
		logFn(enabledMsg)
	} else {
		logFn(disabledMsg)
	}
	return applyIfConfigured(key, handler, middleware)
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
			logAuth.Printf("Rejecting auth request: status=%d, code=%s, detail=%s, path=%s, remote=%s", http.StatusUnauthorized, "unauthorized", "missing_auth_header", r.URL.Path, r.RemoteAddr)
			rejectRequest(w, r, http.StatusUnauthorized, "unauthorized", "missing Authorization header", "auth", "authentication_failed", "missing_auth_header")
			return
		}

		// Spec 7.2 item 3: Malformed Authorization headers (null bytes, non-printable
		// control characters) must return 400 Bad Request, not 401.
		if auth.IsMalformedHeader(authHeader) {
			logAuth.Printf("Rejecting auth request: status=%d, code=%s, detail=%s, path=%s, remote=%s", http.StatusBadRequest, "bad_request", "malformed_auth_header", r.URL.Path, r.RemoteAddr)
			rejectRequest(w, r, http.StatusBadRequest, "bad_request", "malformed Authorization header", "auth", "authentication_failed", "malformed_auth_header")
			return
		}

		// Spec 7.1: Authorization header must contain API key directly.
		if subtle.ConstantTimeCompare([]byte(authHeader), []byte(apiKey)) != 1 {
			logAuth.Printf("Rejecting auth request: status=%d, code=%s, detail=%s, path=%s, remote=%s", http.StatusUnauthorized, "unauthorized", "invalid_api_key", r.URL.Path, r.RemoteAddr)
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
	return applyIfConfiguredWithLog(
		apiKey,
		handler,
		authMiddleware,
		logAuth.Print,
		"Auth key configured, applying middleware",
		"No auth key configured, skipping middleware",
	)
}
