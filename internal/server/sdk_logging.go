package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/mcp"
	"github.com/github/gh-aw-mcpg/internal/sanitize"
)

var logSDK = logger.New("server:sdk-frontend")

// WithSDKLogging wraps an SDK StreamableHTTPHandler to log JSON-RPC translation results.
// This captures the request/response at the HTTP boundary to understand what the SDK
// sees and what it returns, particularly for debugging protocol state issues.
func WithSDKLogging(handler http.Handler, mode string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Extract session info for logging context
		sessionID := extractSessionIDFromRequest(r)
		mcpSessionID := r.Header.Get("Mcp-Session-Id")

		// Log incoming request
		logSDK.Printf(">>> SDK Request [%s] session=%s mcp-session=%s method=%s path=%s",
			mode, truncateSessionID(sessionID), truncateSessionID(mcpSessionID), r.Method, r.URL.Path)

		// Capture and log request body for POST requests
		requestBody, err := peekRequestBody(r)
		var jsonrpcReq mcp.Request
		if err == nil && len(requestBody) > 0 {
			// Parse JSON-RPC request
			if err := json.Unmarshal(requestBody, &jsonrpcReq); err == nil {
				logSDK.Printf("    JSON-RPC Request: method=%s id=%v", jsonrpcReq.Method, jsonrpcReq.ID)
				logger.LogDebug("sdk-frontend", "JSON-RPC request parsed: mode=%s, method=%s, id=%v, session=%s",
					mode, jsonrpcReq.Method, jsonrpcReq.ID, truncateSessionID(sessionID))
			} else {
				logSDK.Printf("    Failed to parse JSON-RPC request: %v", err)
				sanitizedBody := sanitize.SanitizeString(string(requestBody))
				logSDK.Printf("    Raw body (sanitized): %.500s", sanitizedBody)
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
						mode, lw.StatusCode, duration)
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
							mode, jsonrpcReq.Method, truncateSessionID(sessionID), errorCode, errorMsg)
					}

					// Log detailed error info for protocol state issues
					if strings.Contains(errorMsg, "session initialization") ||
						strings.Contains(errorMsg, "invalid during") {
						logSDK.Printf("    ⚠️  PROTOCOL STATE ERROR DETECTED")
						logSDK.Printf("    Request method was: %s", jsonrpcReq.Method)
						logSDK.Printf("    Session ID: %s", truncateSessionID(sessionID))
						logSDK.Printf("    MCP-Session-Id header: %s", truncateSessionID(mcpSessionID))
						logSDK.Printf("    This error indicates SDK's StreamableHTTPHandler created fresh protocol state")

						logger.LogWarn("sdk-frontend",
							"Protocol state error: mode=%s, method=%s, session=%s, mcp_session=%s, error=%q",
							mode, jsonrpcReq.Method, truncateSessionID(sessionID),
							truncateSessionID(mcpSessionID), errorMsg)
					} else if (errorCode != -32602 && errorCode != -32601) || jsonrpcReq.Method != "tools/call" {
						// Only log as general error if not already logged above
						logger.LogError("sdk-frontend",
							"JSON-RPC error: mode=%s, method=%s, code=%d, message=%q",
							mode, jsonrpcReq.Method, errorCode, errorMsg)
					}
				} else {
					// Success response
					logSDK.Printf("<<< SDK Response [%s] SUCCESS status=%d duration=%v",
						mode, lw.StatusCode, duration)
					logSDK.Printf("    JSON-RPC Response id=%v has result=%v",
						jsonrpcResp.ID, jsonrpcResp.Result != nil)

					logger.LogDebug("sdk-frontend",
						"JSON-RPC success: mode=%s, method=%s, id=%v, duration=%v",
						mode, jsonrpcReq.Method, jsonrpcResp.ID, duration)
				}
			} else {
				// Could be SSE stream or other format
				logSDK.Printf("<<< SDK Response [%s] status=%d duration=%v (non-JSON or stream)",
					mode, lw.StatusCode, duration)
				if len(responseBody) < 500 {
					logSDK.Printf("    Raw response: %s", string(responseBody))
				} else {
					logSDK.Printf("    Raw response (truncated): %s...", string(responseBody[:500]))
				}
			}
		} else {
			logSDK.Printf("<<< SDK Response [%s] status=%d duration=%v (empty body)",
				mode, lw.StatusCode, duration)
		}
	})
}

// withResponseLogging wraps an http.Handler to log response bodies
func withResponseLogging(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lw := newResponseWriter(w)
		handler.ServeHTTP(lw, r)
		if len(lw.Body()) > 0 {
			sanitizedBody := sanitize.SanitizeString(string(lw.Body()))
			logHelpers.Printf("[%s] %s %s - Status: %d, Response: %s", r.RemoteAddr, r.Method, r.URL.Path, lw.StatusCode, sanitizedBody)
		}
	})
}
