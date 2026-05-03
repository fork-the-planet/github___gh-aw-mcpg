package tracing

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	srw := &statusResponseWriter{ResponseWriter: rec}

	srw.WriteHeader(http.StatusCreated)

	assert.Equal(t, http.StatusCreated, srw.statusCode)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestStatusResponseWriter_Write_SetsImplicit200(t *testing.T) {
	rec := httptest.NewRecorder()
	srw := &statusResponseWriter{ResponseWriter: rec}

	// statusCode starts at zero – Write should set it to 200 implicitly.
	n, err := srw.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, http.StatusOK, srw.statusCode)
}

func TestStatusResponseWriter_Write_PreservesExplicitStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	srw := &statusResponseWriter{ResponseWriter: rec, statusCode: http.StatusAccepted}

	_, err := srw.Write([]byte("body"))
	require.NoError(t, err)
	// statusCode was already set; Write must not overwrite it.
	assert.Equal(t, http.StatusAccepted, srw.statusCode)
}

func TestStatusResponseWriter_Unwrap_ReturnsUnderlying(t *testing.T) {
	rec := httptest.NewRecorder()
	srw := &statusResponseWriter{ResponseWriter: rec}

	underlying := srw.Unwrap()
	assert.Equal(t, rec, underlying, "Unwrap should return the wrapped ResponseWriter")
}

func TestStatusResponseWriter_Unwrap_ExposesOptionalInterfaces(t *testing.T) {
	mf := &mockFlusher{ResponseRecorder: *httptest.NewRecorder()}
	srw := &statusResponseWriter{ResponseWriter: mf}

	// http.ResponseController uses Unwrap to discover optional interfaces like Flusher.
	rc := http.NewResponseController(srw)
	err := rc.Flush()
	require.NoError(t, err, "Flush via ResponseController should succeed when underlying writer is a Flusher")
	assert.True(t, mf.flushed, "underlying Flusher should have been called through Unwrap")
}
