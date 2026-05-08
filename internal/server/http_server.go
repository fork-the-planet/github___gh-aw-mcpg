package server

import (
	"net/http"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
)

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:    addr,
		Handler: handler,
	}
}

// buildMCPHTTPServer creates the common HTTP server scaffold shared by all server modes.
// It creates a new mux, registers common endpoints (health, close, OAuth discovery),
// resolves the session timeout, and delegates mode-specific route registration to
// the provided routeBuilder before returning the configured *http.Server.
func buildMCPHTTPServer(
	addr string,
	unifiedServer *UnifiedServer,
	apiKey, hmacSecret string,
	routeBuilder func(mux *http.ServeMux, sessionTimeout time.Duration),
) *http.Server {
	mux := http.NewServeMux()
	registerCommonEndpoints(mux, unifiedServer, apiKey)
	sessionTimeout := config.GetGatewaySessionTimeoutFromEnv()
	routeBuilder(mux, sessionTimeout)
	return newHTTPServer(addr, mux)
}
