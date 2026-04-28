package server

import (
	"net/http"
	"time"

	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var logTransport = logger.New("server:transport")

// CreateHTTPServerForMCP creates an HTTP server that handles MCP over streamable HTTP transport
// If apiKey is provided, all requests except /health require authentication (spec 7.1)
// If hmacSecret is provided, each MCP request (/mcp, /mcp/) must carry a valid HMAC-SHA256
// signature (ASI-07); common endpoints (e.g. /health, /close) are not HMAC-protected.
func CreateHTTPServerForMCP(addr string, unifiedServer *UnifiedServer, apiKey, hmacSecret string) *http.Server {
	logTransport.Printf("Creating HTTP server for MCP: addr=%s, auth_enabled=%v, hmac_enabled=%v", addr, apiKey != "", hmacSecret != "")
	mux := http.NewServeMux()

	// Register common endpoints (OAuth discovery, health, close)
	registerCommonEndpoints(mux, unifiedServer, apiKey)

	logTransport.Print("Registering streamable HTTP handler for MCP protocol")
	// Create StreamableHTTP handler for MCP protocol (supports POST requests)
	// This is what Codex uses with transport = "streamablehttp"
	streamableHandler := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		// With streamable HTTP, this callback fires for each new session establishment
		// Subsequent JSON-RPC messages in the same session are handled by the SDK
		// We use the Authorization header value as the session ID
		// This groups all requests from the same agent (same auth value) into one session
		if _, ok := setupSessionCallback(r, ""); !ok {
			// Return nil to reject the connection
			// The SDK will handle sending an appropriate error response
			return nil
		}

		return unifiedServer.server
	}, &sdk.StreamableHTTPOptions{
		Stateless:      false,                                                              // Support stateful sessions
		Logger:         logger.NewSlogLoggerWithHandler(logTransport),                      // Integrate SDK logging with project logger
		SessionTimeout: envutil.GetEnvDuration("MCP_GATEWAY_SESSION_TIMEOUT", 6*time.Hour), // Configurable; 6h default matches GitHub Actions default timeout
	})

	// Apply standard middleware stack (SDK logging → shutdown check → auth → HMAC)
	finalHandler := wrapWithMiddleware(streamableHandler, "unified", unifiedServer, apiKey, hmacSecret)

	// Mount handler at /mcp endpoint (logging is done in the callback above)
	mux.Handle("/mcp/", finalHandler)
	mux.Handle("/mcp", finalHandler)

	return newHTTPServer(addr, mux)
}
