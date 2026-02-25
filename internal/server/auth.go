package server

import (
	"net/http"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logAuth = logger.New("server:auth")

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
			logAuth.Printf("Authentication failed: missing Authorization header")
			// Spec 7.1: Missing token returns 401
			logger.LogErrorMd("auth", "Authentication failed: missing Authorization header, remote=%s, path=%s", r.RemoteAddr, r.URL.Path)
			logRuntimeError("authentication_failed", "missing_auth_header", r, nil)
			http.Error(w, "Unauthorized: missing Authorization header", http.StatusUnauthorized)
			return
		}

		// Spec 7.1: Authorization header must contain API key directly (not Bearer scheme)
		if authHeader != apiKey {
			logAuth.Printf("Authentication failed: invalid API key")
			logger.LogErrorMd("auth", "Authentication failed: invalid API key, remote=%s, path=%s", r.RemoteAddr, r.URL.Path)
			logRuntimeError("authentication_failed", "invalid_api_key", r, nil)
			http.Error(w, "Unauthorized: invalid API key", http.StatusUnauthorized)
			return
		}

		logAuth.Printf("Authentication successful")
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
