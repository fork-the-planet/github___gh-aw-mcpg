package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw-mcpg/internal/auth"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/mcp"
	"github.com/github/gh-aw-mcpg/internal/strutil"
	"github.com/github/gh-aw-mcpg/internal/syncutil"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var logSession = logger.New("server:session")

// truncateSessionID returns a truncated session ID for safe logging (first 8 bytes).
// Returns "(none)" for empty session IDs, and appends "..." for truncated values.
func truncateSessionID(sessionID string) string {
	if sessionID == "" {
		return "(none)"
	}
	return strutil.Truncate(sessionID, 8)
}

// truncateCacheKeyForLog returns a log-safe version of a cache key of the form
// "backendID/sessionID" by truncating the session ID portion.
func truncateCacheKeyForLog(key string) string {
	backendID, sessionID, found := strings.Cut(key, "/")
	if !found {
		return key
	}

	return fmt.Sprintf("%s/%s", backendID, truncateSessionID(sessionID))
}

// extractSessionIDFromRequest extracts the session ID from X-Agent-ID and
// Authorization headers. Returns "" if neither header is present or valid.
func extractSessionIDFromRequest(r *http.Request) string {
	return auth.ExtractSessionIDFromHeaders(
		r.Header.Get("X-Agent-ID"),
		r.Header.Get("Authorization"),
	)
}

// NewSession creates a new Session with the given session ID and optional token
func NewSession(sessionID, token string) *Session {
	logSession.Printf("Creating new session: sessionID=%s, has_token=%v", truncateSessionID(sessionID), token != "")
	return &Session{
		Token:     token,
		SessionID: sessionID,
		StartTime: time.Now(),
		GuardInit: make(map[string]*GuardSessionState),
	}
}

// SessionIDFromContext returns the MCP session ID stored in ctx, or "default" if the
// context contains no session ID (or one of the wrong type). This is the canonical
// place in the server package that reads SessionIDContextKey directly.
func SessionIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(SessionIDContextKey).(string); ok && id != "" {
		return id
	}
	return "default"
}

// getSessionID extracts the MCP session ID from the context
func (us *UnifiedServer) getSessionID(ctx context.Context) string {
	sessionID := SessionIDFromContext(ctx)
	logSession.Printf("Extracted session ID from context: %s", truncateSessionID(sessionID))
	return sessionID
}

// ensureSessionDirectory creates the session subdirectory in the payload directory if it doesn't exist
func (us *UnifiedServer) ensureSessionDirectory(sessionID string) error {
	sessionDir := filepath.Join(us.payloadDir, sessionID)

	// Check if directory already exists
	if _, err := os.Stat(sessionDir); err == nil {
		// Directory already exists
		logSession.Printf("Session directory already exists: %s", sessionDir)
		return nil
	} else if !os.IsNotExist(err) {
		// Some other error occurred while checking
		return fmt.Errorf("failed to check session directory: %w", err)
	}

	// Directory doesn't exist, create it with world-readable permissions (for agent access)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	logSession.Printf("Created session directory: %s", sessionDir)
	return nil
}

// requireSession checks that a session has been initialized for this request
// Sessions are automatically created if one doesn't exist (for standard MCP client compatibility)
func (us *UnifiedServer) requireSession(ctx context.Context) error {
	sessionID := us.getSessionID(ctx)
	logSession.Printf("Checking session: sessionID=%s", truncateSessionID(sessionID))

	// Use syncutil.GetOrCreate to handle the double-checked locking pattern.
	// The isNew flag is set inside the create callback (while the write lock is held)
	// so that ensureSessionDirectory is called exactly once per new session.
	isNew := false
	if _, err := syncutil.GetOrCreate(&us.sessionMu, us.sessions, sessionID, func() (*Session, error) {
		logSession.Printf("Auto-creating session for ID: %s", truncateSessionID(sessionID))
		s := NewSession(sessionID, "")
		logSession.Printf("Session auto-created for ID: %s", truncateSessionID(sessionID))
		isNew = true
		return s, nil
	}); err != nil {
		return err
	}

	if isNew {
		// Ensure session directory exists in payload mount point.
		// Called after GetOrCreate releases the lock to avoid holding it during I/O.
		if err := us.ensureSessionDirectory(sessionID); err != nil {
			logger.LogWarn("client", "Failed to create session directory for session=%s: %v", truncateSessionID(sessionID), err)
			// Don't fail - payloads will attempt to create the directory when needed
		}
	}

	logSession.Printf("Session validated for ID: %s", truncateSessionID(sessionID))
	return nil
}

func (us *UnifiedServer) sysInitHandler(ctx context.Context, req *sdk.CallToolRequest, _ interface{}) (*sdk.CallToolResult, interface{}, error) {
	toolArgs, err := mcp.ParseToolArguments(req)
	if err != nil {
		logger.LogError("client", "Failed to unmarshal sys_init arguments, error=%v", err)
		return mcp.NewErrorCallToolResult(err)
	}

	token := ""
	if t, ok := toolArgs["token"].(string); ok {
		token = t
	}

	sessionID := us.getSessionID(ctx)
	if sessionID == "" {
		logger.LogError("client", "MCP session initialization failed: no session ID provided")
		return mcp.NewErrorCallToolResult(fmt.Errorf("no session ID provided"))
	}

	logger.LogInfo("client", "MCP session initialization started, session=%s, has_token=%v", truncateSessionID(sessionID), token != "")

	us.sessionMu.Lock()
	us.sessions[sessionID] = NewSession(sessionID, token)
	us.sessionMu.Unlock()

	if err := us.ensureSessionDirectory(sessionID); err != nil {
		logger.LogWarn("client", "Failed to create session directory for session=%s: %v", sessionID, err)
	}

	logger.LogInfo("client", "MCP session initialized successfully, session=%s, available_servers=%v", sessionID, us.launcher.ServerIDs())
	return us.callAndLogSysTool(sessionID, "session initialization", "sys_init")
}

func (us *UnifiedServer) sysListServersHandler(ctx context.Context, _ *sdk.CallToolRequest, _ interface{}) (*sdk.CallToolResult, interface{}, error) {
	sessionID := us.getSessionID(ctx)
	logger.LogInfo("client", "MCP sys_list_servers request, session=%s", sessionID)

	if err := us.requireSession(ctx); err != nil {
		logger.LogError("client", "MCP sys_list_servers failed: session not initialized, session=%s", sessionID)
		return mcp.NewErrorCallToolResult(err)
	}

	return us.callAndLogSysTool(sessionID, "sys_list_servers", "sys_list_servers")
}

// getSessionKeys returns a list of active session IDs for debugging
func (us *UnifiedServer) getSessionKeys() []string {
	us.sessionMu.RLock()
	defer us.sessionMu.RUnlock()

	keys := make([]string, 0, len(us.sessions))
	for k := range us.sessions {
		keys = append(keys, k)
	}
	logSession.Printf("Active sessions: count=%d", len(keys))
	return keys
}

// extractAndValidateSession extracts the session ID from request headers.
// and logs connection details. Returns empty string if validation fails.
func extractAndValidateSession(r *http.Request) string {
	logSession.Printf("Extracting session from request: remote=%s, path=%s", r.RemoteAddr, r.URL.Path)

	sessionID := extractSessionIDFromRequest(r)

	if sessionID == "" {
		logSession.Printf("Session extraction failed: missing or invalid X-Agent-ID/Authorization header, remote=%s", r.RemoteAddr)
		logger.LogError("client", "Rejected MCP client connection: missing or invalid X-Agent-ID/Authorization header, remote=%s, path=%s", r.RemoteAddr, r.URL.Path)
		return ""
	}
	if !isSinglePathSegmentSessionID(sessionID) {
		logSession.Printf("Session extraction failed: invalid session identifier format, remote=%s", r.RemoteAddr)
		logger.LogError("client", "Rejected MCP client connection: invalid session identifier format, remote=%s, path=%s", r.RemoteAddr, r.URL.Path)
		return ""
	}

	logSession.Printf("Session extracted successfully: sessionID=%s, remote=%s", truncateSessionID(sessionID), r.RemoteAddr)
	return sessionID
}

func isSinglePathSegmentSessionID(sessionID string) bool {
	if sessionID == "" || sessionID == "." || sessionID == ".." {
		return false
	}
	if filepath.IsAbs(sessionID) || filepath.VolumeName(sessionID) != "" {
		return false
	}
	if strings.Contains(sessionID, "/") || strings.Contains(sessionID, "\\") {
		return false
	}
	if filepath.Base(sessionID) != sessionID {
		return false
	}
	return filepath.Clean(sessionID) == sessionID
}

// injectSessionContext stores the session ID and optional backend ID into the request context.
// If backendID is empty, only session ID is injected (unified mode).
// Returns the modified request with updated context.
func injectSessionContext(r *http.Request, sessionID, backendID string) *http.Request {
	logSession.Printf("Injecting session context: sessionID=%s, backendID=%s", truncateSessionID(sessionID), backendID)

	ctx := context.WithValue(r.Context(), SessionIDContextKey, sessionID)
	ctx = guard.SetAgentIDInContext(ctx, sessionID)

	if backendID != "" {
		logSession.Printf("Adding backend ID to context: backendID=%s", backendID)
		ctx = context.WithValue(ctx, mcp.ContextKey("backend-id"), backendID)
	}

	logSession.Print("Session context injected successfully")
	return r.WithContext(ctx)
}

// setupSessionCallback extracts the session ID, logs the new connection, injects
// the session into the request context, and returns the session ID.
// Used by both routed and unified StreamableHTTP session establishment callbacks.
func setupSessionCallback(r *http.Request, backendID string) (string, bool) {
	sessionID := extractAndValidateSession(r)
	if sessionID == "" {
		return "", false
	}

	if backendID != "" {
		logger.LogInfo("client", "New MCP client connection, remote=%s, method=%s, path=%s, backend=%s, session=%s",
			r.RemoteAddr, r.Method, r.URL.Path, backendID, truncateSessionID(sessionID))
	} else {
		logger.LogInfo("client", "MCP connection established, remote=%s, method=%s, path=%s, session=%s",
			r.RemoteAddr, r.Method, r.URL.Path, truncateSessionID(sessionID))
	}

	logHTTPRequestBody(r, sessionID, backendID)

	*r = *injectSessionContext(r, sessionID, backendID)

	return sessionID, true
}
