package tracing

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/httputil"
)

// mockFlusher implements http.ResponseWriter and http.Flusher so we can verify
// that Unwrap gives callers transparent access to the underlying writer's optional
// interfaces.
type mockFlusher struct {
	httptest.ResponseRecorder
	flushed bool
}

func (m *mockFlusher) Flush() { m.flushed = true }

func TestStatusResponseWriter_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	srw := &statusResponseWriter{BaseResponseWriter: httputil.BaseResponseWriter{ResponseWriter: rec}}

	srw.WriteHeader(http.StatusCreated)

	assert.Equal(t, http.StatusCreated, srw.StatusCode)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestStatusResponseWriter_Write_SetsImplicit200(t *testing.T) {
	rec := httptest.NewRecorder()
	srw := &statusResponseWriter{BaseResponseWriter: httputil.BaseResponseWriter{ResponseWriter: rec}}

	// StatusCode starts at zero – Write should set it to 200 implicitly.
	n, err := srw.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, http.StatusOK, srw.StatusCode)
}

func TestStatusResponseWriter_Write_PreservesExplicitStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	srw := &statusResponseWriter{BaseResponseWriter: httputil.BaseResponseWriter{ResponseWriter: rec}}

	srw.WriteHeader(http.StatusAccepted)

	_, err := srw.Write([]byte("body"))
	require.NoError(t, err)
	// StatusCode was already set via WriteHeader; Write must not overwrite it.
	assert.Equal(t, http.StatusAccepted, srw.StatusCode)
	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestStatusResponseWriter_Unwrap_ReturnsUnderlying(t *testing.T) {
	rec := httptest.NewRecorder()
	srw := &statusResponseWriter{BaseResponseWriter: httputil.BaseResponseWriter{ResponseWriter: rec}}

	underlying := srw.Unwrap()
	assert.Same(t, rec, underlying, "Unwrap should return the wrapped ResponseWriter")
}

// TestWrapHTTPHandler_PatternMethodMismatch_DoesNotPanic verifies that WrapHTTPHandler
// handles requests whose r.Pattern contains a method prefix that doesn't match r.Method
// (a defensive scenario that shouldn't occur with net/http mux routing).
//
// This still exercises the `route = ""` branch in WrapHTTPHandler.
func TestWrapHTTPHandler_PatternMethodMismatch_DoesNotPanic(t *testing.T) {
	// Build a request whose Pattern method differs from its actual Method.
	// In normal mux routing this cannot happen, but direct manipulation lets us
	// verify that WrapHTTPHandler handles it gracefully without setting http.route.
	req := httptest.NewRequest("GET", "/some/path", nil)
	req.Pattern = "POST /some/path" // method in pattern != request method

	rr := httptest.NewRecorder()
	var capturedRoute string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record the Pattern so the test can confirm we reached the inner handler.
		capturedRoute = r.Pattern
		w.WriteHeader(http.StatusOK)
	})

	// No panics, no mismatched route attribute.
	require.NotPanics(t, func() {
		WrapHTTPHandler(inner, "test.route.mismatch").ServeHTTP(rr, req)
	})
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "POST /some/path", capturedRoute, "inner handler should receive the original request unchanged")
}

func TestStatusResponseWriter_Unwrap_ExposesOptionalInterfaces(t *testing.T) {
	mf := &mockFlusher{ResponseRecorder: *httptest.NewRecorder()}
	srw := &statusResponseWriter{BaseResponseWriter: httputil.BaseResponseWriter{ResponseWriter: mf}}

	// http.ResponseController uses Unwrap to discover optional interfaces like Flusher.
	rc := http.NewResponseController(srw)
	err := rc.Flush()
	require.NoError(t, err, "Flush via ResponseController should succeed when underlying writer is a Flusher")
	assert.True(t, mf.flushed, "underlying Flusher should have been called through Unwrap")
}
