package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/github/gh-aw-mcpg/internal/auth"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/mcp"
	"github.com/github/gh-aw-mcpg/internal/syncutil"
)

var logSession = logger.New("server:session")

// NewSession creates a new Session with the given session ID and optional token
func NewSession(sessionID, token string) *Session {
	logSession.Printf("Creating new session: sessionID=%s, has_token=%v", auth.TruncateSessionID(sessionID), token != "")
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
	logSession.Printf("Extracted session ID from context: %s", auth.TruncateSessionID(sessionID))
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
	logSession.Printf("Checking session: sessionID=%s", auth.TruncateSessionID(sessionID))

	// Use syncutil.GetOrCreate to handle the double-checked locking pattern.
	// The isNew flag is set inside the create callback (while the write lock is held)
	// so that ensureSessionDirectory is called exactly once per new session.
	isNew := false
	if _, err := syncutil.GetOrCreate(&us.sessionMu, us.sessions, sessionID, func() (*Session, error) {
		logSession.Printf("Auto-creating session for ID: %s", auth.TruncateSessionID(sessionID))
		s := NewSession(sessionID, "")
		logSession.Printf("Session auto-created for ID: %s", auth.TruncateSessionID(sessionID))
		isNew = true
		return s, nil
	}); err != nil {
		return err
	}

	if isNew {
		// Ensure session directory exists in payload mount point.
		// Called after GetOrCreate releases the lock to avoid holding it during I/O.
		if err := us.ensureSessionDirectory(sessionID); err != nil {
			logger.LogWarn("client", "Failed to create session directory for session=%s: %v", auth.TruncateSessionID(sessionID), err)
			// Don't fail - payloads will attempt to create the directory when needed
		}
	}

	logSession.Printf("Session validated for ID: %s", auth.TruncateSessionID(sessionID))
	return nil
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

// extractAndValidateSession extracts the session ID from the Authorization header
// and logs connection details. Returns empty string if validation fails.
func extractAndValidateSession(r *http.Request) string {
	logSession.Printf("Extracting session from request: remote=%s, path=%s", r.RemoteAddr, r.URL.Path)

	authHeader := r.Header.Get("Authorization")
	sessionID := auth.ExtractSessionID(authHeader)

	if sessionID == "" {
		logSession.Printf("Session extraction failed: missing or invalid Authorization header, remote=%s", r.RemoteAddr)
		logger.LogError("client", "Rejected MCP client connection: missing or invalid Authorization header, remote=%s, path=%s", r.RemoteAddr, r.URL.Path)
		return ""
	}

	logSession.Printf("Session extracted successfully: sessionID=%s, remote=%s", auth.TruncateSessionID(sessionID), r.RemoteAddr)
	return sessionID
}

// injectSessionContext stores the session ID and optional backend ID into the request context.
// If backendID is empty, only session ID is injected (unified mode).
// Returns the modified request with updated context.
func injectSessionContext(r *http.Request, sessionID, backendID string) *http.Request {
	logSession.Printf("Injecting session context: sessionID=%s, backendID=%s", auth.TruncateSessionID(sessionID), backendID)

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
			r.RemoteAddr, r.Method, r.URL.Path, backendID, auth.TruncateSessionID(sessionID))
	} else {
		logger.LogInfo("client", "MCP connection established, remote=%s, method=%s, path=%s, session=%s",
			r.RemoteAddr, r.Method, r.URL.Path, auth.TruncateSessionID(sessionID))
	}

	logHTTPRequestBody(r, sessionID, backendID)

	*r = *injectSessionContext(r, sessionID, backendID)

	return sessionID, true
}
