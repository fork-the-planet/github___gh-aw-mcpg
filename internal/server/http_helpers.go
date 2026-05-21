package server

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/github/gh-aw-mcpg/internal/auth"
	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/logger/sanitize"
)

var logHelpers = logger.New("server:helpers")

// logRuntimeError logs runtime errors to stdout per spec section 9.2
func logRuntimeError(errorType, detail string, r *http.Request, serverName *string) {
	logHelpers.Printf("Logging runtime error: type=%s, detail=%s", errorType, detail)

	timestamp := time.Now().UTC().Format(time.RFC3339)
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = "unknown"
	}

	server := "gateway"
	if serverName != nil {
		server = *serverName
	}

	// Spec 9.2: Log to stdout with timestamp, server name, request ID, error details
	log.Printf("[ERROR] timestamp=%s server=%s request_id=%s error_type=%s detail=%s path=%s method=%s",
		timestamp, server, requestID, errorType, detail, r.URL.Path, r.Method)
}

// rejectRequest logs a structured error, records a runtime error, and writes an
// HTTP error response. This consolidates the 3-step rejection pattern that was
// previously duplicated across auth and handler code paths.
//
// Parameters:
//   - logCategory: category for the structured log (e.g. "auth")
//   - runtimeErrType: error_type field for runtime error log (e.g. "authentication_failed")
//   - runtimeDetail: detail field for runtime error log (e.g. "missing_auth_header")
//   - msg: human-readable message sent back in the HTTP response
func rejectRequest(w http.ResponseWriter, r *http.Request, status int, code, msg, logCategory, runtimeErrType, runtimeDetail string) {
	logger.LogErrorToMarkdown(logCategory, "Request rejected: %s, remote=%s, path=%s", msg, r.RemoteAddr, r.URL.Path)
	logRuntimeError(runtimeErrType, runtimeDetail, r, nil)
	httputil.WriteErrorResponse(w, status, code, msg)
}

// withResponseLogging wraps an http.Handler to log response bodies
func withResponseLogging(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lw := newResponseWriter(w)
		handler.ServeHTTP(lw, r)
		if len(lw.Body()) > 0 {
			sanitizedBody := sanitize.SanitizeString(string(lw.Body()))
			logHelpers.Printf("[%s] %s %s - Status: %d, Response: %s", r.RemoteAddr, r.Method, r.URL.Path, lw.StatusCode(), sanitizedBody)
		}
	})
}

// peekRequestBody reads all bytes from a POST request body and restores it
// so downstream handlers can read it again.
// Returns nil, nil for non-POST requests or requests with no body.
func peekRequestBody(r *http.Request) ([]byte, error) {
	if r.Method != http.MethodPost || r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}

	origBody := r.Body
	b, err := io.ReadAll(origBody)
	closeErr := origBody.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}

	if len(b) == 0 {
		r.Body = http.NoBody
		return b, nil
	}

	r.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}

// logHTTPRequestBody logs the request body for debugging purposes.
// It reads the body, logs it, and restores it so it can be read again.
// The backendID parameter is optional and can be empty for unified mode.
func logHTTPRequestBody(r *http.Request, sessionID, backendID string) {
	logHelpers.Printf("Checking request body: method=%s, hasBody=%v, sessionID=%s", r.Method, r.Body != nil, auth.TruncateSessionID(sessionID))

	bodyBytes, err := peekRequestBody(r)
	if err != nil {
		logHelpers.Printf("Body read failed: err=%v", err)
		return
	}
	if len(bodyBytes) == 0 {
		logHelpers.Printf("Skipping body logging: not a POST request, no body present, or empty body")
		return
	}

	logHelpers.Printf("Request body read: size=%d bytes, sessionID=%s, backendID=%s", len(bodyBytes), auth.TruncateSessionID(sessionID), backendID)

	sanitizedBody := sanitize.SanitizeString(string(bodyBytes))

	if backendID != "" {
		logger.LogDebug("client", "MCP client request body, backend=%s, body=%s", backendID, sanitizedBody)
	} else {
		logger.LogDebug("client", "MCP request body, session=%s, body=%s", auth.TruncateSessionID(sessionID), sanitizedBody)
	}
	logHelpers.Print("Request body logged for debugging")
}

func truncateCacheKeyForLog(key string) string {
	backendID, sessionID, found := strings.Cut(key, "/")
	if !found {
		return key
	}

	return fmt.Sprintf("%s/%s", backendID, auth.TruncateSessionID(sessionID))
}
