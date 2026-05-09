package server

import (
	"net/http"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logHTTPServer = logger.New("server:http_server")

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	logHTTPServer.Printf("Creating HTTP server: addr=%s", addr)
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
	logHTTPServer.Printf("Building MCP HTTP server: addr=%s, auth_enabled=%v, hmac_enabled=%v", addr, apiKey != "", hmacSecret != "")
	mux := http.NewServeMux()
	registerCommonEndpoints(mux, unifiedServer, apiKey)
	sessionTimeout := config.GetGatewaySessionTimeoutFromEnv()
	logHTTPServer.Printf("Session timeout configured: %s", sessionTimeout)
	routeBuilder(mux, sessionTimeout)
	logHTTPServer.Printf("MCP HTTP server scaffold ready: addr=%s", addr)
	return newHTTPServer(addr, mux)
}
