package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/github/gh-aw-mcpg/internal/auth"
	"github.com/github/gh-aw-mcpg/internal/logger"
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
