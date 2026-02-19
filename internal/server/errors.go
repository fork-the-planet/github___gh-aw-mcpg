package server

import (
	"log"
	"net/http"
	"time"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logErrors = logger.New("server:errors")

// logRuntimeError logs runtime errors to stdout per spec section 9.2
func logRuntimeError(errorType, detail string, r *http.Request, serverName *string) {
	logErrors.Printf("Logging runtime error: type=%s, detail=%s", errorType, detail)

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
