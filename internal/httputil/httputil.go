// Package httputil provides shared HTTP helper utilities used across multiple
// HTTP-facing packages (server, proxy, etc.).
package httputil

import (
	"encoding/json"
	"net/http"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logHTTP = logger.ForFile()

// WriteJSONResponse sets the Content-Type header, writes the status code, and encodes
// body as JSON. It centralises the three-line pattern used across HTTP handlers.
func WriteJSONResponse(w http.ResponseWriter, statusCode int, body interface{}) {
	logHTTP.Printf("Writing JSON response: statusCode=%d", statusCode)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	data, err := json.Marshal(body)
	if err != nil {
		logHTTP.Printf("Failed to marshal JSON response body: %v", err)
		return
	}
	logHTTP.Printf("JSON response body size: %d bytes", len(data))
	n, err := w.Write(data)
	if err != nil {
		logHTTP.Printf("Failed to write JSON response body: wrote=%d expected=%d err=%v", n, len(data), err)
		return
	}
	if n != len(data) {
		logHTTP.Printf("Short write for JSON response body: wrote=%d expected=%d", n, len(data))
	}
}

// WriteErrorResponse writes a JSON error response with a consistent
// {"error": code, "message": message} shape. Both the server and proxy packages
// should use this helper so that API consumers always receive the same error shape.
func WriteErrorResponse(w http.ResponseWriter, statusCode int, code, message string) {
	WriteJSONResponse(w, statusCode, map[string]string{
		"error":   code,
		"message": message,
	})
}

// IsTransientHTTPError returns true for status codes that indicate a temporary
// server-side condition (rate-limiting or transient failure) worth retrying.
func IsTransientHTTPError(statusCode int) bool {
	transient := statusCode == http.StatusTooManyRequests ||
		statusCode == http.StatusServiceUnavailable ||
		(statusCode >= 500 && statusCode < 600)
	if transient {
		logHTTP.Printf("Transient HTTP error detected: statusCode=%d", statusCode)
	}
	return transient
}

// WriteSimpleHealthResponse writes a minimal {"status":"ok"} JSON health response.
// This is the standard health response shape for endpoints that do not have access
// to detailed server status (e.g. the proxy), keeping them consistent with each other.
func WriteSimpleHealthResponse(w http.ResponseWriter) {
	WriteJSONResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// WriteReflectResponse writes the DIFC label snapshot JSON response for /reflect endpoints.
// Both the server and the proxy expose /reflect; sharing this helper ensures the output
// format evolves consistently as difc.BuildReflectResponse changes.
func WriteReflectResponse(w http.ResponseWriter, difcComponents difc.DIFCComponents) {
	WriteJSONResponse(w, http.StatusOK, difc.BuildReflectResponse(difcComponents))
}
