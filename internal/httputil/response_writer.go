package httputil

import "net/http"

// BaseResponseWriter wraps http.ResponseWriter and captures the HTTP response
// status code. It implements Write (with implicit-200 capture), WriteHeader,
// and Unwrap so that http.ResponseController callers can access optional
// interfaces (e.g. http.Flusher, http.Hijacker) on the underlying writer.
//
// Embed BaseResponseWriter in package-specific types to avoid duplicating
// this status-capture boilerplate.
type BaseResponseWriter struct {
	http.ResponseWriter
	// StatusCode holds the captured HTTP status code. It is set by WriteHeader
	// and, if still zero when Write is first called, defaults to http.StatusOK.
	// Only the first call to WriteHeader or Write sets StatusCode, matching
	// net/http semantics where only the first status sent is effective.
	StatusCode  int
	wroteHeader bool
}

// WriteHeader captures the status code on first call and forwards it to the
// underlying writer. Subsequent calls are forwarded but do not update StatusCode,
// matching net/http semantics where only the first WriteHeader is effective.
func (w *BaseResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		logHTTP.Printf("BaseResponseWriter.WriteHeader: capturing status code=%d", code)
		w.StatusCode = code
		w.wroteHeader = true
	} else {
		logHTTP.Printf("BaseResponseWriter.WriteHeader: ignoring duplicate status code=%d (already captured=%d)", code, w.StatusCode)
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write captures an implicit 200 status on first call when no prior WriteHeader
// was issued, then delegates to the underlying writer.
func (w *BaseResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		logHTTP.Printf("BaseResponseWriter.Write: implicit 200 status captured (first write, len=%d)", len(b))
		w.StatusCode = http.StatusOK
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap returns the underlying http.ResponseWriter so that callers using
// http.ResponseController can discover optional interfaces such as http.Flusher
// or http.Hijacker on the real writer.
func (w *BaseResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
