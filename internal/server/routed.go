package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw-mcpg/internal/auth"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/version"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var logRouted = logger.New("server:routed")

func truncateCacheKeyForLog(key string) string {
	backendID, sessionID, found := strings.Cut(key, "/")
	if !found {
		return key
	}

	return fmt.Sprintf("%s/%s", backendID, auth.TruncateSessionID(sessionID))
}

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

// filteredServerCacheMaxSize is the maximum number of entries the filteredServerCache
// will hold. When the cache is full, the least-recently-used entry is evicted to make room.
const filteredServerCacheMaxSize = 1000

// filteredServerCache caches filtered server instances per (backend, session) key.
// Entries are evicted after the configured TTL to prevent unbounded memory growth
// in long-running deployments with many sessions. A max-size cap provides an additional
// safety guard against an unbounded number of unique sessions.
type filteredServerCache struct {
	servers map[string]*filteredServerEntry
	ttl     time.Duration
	maxSize int
	mu      sync.RWMutex
}

type filteredServerEntry struct {
	server   *sdk.Server
	lastUsed time.Time
}

// newFilteredServerCache creates a new server cache with the given entry TTL.
func newFilteredServerCache(ttl time.Duration) *filteredServerCache {
	return &filteredServerCache{
		servers: make(map[string]*filteredServerEntry),
		ttl:     ttl,
		maxSize: filteredServerCacheMaxSize,
	}
}

// getOrCreate returns a cached server or creates a new one.
// Expired entries are lazily evicted on each call. When the cache has reached its
// maximum size, the least-recently-used entry is evicted to make room.
func (c *filteredServerCache) getOrCreate(backendID, sessionID string, creator func() *sdk.Server) *sdk.Server {
	key := fmt.Sprintf("%s/%s", backendID, sessionID)
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Lazy eviction of expired entries
	for k, entry := range c.servers {
		if now.Sub(entry.lastUsed) > c.ttl {
			logRouted.Printf("[CACHE] Evicting expired server: key=%s (idle %s)", truncateCacheKeyForLog(k), now.Sub(entry.lastUsed).Round(time.Second))
			delete(c.servers, k)
		}
	}

	if entry, ok := c.servers[key]; ok {
		entry.lastUsed = now
		return entry.server
	}

	// When at capacity after TTL eviction, evict the least-recently-used entry
	// to bound memory growth reliably. This may interrupt an active session for
	// the evicted (backend, session) pair, but is preferable to unbounded growth.
	if len(c.servers) >= c.maxSize {
		lruKey := ""
		var lruTime time.Time
		first := true
		for k, entry := range c.servers {
			if first || entry.lastUsed.Before(lruTime) {
				lruKey = k
				lruTime = entry.lastUsed
				first = false
			}
		}
		if lruKey != "" {
			logRouted.Printf("[CACHE] Max size reached (%d), evicting LRU entry: key=%s (idle %s)", c.maxSize, truncateCacheKeyForLog(lruKey), now.Sub(lruTime).Round(time.Second))
			delete(c.servers, lruKey)
		}
	}

	logRouted.Printf("[CACHE] Creating new filtered server: backend=%s, session=%s", backendID, auth.TruncateSessionID(sessionID))
	server := creator()
	c.servers[key] = &filteredServerEntry{server: server, lastUsed: now}
	return server
}

// getSessionTimeout returns the session timeout by reading MCP_GATEWAY_SESSION_TIMEOUT
// with a 6-hour default. It is shared by both routed and unified (transport) mode and
// extracted as a package-level function so tests can assert the env-var wiring directly.
func getSessionTimeout() time.Duration {
	return envutil.GetEnvDuration("MCP_GATEWAY_SESSION_TIMEOUT", 6*time.Hour)
}

// CreateHTTPServerForRoutedMode creates an HTTP server for routed mode
// In routed mode, each backend is accessible at /mcp/<server>
// Multiple routes from the same Authorization header share a session
// If apiKey is provided, all requests except /health require authentication (spec 7.1)
// If hmacSecret is provided, routed /mcp/<server> requests must carry a valid
// HMAC-SHA256 signature (ASI-07); common endpoints (e.g. /health, /close) are not HMAC-protected.
func CreateHTTPServerForRoutedMode(addr string, unifiedServer *UnifiedServer, apiKey, hmacSecret string) *http.Server {
	logRouted.Printf("Creating HTTP server for routed mode: addr=%s", addr)
	mux := http.NewServeMux()

	// Register common endpoints (OAuth discovery, health, close)
	registerCommonEndpoints(mux, unifiedServer, apiKey)

	// Create routes for all configured backend servers.
	// Sys tools are deprecated and intentionally not exposed via /mcp/sys.
	allBackends := unifiedServer.GetServerIDs()
	logRouted.Printf("Registering routes for %d backends: %v", len(allBackends), allBackends)

	// Create server cache for session-aware server instances.
	// TTL matches the SDK SessionTimeout so cache entries expire with sessions.
	// Long-running agentic workflows (e.g. >30 min GitHub Actions jobs) need this
	// to be at least as long as the workflow to avoid spurious "session not found" errors.
	routedSessionTimeout := getSessionTimeout()
	serverCache := newFilteredServerCache(routedSessionTimeout)

	// Create a proxy for each backend server
	for _, serverID := range allBackends {
		// Capture serverID for the closure
		backendID := serverID
		route := fmt.Sprintf("/mcp/%s", backendID)

		// Create StreamableHTTP handler for this route
		routeHandler := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
			if _, ok := setupSessionCallback(r, backendID); !ok {
				return nil
			}

			// Return a cached filtered proxy server for this backend and session
			// This ensures the same server instance is reused for all requests in a session
			sessionID := SessionIDFromContext(r.Context())
			return serverCache.getOrCreate(backendID, sessionID, func() *sdk.Server {
				return createFilteredServer(unifiedServer, backendID)
			})
		}, &sdk.StreamableHTTPOptions{
			Stateless:      false,
			Logger:         logger.NewSlogLoggerWithHandler(logRouted),
			SessionTimeout: routedSessionTimeout,
		})

		// Wrap with session auto-init to handle clients (e.g. Gemini CLI v0.37.x) that send
		// tools/call before completing the MCP initialize handshake.
		autoInitHandler := WrapWithSessionAutoInit(routeHandler)

		// Apply standard middleware stack (outermost-first: OTEL tracing → auth → HMAC → shutdown check → SDK logging)
		finalHandler := wrapWithMiddleware(autoInitHandler, "routed:"+backendID, unifiedServer, apiKey, hmacSecret)

		// Mount the handler at both /mcp/<server> and /mcp/<server>/
		mux.Handle(route+"/", finalHandler)
		mux.Handle(route, finalHandler)
		log.Printf("Registered route: %s", route)
	}

	return newHTTPServer(addr, mux)
}

// createFilteredServer creates an MCP server that only exposes tools for a specific backend
// This reuses the unified server's tool handlers, ensuring all calls go through the same session
func createFilteredServer(unifiedServer *UnifiedServer, backendID string) *sdk.Server {
	logRouted.Printf("Creating filtered server: backend=%s", backendID)

	// Create a new SDK server for this route with logger
	server := sdk.NewServer(&sdk.Implementation{
		Name:    fmt.Sprintf("awmg-%s", backendID),
		Version: version.Get(),
	}, &sdk.ServerOptions{
		Logger: logger.NewSlogLoggerWithHandler(logRouted),
	})

	// Get tools for this backend from the unified server
	tools := unifiedServer.GetToolsForBackend(backendID)

	log.Printf("Creating filtered server for %s with %d tools", backendID, len(tools))
	logRouted.Printf("Backend %s has %d tools available", backendID, len(tools))

	// Register each tool (without prefix) using the unified server's handlers
	for _, toolInfo := range tools {
		// Capture for closure
		toolNameCopy := toolInfo.Name

		// Get the unified server's handler for this tool
		handler := unifiedServer.GetToolHandler(backendID, toolInfo.Name)
		if handler == nil {
			log.Printf("WARNING: No handler found for %s___%s", backendID, toolInfo.Name)
			continue
		}

		// Use registerToolWithoutValidation to bypass JSON Schema validation, allowing
		// InputSchema from backends using different JSON Schema versions (e.g., draft-07).
		registerToolWithoutValidation(server, &sdk.Tool{
			Name:        toolInfo.Name, // Without prefix for the client
			Description: toolInfo.Description,
			InputSchema: toolInfo.InputSchema, // Include schema for clients
		}, func(ctx context.Context, req *sdk.CallToolRequest, _ interface{}) (*sdk.CallToolResult, interface{}, error) {
			log.Printf("[ROUTED] Calling unified handler for: %s", toolNameCopy)
			return handler(ctx, req, nil)
		})
	}

	return server
}
