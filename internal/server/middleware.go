package server

import (
	"net/http"
	"time"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/tracing"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpHandlerConfig holds the non-factory options for buildMCPHandler.
// Using a struct instead of positional parameters makes call sites
// self-documenting and eliminates the risk of swapping the logTag, apiKey,
// and hmacSecret string arguments.
type mcpHandlerConfig struct {
	handlerLog     *logger.Logger
	sessionTimeout time.Duration
	logTag         string
	unifiedServer  *UnifiedServer
	apiKey         string
	hmacSecret     string
}

type defaultHandlerConfigOptions struct {
	handlerLog *logger.Logger
	logTag     string
	apiKey     string
	hmacSecret string
}

func buildDefaultHandlerConfig(unifiedServer *UnifiedServer, sessionTimeout time.Duration, opts defaultHandlerConfigOptions) mcpHandlerConfig {
	return mcpHandlerConfig{
		handlerLog:     opts.handlerLog,
		sessionTimeout: sessionTimeout,
		logTag:         opts.logTag,
		unifiedServer:  unifiedServer,
		apiKey:         opts.apiKey,
		hmacSecret:     opts.hmacSecret,
	}
}

// WithOTELTracing wraps an http.Handler with an OpenTelemetry span for each request.
// The span covers the full HTTP handler lifecycle and includes session ID, HTTP path,
// and method as span attributes. The span context is propagated into the request context
// so that nested spans (e.g. tool call spans) are automatically parented to it.
//
// Incoming W3C traceparent/tracestate headers are extracted so that an
// agent-originated trace is continued; if no such headers are present a fresh
// root span (and new trace ID) is created automatically.
func WithOTELTracing(next http.Handler, tag string) http.Handler {
	// Wrap next with an enrichment handler that adds session ID to the span
	// after the inner handler returns (once the session has been attached to the context).
	// This works because setupSessionCallback uses pointer mutation (*r = *injectSessionContext(...))
	// to update the request struct in-place, so r.Context() after ServeHTTP reflects the session ID.
	enriched := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		sessionID := SessionIDFromContext(r.Context())
		span := oteltrace.SpanFromContext(r.Context())
		span.SetAttributes(tracing.GenAIConversationID.String(truncateSessionID(sessionID)))
	})
	return tracing.WrapHTTPHandler(enriched, "gateway.request", tracing.GatewayTag.String(tag))
}

// wrapWithMiddleware applies the standard middleware stack to an SDK handler.
// The middleware is applied in the following order (outermost-first):
// 1. OTEL tracing (WithOTELTracing) - OpenTelemetry span for the request
// 2. Auth (applyAuthIfConfigured) - Spec 7.1: API key authentication if configured
// 3. HMAC (applyHMACIfConfigured) - ASI-07: signature verification + replay protection
// 4. Shutdown check (rejectIfShutdown) - Spec 5.1.3: Reject requests during shutdown
// 5. SDK logging (WithSDKLogging) - Detailed JSON-RPC translation debugging
//
// Auth runs before HMAC so that unauthenticated requests are rejected cheaply
// without paying the body-read cost of HMAC validation.
//
// This ensures consistent middleware ordering across both routed and unified server modes.
func wrapWithMiddleware(handler http.Handler, logTag string, unifiedServer *UnifiedServer, apiKey, hmacSecret string) http.HandlerFunc {
	logHelpers.Printf("Wrapping handler with middleware: logTag=%s, authEnabled=%v, hmacEnabled=%v", logTag, apiKey != "", hmacSecret != "")

	// Wrap SDK handler with detailed logging for JSON-RPC translation debugging
	loggedHandler := WithSDKLogging(handler, logTag)

	// Apply shutdown check middleware (spec 5.1.3)
	// This must come before auth to ensure shutdown takes precedence
	shutdownHandler := rejectIfShutdown(unifiedServer, loggedHandler, "server:"+logTag)

	// Apply HMAC signature verification if secret is configured (ASI-07).
	// HMAC wraps the shutdown handler so only post-auth requests pay the body-read cost.
	hmacHandler := applyHMACIfConfigured(hmacSecret, shutdownHandler.ServeHTTP)

	// Apply auth middleware if API key is configured (spec 7.1).
	// Auth is the outermost application-level check so unauthenticated requests are
	// rejected before HMAC validation (and its body-read overhead) runs.
	authedHandler := applyAuthIfConfigured(apiKey, hmacHandler)

	// Wrap with OTEL tracing span (outermost, so it covers auth + HMAC + shutdown + logging)
	tracingHandler := WithOTELTracing(authedHandler, logTag)

	logHelpers.Printf("Middleware wrapping complete: logTag=%s", logTag)
	return tracingHandler.ServeHTTP
}

// buildMCPHandler constructs the standard streamable HTTP handler stack used by both
// unified (transport.go) and routed (routed.go) server modes.
//
// The stack (innermost to outermost) is:
//  1. sdk.NewStreamableHTTPHandler - stateful MCP session management
//  2. WrapWithSessionAutoInit - transparent auto-init for clients that skip the
//     MCP initialize handshake (e.g. Gemini CLI v0.37.x)
//  3. wrapWithMiddleware - standard middleware chain (OTEL -> auth -> HMAC ->
//     shutdown check -> SDK logging)
func buildMCPHandler(serverFactory func(*http.Request) *sdk.Server, cfg mcpHandlerConfig) http.Handler {
	h := sdk.NewStreamableHTTPHandler(serverFactory, &sdk.StreamableHTTPOptions{
		Stateless:      false,
		Logger:         logger.NewSlogLoggerWithHandler(cfg.handlerLog),
		SessionTimeout: cfg.sessionTimeout,
	})
	return wrapWithMiddleware(WrapWithSessionAutoInit(h), cfg.logTag, cfg.unifiedServer, cfg.apiKey, cfg.hmacSecret)
}
