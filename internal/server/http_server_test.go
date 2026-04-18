package server

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPServer(t *testing.T) {
	handler := http.NewServeMux()

	server := newHTTPServer("127.0.0.1:1234", handler)
	require.NotNil(t, server)
	assert.Equal(t, "127.0.0.1:1234", server.Addr)
	assert.Same(t, handler, server.Handler)
}
