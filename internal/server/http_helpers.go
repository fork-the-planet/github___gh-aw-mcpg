package server

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/sanitize"
	"github.com/github/gh-aw-mcpg/internal/util"
)

var logServerHelpers = logger.New("server:helpers")

// logRuntimeError logs runtime errors to stdout per spec section 9.2
func logRuntimeError(errorType, detail string, r *http.Request, serverName *string) {
	logServerHelpers.Printf("Logging runtime error: type=%s, detail=%s", errorType, detail)

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
// This is the canonical rejection entry point for all middleware and handler code.
// New middleware should call rejectRequest directly, optionally emitting any
// component-specific debug logging before rejection.
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
	logServerHelpers.Printf("Checking request body: method=%s, hasBody=%v, sessionID=%s", r.Method, r.Body != nil, util.FormatSessionIDForLog(sessionID))

	bodyBytes, err := peekRequestBody(r)
	if err != nil {
		logServerHelpers.Printf("Body read failed: err=%v", err)
		return
	}
	if len(bodyBytes) == 0 {
		logServerHelpers.Printf("Skipping body logging: not a POST request, no body present, or empty body")
		return
	}

	logServerHelpers.Printf("Request body read: size=%d bytes, sessionID=%s, backendID=%s", len(bodyBytes), util.FormatSessionIDForLog(sessionID), backendID)

	sanitizedBody := sanitize.SanitizeString(string(bodyBytes))

	if backendID != "" {
		logger.LogDebug("client", "MCP client request body, backend=%s, body=%s", backendID, sanitizedBody)
	} else {
		logger.LogDebug("client", "MCP request body, session=%s, body=%s", util.FormatSessionIDForLog(sessionID), sanitizedBody)
	}
	logServerHelpers.Print("Request body logged for debugging")
}
