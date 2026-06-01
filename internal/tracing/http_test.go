package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"

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

// TestWrapHTTPHandler_PatternMethodMismatch_OmitsRouteAttribute verifies that
// WrapHTTPHandler omits http.route when r.Pattern contains a method prefix that
// doesn't match r.Method.
func TestWrapHTTPHandler_PatternMethodMismatch_OmitsRouteAttribute(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		require.NoError(t, tp.Shutdown(context.Background()))
	})

	// Build a request whose Pattern method differs from its actual Method.
	// In normal mux routing this cannot happen, but direct manipulation lets us
	// verify that WrapHTTPHandler handles it gracefully.
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

	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "expected exactly one span")

	var foundMethod, foundPath, foundRoute bool
	for _, attr := range spans[0].Attributes {
		switch attr.Key {
		case semconv.HTTPRequestMethodKey:
			foundMethod = true
			assert.Equal(t, http.MethodGet, attr.Value.AsString())
		case semconv.URLPathKey:
			foundPath = true
			assert.Equal(t, "/some/path", attr.Value.AsString())
		case semconv.HTTPRouteKey:
			foundRoute = true
		}
	}
	assert.True(t, foundMethod, "http.request.method attribute must be present on the span")
	assert.True(t, foundPath, "url.path attribute must be present on the span")
	assert.False(t, foundRoute, "http.route attribute must be omitted when the pattern method mismatches the request method")
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
