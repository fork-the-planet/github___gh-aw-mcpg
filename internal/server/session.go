package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/github/gh-aw-mcpg/internal/auth"
	"github.com/github/gh-aw-mcpg/internal/logger"
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

// getSessionID extracts the MCP session ID from the context
func (us *UnifiedServer) getSessionID(ctx context.Context) string {
	if sessionID, ok := ctx.Value(SessionIDContextKey).(string); ok && sessionID != "" {
		logSession.Printf("Extracted session ID from context: %s", auth.TruncateSessionID(sessionID))
		return sessionID
	}
	// No session ID in context - this happens before the SDK assigns one
	// For now, use "default" as a placeholder for single-client scenarios
	// In production multi-agent scenarios, the SDK will provide session IDs after initialize
	log.Printf("No session ID in context, using 'default' (this is normal before SDK session is established)")
	return "default"
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
	log.Printf("Created payload directory for session: %s", auth.TruncateSessionID(sessionID))
	return nil
}

// requireSession checks that a session has been initialized for this request
// Sessions are automatically created if one doesn't exist (for standard MCP client compatibility)
func (us *UnifiedServer) requireSession(ctx context.Context) error {
	sessionID := us.getSessionID(ctx)
	logSession.Printf("Checking session: sessionID=%s", auth.TruncateSessionID(sessionID))

	// Use double-checked locking to auto-create session if needed
	us.sessionMu.RLock()
	session := us.sessions[sessionID]
	us.sessionMu.RUnlock()

	if session == nil {
		// Need to create session - acquire write lock
		us.sessionMu.Lock()
		// Double-check after acquiring write lock to avoid race condition
		if us.sessions[sessionID] == nil {
			log.Printf("Auto-creating session for ID: %s", auth.TruncateSessionID(sessionID))
			us.sessions[sessionID] = NewSession(sessionID, "")
			log.Printf("Session auto-created for ID: %s", auth.TruncateSessionID(sessionID))

			// Ensure session directory exists in payload mount point
			// This is done after releasing the lock to avoid holding it during I/O
			us.sessionMu.Unlock()
			if err := us.ensureSessionDirectory(sessionID); err != nil {
				logger.LogWarn("client", "Failed to create session directory for session=%s: %v", auth.TruncateSessionID(sessionID), err)
				// Don't fail - payloads will attempt to create the directory when needed
			}
			return nil
		}
		us.sessionMu.Unlock()
	}

	log.Printf("Session validated for ID: %s", auth.TruncateSessionID(sessionID))
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
