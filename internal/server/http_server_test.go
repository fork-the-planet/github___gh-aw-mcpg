package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/config"
)

func TestNewHTTPServer(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		handler http.Handler
	}{
		{
			name:    "host:port address",
			addr:    "127.0.0.1:1234",
			handler: http.NewServeMux(),
		},
		{
			name:    "port-only address",
			addr:    ":8080",
			handler: http.NewServeMux(),
		},
		{
			name:    "zero port",
			addr:    "127.0.0.1:0",
			handler: http.NewServeMux(),
		},
		{
			name:    "empty address",
			addr:    "",
			handler: http.NewServeMux(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newHTTPServer(tt.addr, tt.handler)
			require.NotNil(t, server)
			assert.Equal(t, tt.addr, server.Addr)
			assert.Same(t, tt.handler, server.Handler)
		})
	}
}

// TestBuildMCPHTTPServer_ReturnsServerWithCorrectAddr verifies that buildMCPHTTPServer
// returns an http.Server bound to the requested address.
func TestBuildMCPHTTPServer_ReturnsServerWithCorrectAddr(t *testing.T) {
	us, err := NewUnified(context.Background(), &config.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { us.Close() })

	const addr = "127.0.0.1:0"
	server := buildMCPHTTPServer(addr, us, "", "", func(_ *http.ServeMux, _ time.Duration) {})

	require.NotNil(t, server)
	assert.Equal(t, addr, server.Addr)
}

// TestBuildMCPHTTPServer_RouteBuilderIsCalled verifies that buildMCPHTTPServer
// invokes the supplied routeBuilder callback.
func TestBuildMCPHTTPServer_RouteBuilderIsCalled(t *testing.T) {
	us, err := NewUnified(context.Background(), &config.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { us.Close() })

	called := false
	buildMCPHTTPServer("127.0.0.1:0", us, "", "", func(_ *http.ServeMux, _ time.Duration) {
		called = true
	})

	assert.True(t, called, "routeBuilder should be called by buildMCPHTTPServer")
}

// TestBuildMCPHTTPServer_RouteBuilderReceivesSessionTimeout verifies that
// the session timeout passed to routeBuilder reflects the environment variable.
func TestBuildMCPHTTPServer_RouteBuilderReceivesSessionTimeout(t *testing.T) {
	us, err := NewUnified(context.Background(), &config.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { us.Close() })

	t.Setenv("MCP_GATEWAY_SESSION_TIMEOUT", "15m")

	var capturedTimeout time.Duration
	buildMCPHTTPServer("127.0.0.1:0", us, "", "", func(_ *http.ServeMux, sessionTimeout time.Duration) {
		capturedTimeout = sessionTimeout
	})

	assert.Equal(t, 15*time.Minute, capturedTimeout)
}

// TestBuildMCPHTTPServer_CustomRouteFromBuilder verifies that routes registered
// inside the routeBuilder callback are accessible via the returned server's handler.
func TestBuildMCPHTTPServer_CustomRouteFromBuilder(t *testing.T) {
	us, err := NewUnified(context.Background(), &config.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { us.Close() })

	server := buildMCPHTTPServer("127.0.0.1:0", us, "", "", func(mux *http.ServeMux, _ time.Duration) {
		mux.HandleFunc("/custom-test-route", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/custom-test-route", nil)
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusTeapot, rr.Code)
}
