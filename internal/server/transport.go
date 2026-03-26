package server

import (
	"net/http"
	"time"

	"github.com/github/gh-aw-mcpg/internal/logger"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var logTransport = logger.New("server:transport")

// CreateHTTPServerForMCP creates an HTTP server that handles MCP over streamable HTTP transport
// If apiKey is provided, all requests except /health require authentication (spec 7.1)
func CreateHTTPServerForMCP(addr string, unifiedServer *UnifiedServer, apiKey string) *http.Server {
	logTransport.Printf("Creating HTTP server for MCP: addr=%s, auth_enabled=%v", addr, apiKey != "")
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
		Stateless:      false,                                         // Support stateful sessions
		Logger:         logger.NewSlogLoggerWithHandler(logTransport), // Integrate SDK logging with project logger
		SessionTimeout: 2 * time.Hour,                                 // Long-running agent workflows can exceed 30 min without MCP activity; 2 h reduces forced reconnects
	})

	// Apply standard middleware stack (SDK logging → shutdown check → auth)
	finalHandler := wrapWithMiddleware(streamableHandler, "unified", unifiedServer, apiKey)

	// Mount handler at /mcp endpoint (logging is done in the callback above)
	mux.Handle("/mcp/", finalHandler)
	mux.Handle("/mcp", finalHandler)

	return &http.Server{
		Addr:    addr,
		Handler: mux,
	}
}
