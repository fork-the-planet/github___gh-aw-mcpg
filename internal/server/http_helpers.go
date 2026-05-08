package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/github/gh-aw-mcpg/internal/auth"
	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/logger/sanitize"
	"github.com/github/gh-aw-mcpg/internal/mcp"
	"github.com/github/gh-aw-mcpg/internal/tracing"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var logHelpers = logger.New("server:helpers")
var logSDK = logger.New("server:sdk-frontend")

// logRuntimeError logs runtime errors to stdout per spec section 9.2
func logRuntimeError(errorType, detail string, r *http.Request, serverName *string) {
	logHelpers.Printf("Logging runtime error: type=%s, detail=%s", errorType, detail)

	timestamp := time.Now().UTC().Format(time.RFC3339)
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = "unknown"
	}

	server := "gateway"
	if serverName != nil {
		server = *serverName
	}

	// Spec 9.2: Log to stdout with timestamp, server name, request ID, error details
	log.Printf("[ERROR] timestamp=%s server=%s request_id=%s error_type=%s detail=%s path=%s method=%s",
		timestamp, server, requestID, errorType, detail, r.URL.Path, r.Method)
}

// rejectRequest logs a structured error, records a runtime error, and writes an
// HTTP error response. This consolidates the 3-step rejection pattern that was
// previously duplicated across auth and handler code paths.
//
// Parameters:
//   - logCategory: category for the structured log (e.g. "auth")
//   - runtimeErrType: error_type field for runtime error log (e.g. "authentication_failed")
//   - runtimeDetail: detail field for runtime error log (e.g. "missing_auth_header")
//   - msg: human-readable message sent back in the HTTP response
func rejectRequest(w http.ResponseWriter, r *http.Request, status int, code, msg, logCategory, runtimeErrType, runtimeDetail string) {
	logger.LogErrorMd(logCategory, "Request rejected: %s, remote=%s, path=%s", msg, r.RemoteAddr, r.URL.Path)
	logRuntimeError(runtimeErrType, runtimeDetail, r, nil)
	httputil.WriteErrorResponse(w, status, code, msg)
}

// withResponseLogging wraps an http.Handler to log response bodies
func withResponseLogging(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lw := newResponseWriter(w)
		handler.ServeHTTP(lw, r)
		if len(lw.Body()) > 0 {
			sanitizedBody := sanitize.SanitizeString(string(lw.Body()))
			logHelpers.Printf("[%s] %s %s - Status: %d, Response: %s", r.RemoteAddr, r.Method, r.URL.Path, lw.StatusCode(), sanitizedBody)
		}
	})
}

// peekRequestBody reads all bytes from a POST request body and restores it
// so downstream handlers can read it again.
// Returns nil, nil for non-POST requests or requests with no body.
func peekRequestBody(r *http.Request) ([]byte, error) {
	if r.Method != http.MethodPost || r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}

	origBody := r.Body
	b, err := io.ReadAll(origBody)
	closeErr := origBody.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}

	if len(b) == 0 {
		r.Body = http.NoBody
		return b, nil
	}

	r.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}

// logHTTPRequestBody logs the request body for debugging purposes.
// It reads the body, logs it, and restores it so it can be read again.
// The backendID parameter is optional and can be empty for unified mode.
func logHTTPRequestBody(r *http.Request, sessionID, backendID string) {
	logHelpers.Printf("Checking request body: method=%s, hasBody=%v, sessionID=%s", r.Method, r.Body != nil, auth.TruncateSessionID(sessionID))

	bodyBytes, err := peekRequestBody(r)
	if err != nil {
		logHelpers.Printf("Body read failed: err=%v", err)
		return
	}
	if len(bodyBytes) == 0 {
		logHelpers.Printf("Skipping body logging: not a POST request, no body present, or empty body")
		return
	}

	logHelpers.Printf("Request body read: size=%d bytes, sessionID=%s, backendID=%s", len(bodyBytes), auth.TruncateSessionID(sessionID), backendID)

	sanitizedBody := sanitize.SanitizeString(string(bodyBytes))

	if backendID != "" {
		logger.LogDebug("client", "MCP client request body, backend=%s, body=%s", backendID, sanitizedBody)
	} else {
		logger.LogDebug("client", "MCP request body, session=%s, body=%s", auth.TruncateSessionID(sessionID), sanitizedBody)
	}
	logHelpers.Print("Request body logged for debugging")
}

func truncateCacheKeyForLog(key string) string {
	backendID, sessionID, found := strings.Cut(key, "/")
	if !found {
		return key
	}

	return fmt.Sprintf("%s/%s", backendID, auth.TruncateSessionID(sessionID))
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
		span.SetAttributes(attribute.String("session.id", auth.TruncateSessionID(sessionID)))
	})
	return tracing.WrapHTTPHandler(enriched, "gateway.request", attribute.String("gateway.tag", tag))
}

// applyIfConfigured wraps handler with middleware(key, handler) when key is non-empty.
// If key is empty the handler is returned unchanged.
func applyIfConfigured(key string, handler http.HandlerFunc, middleware func(string, http.HandlerFunc) http.HandlerFunc) http.HandlerFunc {
	if key != "" {
		return middleware(key, handler)
	}
	return handler
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

// buildMCPHandler constructs the standard streamable HTTP handler stack used by both
// unified (transport.go) and routed (routed.go) server modes.
//
// The stack (innermost to outermost) is:
//  1. sdk.NewStreamableHTTPHandler – stateful MCP session management
//  2. WrapWithSessionAutoInit – transparent auto-init for clients that skip the
//     MCP initialize handshake (e.g. Gemini CLI v0.37.x)
//  3. wrapWithMiddleware – standard middleware chain (OTEL → auth → HMAC →
//     shutdown check → SDK logging)
func buildMCPHandler(serverFactory func(*http.Request) *sdk.Server, cfg mcpHandlerConfig) http.Handler {
	h := sdk.NewStreamableHTTPHandler(serverFactory, &sdk.StreamableHTTPOptions{
		Stateless:      false,
		Logger:         logger.NewSlogLoggerWithHandler(cfg.handlerLog),
		SessionTimeout: cfg.sessionTimeout,
	})
	return wrapWithMiddleware(WrapWithSessionAutoInit(h), cfg.logTag, cfg.unifiedServer, cfg.apiKey, cfg.hmacSecret)
}

// WithSDKLogging wraps an SDK StreamableHTTPHandler to log JSON-RPC translation results.
// This captures the request/response at the HTTP boundary to understand what the SDK
// sees and what it returns, particularly for debugging protocol state issues.
func WithSDKLogging(handler http.Handler, mode string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Extract session info for logging context
		authHeader := r.Header.Get("Authorization")
		sessionID := auth.ExtractSessionID(authHeader)
		mcpSessionID := r.Header.Get("Mcp-Session-Id")

		// Log incoming request
		logSDK.Printf(">>> SDK Request [%s] session=%s mcp-session=%s method=%s path=%s",
			mode, auth.TruncateSessionID(sessionID), auth.TruncateSessionID(mcpSessionID), r.Method, r.URL.Path)

		// Capture and log request body for POST requests
		requestBody, err := peekRequestBody(r)
		var jsonrpcReq mcp.Request
		if err == nil && len(requestBody) > 0 {
			// Parse JSON-RPC request
			if err := json.Unmarshal(requestBody, &jsonrpcReq); err == nil {
				logSDK.Printf("    JSON-RPC Request: method=%s id=%v", jsonrpcReq.Method, jsonrpcReq.ID)
				logger.LogDebug("sdk-frontend", "JSON-RPC request parsed: mode=%s, method=%s, id=%v, session=%s",
					mode, jsonrpcReq.Method, jsonrpcReq.ID, auth.TruncateSessionID(sessionID))
			} else {
				logSDK.Printf("    Failed to parse JSON-RPC request: %v", err)
				logSDK.Printf("    Raw body: %s", string(requestBody))
			}
		}

		// Wrap response writer to capture output
		lw := newResponseWriter(w)

		// Call the actual SDK handler
		handler.ServeHTTP(lw, r)

		duration := time.Since(startTime)

		// Parse and log response
		responseBody := lw.Body()
		if len(responseBody) > 0 {
			// Try to parse as JSON-RPC response
			var jsonrpcResp mcp.Response
			if err := json.Unmarshal(responseBody, &jsonrpcResp); err == nil {
				if jsonrpcResp.Error != nil {
					// Error response - this is what we're particularly interested in
					logSDK.Printf("<<< SDK Response [%s] ERROR status=%d duration=%v",
						mode, lw.StatusCode(), duration)
					logSDK.Printf("    JSON-RPC Error: code=%d message=%q",
						jsonrpcResp.Error.Code, jsonrpcResp.Error.Message)

					// Check for specific error types
					errorCode := jsonrpcResp.Error.Code
					errorMsg := jsonrpcResp.Error.Message

					// Log tool not found errors specifically for better monitoring
					// Error code -32602 (Invalid params) is used by the SDK for unknown tools
					// Error code -32601 (Method not found) could also indicate tool issues
					// We check the method to ensure this is a tools/call request
					if (errorCode == -32602 || errorCode == -32601) && jsonrpcReq.Method == "tools/call" {
						logSDK.Printf("    ⚠️  TOOL NOT FOUND ERROR")
						logger.LogWarn("client",
							"Tool not found: mode=%s, method=%s, session=%s, code=%d, message=%q",
							mode, jsonrpcReq.Method, auth.TruncateSessionID(sessionID), errorCode, errorMsg)
					}

					// Log detailed error info for protocol state issues
					if strings.Contains(errorMsg, "session initialization") ||
						strings.Contains(errorMsg, "invalid during") {
						logSDK.Printf("    ⚠️  PROTOCOL STATE ERROR DETECTED")
						logSDK.Printf("    Request method was: %s", jsonrpcReq.Method)
						logSDK.Printf("    Session ID: %s", auth.TruncateSessionID(sessionID))
						logSDK.Printf("    MCP-Session-Id header: %s", auth.TruncateSessionID(mcpSessionID))
						logSDK.Printf("    This error indicates SDK's StreamableHTTPHandler created fresh protocol state")

						logger.LogWarn("sdk-frontend",
							"Protocol state error: mode=%s, method=%s, session=%s, mcp_session=%s, error=%q",
							mode, jsonrpcReq.Method, auth.TruncateSessionID(sessionID),
							auth.TruncateSessionID(mcpSessionID), errorMsg)
					} else if (errorCode != -32602 && errorCode != -32601) || jsonrpcReq.Method != "tools/call" {
						// Only log as general error if not already logged above
						logger.LogError("sdk-frontend",
							"JSON-RPC error: mode=%s, method=%s, code=%d, message=%q",
							mode, jsonrpcReq.Method, errorCode, errorMsg)
					}
				} else {
					// Success response
					logSDK.Printf("<<< SDK Response [%s] SUCCESS status=%d duration=%v",
						mode, lw.StatusCode(), duration)
					logSDK.Printf("    JSON-RPC Response id=%v has result=%v",
						jsonrpcResp.ID, jsonrpcResp.Result != nil)

					logger.LogDebug("sdk-frontend",
						"JSON-RPC success: mode=%s, method=%s, id=%v, duration=%v",
						mode, jsonrpcReq.Method, jsonrpcResp.ID, duration)
				}
			} else {
				// Could be SSE stream or other format
				logSDK.Printf("<<< SDK Response [%s] status=%d duration=%v (non-JSON or stream)",
					mode, lw.StatusCode(), duration)
				if len(responseBody) < 500 {
					logSDK.Printf("    Raw response: %s", string(responseBody))
				} else {
					logSDK.Printf("    Raw response (truncated): %s...", string(responseBody[:500]))
				}
			}
		} else {
			logSDK.Printf("<<< SDK Response [%s] status=%d duration=%v (empty body)",
				mode, lw.StatusCode(), duration)
		}
	})
}
