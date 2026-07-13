package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResponseWriter(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	w := newResponseWriter(rr)

	require.NotNil(t, w)
	// Before any write, status code should be 0 (not set)
	assert.Equal(t, 0, w.StatusCode)
	// Before any write, body should be empty
	assert.Empty(t, w.Body())
}

func TestResponseWriter_WriteHeader(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	w := newResponseWriter(rr)

	w.WriteHeader(http.StatusCreated)

	assert.Equal(t, http.StatusCreated, w.StatusCode)
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestResponseWriter_Write(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	w := newResponseWriter(rr)

	payload := []byte(`{"hello":"world"}`)
	n, err := w.Write(payload)

	require.NoError(t, err)
	assert.Equal(t, len(payload), n)
	assert.Equal(t, payload, w.Body())
	assert.Equal(t, payload, rr.Body.Bytes())
}

func TestResponseWriter_MultipleWrites(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	w := newResponseWriter(rr)

	chunk1 := []byte("hello")
	chunk2 := []byte(" world")
	_, err := w.Write(chunk1)
	require.NoError(t, err)
	_, err = w.Write(chunk2)
	require.NoError(t, err)

	assert.Equal(t, []byte("hello world"), w.Body())
	assert.Equal(t, []byte("hello world"), rr.Body.Bytes())
}

func TestResponseWriter_WriteImplicitStatus200(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	w := newResponseWriter(rr)

	_, err := w.Write([]byte("content"))
	require.NoError(t, err)

	// net/http sets status 200 on first Write if WriteHeader wasn't called
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestResponseWriter_BodyAfterWriteHeader(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	w := newResponseWriter(rr)

	w.WriteHeader(http.StatusNotFound)
	body := []byte(`{"error":"not found"}`)
	_, err := w.Write(body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusNotFound, w.StatusCode)
	assert.Equal(t, body, w.Body())
}

func TestResponseWriter_EmptyBody(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	w := newResponseWriter(rr)

	w.WriteHeader(http.StatusNoContent)

	assert.Equal(t, http.StatusNoContent, w.StatusCode)
	assert.Empty(t, w.Body())
}

func TestResponseWriter_ImplementsResponseWriter(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	w := newResponseWriter(rr)

	// responseWriter should implement http.ResponseWriter
	var _ http.ResponseWriter = w
}

func TestResponseWriter_HeaderPassthrough(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	w := newResponseWriter(rr)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
}
