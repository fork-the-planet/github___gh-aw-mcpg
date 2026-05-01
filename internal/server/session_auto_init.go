package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/github/gh-aw-mcpg/internal/auth"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logAutoInit = logger.New("server:auto-init")

// autoInitProtocolVersion is the MCP protocol version used for auto-initialization.
const autoInitProtocolVersion = "2025-11-25"

// autoInitClientInfo is the JSON snippet for the clientInfo field in the initialize request.
const autoInitClientInfo = `{"name":"mcpg-auto-init","version":"1.0"}`

// WrapWithSessionAutoInit wraps an MCP streamable HTTP handler to automatically
// initialize sessions for clients that send tools/call before completing the MCP
// session handshake.
//
// This addresses a known compatibility issue with Gemini CLI v0.37.x, which calls
// tools/call before sending initialize + notifications/initialized, causing the SDK
// to reject the request with "method 'tools/call' is invalid during session
// initialization". Without MCP tools working, Gemini cannot complete agentic tasks.
//
// When a tools/call POST is detected without an Mcp-Session-Id header, the
// middleware transparently performs the MCP initialization handshake and retries the
// original request with the established session ID. If auto-init fails for any reason,
// the original request is forwarded unchanged so the SDK can return its usual error.
//
// The handler argument must be the SDK's StreamableHTTPHandler BEFORE any
// authentication or HMAC middleware is applied. The internal initialization requests
// copy the Authorization header from the original request, so authentication is
// preserved without going through the outer middleware stack again.
func WrapWithSessionAutoInit(streamableHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only handle POST requests that have no established session.
		if r.Method != http.MethodPost || r.Header.Get("Mcp-Session-Id") != "" {
			streamableHandler.ServeHTTP(w, r)
			return
		}

		// Peek at the request body to detect tools/call.
		bodyBytes, err := peekRequestBody(r)
		if err != nil || len(bodyBytes) == 0 {
			streamableHandler.ServeHTTP(w, r)
			return
		}

		var rpcReq struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(bodyBytes, &rpcReq); err != nil || rpcReq.Method != "tools/call" {
			streamableHandler.ServeHTTP(w, r)
			return
		}

		// tools/call without session — attempt transparent auto-initialization.
		logAutoInit.Printf("tools/call without Mcp-Session-Id, performing auto-init")
		logger.LogWarn("client",
			"Gemini-compat: tools/call received before session initialization "+
				"(known Gemini CLI v0.37.x issue — see gh-aw-firewall#2348), performing auto-init")

		sessionID, err := performSessionAutoInit(r, streamableHandler)
		if err != nil {
			logAutoInit.Printf("auto-init failed: %v, forwarding original request unchanged", err)
			logger.LogError("client", "Gemini-compat: auto-init failed: %v", err)
			streamableHandler.ServeHTTP(w, r)
			return
		}

		logAutoInit.Printf("auto-init succeeded, session=%s, retrying tools/call",
			auth.TruncateSessionID(sessionID))
		logger.LogInfo("client",
			"Gemini-compat: auto-init succeeded, retrying tools/call with session=%s",
			auth.TruncateSessionID(sessionID))

		// Inject the new session ID and forward the original request.
		r.Header.Set("Mcp-Session-Id", sessionID)
		streamableHandler.ServeHTTP(w, r)
	})
}

// performSessionAutoInit sends initialize + notifications/initialized to the
// given streamable handler using auth context from the original request, and
// returns the session ID established by the SDK.
func performSessionAutoInit(originalReq *http.Request, handler http.Handler) (string, error) {
	ctx := originalReq.Context()

	// Step 1: send initialize.
	initBody := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":%s}}`,
		autoInitProtocolVersion, autoInitClientInfo,
	)
	initReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, originalReq.URL.String(), bytes.NewReader([]byte(initBody)),
	)
	if err != nil {
		return "", fmt.Errorf("building initialize request: %w", err)
	}
	copyAutoInitHeaders(initReq.Header, originalReq.Header)

	initRec := httptest.NewRecorder()
	handler.ServeHTTP(initRec, initReq)

	sessionID := initRec.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		return "", fmt.Errorf("initialize response missing Mcp-Session-Id (status=%d)", initRec.Code)
	}
	if initRec.Code != http.StatusOK {
		return "", fmt.Errorf("initialize returned unexpected status %d", initRec.Code)
	}
	logAutoInit.Printf("initialize OK, session=%s", auth.TruncateSessionID(sessionID))

	// Step 2: send notifications/initialized (fire-and-forget notification).
	// The server returns 202 Accepted; we do not need to inspect the response.
	initdBody := `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`
	initdReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, originalReq.URL.String(), bytes.NewReader([]byte(initdBody)),
	)
	if err != nil {
		// Non-fatal: the session was created; proceed without the notification.
		logAutoInit.Printf("building notifications/initialized request failed: %v (continuing)", err)
	} else {
		copyAutoInitHeaders(initdReq.Header, originalReq.Header)
		initdReq.Header.Set("Mcp-Session-Id", sessionID)
		handler.ServeHTTP(httptest.NewRecorder(), initdReq)
		logAutoInit.Printf("notifications/initialized sent")
	}

	return sessionID, nil
}

// copyAutoInitHeaders copies the HTTP headers required for MCP auto-initialization
// requests from src into dst.
func copyAutoInitHeaders(dst, src http.Header) {
	dst.Set("Content-Type", "application/json")
	dst.Set("Accept", "application/json, text/event-stream")
	if a := src.Get("Authorization"); a != "" {
		dst.Set("Authorization", a)
	}
}
