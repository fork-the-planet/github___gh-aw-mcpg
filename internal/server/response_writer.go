package server

import (
	"bytes"
	"net/http"

	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logResponseWriter = logger.ForFile()

// responseWriter wraps http.ResponseWriter to capture response body and status code.
// It embeds httputil.BaseResponseWriter for shared status-code capture logic, and
// adds body buffering for debug logging.
type responseWriter struct {
	httputil.BaseResponseWriter
	body bytes.Buffer
}

// newResponseWriter creates a new responseWriter. StatusCode is 0 until the
// first WriteHeader or Write call; 0 means no response has been written yet.
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	logResponseWriter.Print("Creating new response writer")
	return &responseWriter{
		BaseResponseWriter: httputil.BaseResponseWriter{
			ResponseWriter: w,
		},
	}
}

func (w *responseWriter) WriteHeader(statusCode int) {
	logResponseWriter.Printf("Setting response status code: %d", statusCode)
	w.BaseResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	logResponseWriter.Printf("Writing response body: %d bytes", len(b))
	w.body.Write(b)
	return w.BaseResponseWriter.Write(b)
}

// Body returns the captured response body as bytes
func (w *responseWriter) Body() []byte {
	bodyBytes := w.body.Bytes()
	logResponseWriter.Printf("Retrieving captured body: %d bytes", len(bodyBytes))
	return bodyBytes
}
