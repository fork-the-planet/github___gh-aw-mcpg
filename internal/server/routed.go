package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var logRouted = logger.New("server:routed")

// rejectIfShutdown is a middleware that rejects requests with HTTP 503 when gateway is shutting down
// Per spec 5.1.3: "Immediately reject any new RPC requests to /mcp/{server-name} endpoints with HTTP 503"
// The logNamespace parameter is used to create a logger for debug output specific to the call site.
func rejectIfShutdown(unifiedServer *UnifiedServer, next http.Handler, logNamespace string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unifiedServer.IsShutdown() {
			logger.LogWarn("shutdown", "Request rejected during shutdown, remote=%s, path=%s", r.RemoteAddr, r.URL.Path)
			httputil.WriteJSONResponse(w, http.StatusServiceUnavailable, json.RawMessage(shutdownErrorJSON))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// filteredServerCacheMaxSize is the maximum number of entries the filtered
// server cache will hold. When the cache is full, the least-recently-used entry
// is evicted to make room.
const filteredServerCacheMaxSize = 1000

// createFilteredServer creates an MCP server that only exposes tools for a specific backend
// This reuses the unified server's tool handlers, ensuring all calls go through the same session
func createFilteredServer(unifiedServer *UnifiedServer, backendID string) *sdk.Server {
	logRouted.Printf("Creating filtered server: backend=%s", backendID)

	// Create a new SDK server for this route with logger
	server := newSDKServer(fmt.Sprintf("awmg-%s", backendID), logRouted)

	// Get tools for this backend from the unified server
	tools := unifiedServer.GetToolsForBackend(backendID)

	logRouted.Printf("Creating filtered server for %s with %d tools", backendID, len(tools))
	logRouted.Printf("Backend %s has %d tools available", backendID, len(tools))

	// Register each tool (without prefix) using the unified server's handlers
	for _, toolInfo := range tools {
		// Capture for closure
		toolNameCopy := toolInfo.Name

		// Get the unified server's handler for this tool
		handler := unifiedServer.GetToolHandler(backendID, toolInfo.Name)
		if handler == nil {
			logRouted.Printf("WARNING: No handler found for %s___%s", backendID, toolInfo.Name)
			continue
		}

		// Use registerToolWithoutValidation to bypass JSON Schema validation, allowing
		// InputSchema from backends using different JSON Schema versions (e.g., draft-07).
		registerToolWithoutValidation(server, &sdk.Tool{
			Name:        toolInfo.Name, // Without prefix for the client
			Description: toolInfo.Description,
			InputSchema: toolInfo.InputSchema, // Include schema for clients
			Annotations: toolInfo.Annotations, // Preserve readOnly/destructive hints
		}, func(ctx context.Context, req *sdk.CallToolRequest, _ interface{}) (*sdk.CallToolResult, interface{}, error) {
			logRouted.Printf("[ROUTED] Calling unified handler for: %s", toolNameCopy)
			return handler(ctx, req, nil)
		})
	}

	return server
}
