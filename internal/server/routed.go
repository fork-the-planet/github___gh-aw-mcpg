package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/syncutil"
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

// CreateHTTPServerForRoutedMode creates an HTTP server for routed mode
// In routed mode, each backend is accessible at /mcp/<server>
// Multiple routes from the same Authorization header share a session
// If apiKey is provided, all requests except /health require authentication (spec 7.1)
// If hmacSecret is provided, routed /mcp/<server> requests must carry a valid
// HMAC-SHA256 signature (ASI-07); common endpoints (e.g. /health, /close) are not HMAC-protected.
func CreateHTTPServerForRoutedMode(addr string, unifiedServer *UnifiedServer, apiKey, hmacSecret string) *http.Server {
	logRouted.Printf("Creating HTTP server for routed mode: addr=%s", addr)

	// Create routes for all configured backend servers.
	// Sys tools are deprecated and intentionally not exposed via /mcp/sys.
	allBackends := unifiedServer.GetServerIDs()
	logRouted.Printf("Registering routes for %d backends: %v", len(allBackends), allBackends)

	return buildMCPHTTPServer(addr, unifiedServer, apiKey, hmacSecret, func(mux *http.ServeMux, sessionTimeout time.Duration) {
		// Create server cache for session-aware server instances.
		// TTL matches the SDK SessionTimeout so cache entries expire with sessions.
		// Long-running agentic workflows (e.g. >30 min GitHub Actions jobs) need this
		// to be at least as long as the workflow to avoid spurious "session not found" errors.
		logRouted.Printf("[CACHE] Creating filtered server cache: ttl=%s, maxSize=%d", sessionTimeout, filteredServerCacheMaxSize)
		serverCache := syncutil.NewTTLCache[string, *sdk.Server](sessionTimeout, filteredServerCacheMaxSize)

		// Create a proxy for each backend server
		for _, serverID := range allBackends {
			// Capture serverID for the closure
			backendID := serverID
			route := fmt.Sprintf("/mcp/%s", backendID)

			// Create the standard MCP handler stack (StreamableHTTP + session auto-init + middleware).
			finalHandler := buildMCPHandler(func(r *http.Request) *sdk.Server {
				if _, ok := setupSessionCallback(r, backendID); !ok {
					return nil
				}

				// Return a cached filtered proxy server for this backend and session.
				// This ensures the same server instance is reused for all requests in a session.
				sessionID := SessionIDFromContext(r.Context())
				cacheKey := fmt.Sprintf("%s/%s", backendID, sessionID)
				return serverCache.GetOrCreate(cacheKey, func() *sdk.Server {
					logRouted.Printf("[CACHE] Creating new filtered server: backend=%s, session=%s", backendID, truncateSessionID(sessionID))
					return createFilteredServer(unifiedServer, backendID)
				})
			}, buildDefaultHandlerConfig(unifiedServer, sessionTimeout, defaultHandlerConfigOptions{
				handlerLog: logRouted,
				logTag:     "routed:" + backendID,
				apiKey:     apiKey,
				hmacSecret: hmacSecret,
			}))

			// Mount the handler at both /mcp/<server> and /mcp/<server>/
			mux.Handle(route+"/", finalHandler)
			mux.Handle(route, finalHandler)
			logRouted.Printf("Registered route: %s", route)
		}
	})
}

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
