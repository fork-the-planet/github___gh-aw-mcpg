package httputil

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

func TestBaseResponseWriter_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	brw := &BaseResponseWriter{ResponseWriter: rec}

	brw.WriteHeader(http.StatusCreated)

	assert.Equal(t, http.StatusCreated, brw.StatusCode)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestBaseResponseWriter_Write_SetsImplicit200(t *testing.T) {
	rec := httptest.NewRecorder()
	brw := &BaseResponseWriter{ResponseWriter: rec}

	// StatusCode starts at zero – Write should set it to 200 implicitly.
	n, err := brw.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, http.StatusOK, brw.StatusCode)
}

func TestBaseResponseWriter_Write_PreservesExplicitStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	brw := &BaseResponseWriter{ResponseWriter: rec}

	brw.WriteHeader(http.StatusAccepted)

	_, err := brw.Write([]byte("body"))
	require.NoError(t, err)
	// StatusCode was already set via WriteHeader; Write must not overwrite it.
	assert.Equal(t, http.StatusAccepted, brw.StatusCode)
	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestBaseResponseWriter_Unwrap_ReturnsUnderlying(t *testing.T) {
	rec := httptest.NewRecorder()
	brw := &BaseResponseWriter{ResponseWriter: rec}

	underlying := brw.Unwrap()
	assert.Same(t, rec, underlying, "Unwrap should return the wrapped ResponseWriter")
}

func TestBaseResponseWriter_Unwrap_ExposesOptionalInterfaces(t *testing.T) {
	mf := &mockFlusher{ResponseRecorder: *httptest.NewRecorder()}
	brw := &BaseResponseWriter{ResponseWriter: mf}

	// http.ResponseController uses Unwrap to discover optional interfaces like Flusher.
	rc := http.NewResponseController(brw)
	err := rc.Flush()
	require.NoError(t, err, "Flush via ResponseController should succeed when underlying writer is a Flusher")
	assert.True(t, mf.flushed, "underlying Flusher should have been called through Unwrap")
}
