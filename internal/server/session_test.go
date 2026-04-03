package server

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionIDFromContext directly tests the exported canonical API for
// reading session IDs from context. SessionIDFromContext is the canonical safe
// reader for SessionIDContextKey and returns "default" when the key is absent,
// empty, or carries a non-string value.
func TestSessionIDFromContext(t *testing.T) {
	tests := []struct {
		name   string
		setup  func() context.Context
		wantID string
	}{
		{
			name:   "absent key returns default",
			setup:  func() context.Context { return context.Background() },
			wantID: "default",
		},
		{
			name: "empty string returns default",
			setup: func() context.Context {
				return context.WithValue(context.Background(), SessionIDContextKey, "")
			},
			wantID: "default",
		},
		{
			name: "non-string value returns default",
			setup: func() context.Context {
				return context.WithValue(context.Background(), SessionIDContextKey, 42)
			},
			wantID: "default",
		},
		{
			name: "nil value returns default",
			setup: func() context.Context {
				return context.WithValue(context.Background(), SessionIDContextKey, nil)
			},
			wantID: "default",
		},
		{
			name: "valid session ID is returned unchanged",
			setup: func() context.Context {
				return context.WithValue(context.Background(), SessionIDContextKey, "abc-123")
			},
			wantID: "abc-123",
		},
		{
			name: "API-key-style session ID preserved",
			setup: func() context.Context {
				return context.WithValue(context.Background(), SessionIDContextKey, "ghp_abcdefghijklmnopqrstuvwxyz0123456789")
			},
			wantID: "ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		},
		{
			name: "closest context value wins (innermost wins)",
			setup: func() context.Context {
				outer := context.WithValue(context.Background(), SessionIDContextKey, "outer-session")
				return context.WithValue(outer, SessionIDContextKey, "inner-session")
			},
			wantID: "inner-session",
		},
		{
			name: "inner empty string falls through to default, not outer value",
			setup: func() context.Context {
				// Go's context.Value returns the innermost matching key, so the
				// empty string from the inner value is what the type-assertion sees.
				// SessionIDFromContext must treat an empty string as absent.
				outer := context.WithValue(context.Background(), SessionIDContextKey, "outer-session")
				return context.WithValue(outer, SessionIDContextKey, "")
			},
			wantID: "default",
		},
		{
			name: "SessionIDContextKey and mcp.SessionIDContextKey are the same key",
			setup: func() context.Context {
				// Verify that server.SessionIDContextKey is the same key as
				// mcp.SessionIDContextKey so that SDK middleware and server code
				// share the same context slot.
				return context.WithValue(context.Background(), mcp.SessionIDContextKey, "shared-session")
			},
			wantID: "shared-session",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setup()
			got := SessionIDFromContext(ctx)
			assert.Equal(t, tt.wantID, got)
		})
	}
}

// TestNewSession verifies that NewSession initialises all required fields.
func TestNewSession(t *testing.T) {
	before := time.Now()
	s := NewSession("test-session-id", "test-token")
	after := time.Now()

	require.NotNil(t, s)
	assert.Equal(t, "test-session-id", s.SessionID)
	assert.Equal(t, "test-token", s.Token)
	assert.False(t, s.StartTime.IsZero(), "StartTime should be set")
	assert.True(t, !s.StartTime.Before(before) && !s.StartTime.After(after),
		"StartTime should be between before and after")
	assert.NotNil(t, s.GuardInit, "GuardInit map must be initialised (non-nil)")
	assert.Empty(t, s.GuardInit, "GuardInit map should be empty for a new session")
}

// TestNewSession_EmptyToken verifies that an empty token is allowed.
func TestNewSession_EmptyToken(t *testing.T) {
	s := NewSession("session-no-token", "")
	require.NotNil(t, s)
	assert.Equal(t, "", s.Token)
	assert.Equal(t, "session-no-token", s.SessionID)
}

// TestNewSession_GuardInitNotShared verifies that each session gets its own
// GuardInit map; mutations on one session must not affect another.
func TestNewSession_GuardInitNotShared(t *testing.T) {
	s1 := NewSession("s1", "")
	s2 := NewSession("s2", "")

	s1.GuardInit["guard-a"] = &GuardSessionState{Initialized: true}

	assert.Len(t, s1.GuardInit, 1)
	assert.Empty(t, s2.GuardInit, "s2.GuardInit must not be affected by writes to s1.GuardInit")
}

// newMinimalUnifiedServerForSessionTest creates a UnifiedServer with an empty config for
// use in session-related tests.
func newMinimalUnifiedServerForSessionTest(t *testing.T) *UnifiedServer {
	t.Helper()
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{},
	}
	us, err := NewUnified(context.Background(), cfg)
	require.NoError(t, err, "NewUnified should not fail with an empty config")
	t.Cleanup(func() { us.Close() })
	return us
}

// TestGetSessionID verifies that getSessionID is a thin wrapper around
// SessionIDFromContext, returning the same ID (or "default") for all inputs.
func TestGetSessionID(t *testing.T) {
	us := newMinimalUnifiedServerForSessionTest(t)

	tests := []struct {
		name   string
		ctx    context.Context
		wantID string
	}{
		{
			name:   "context without session ID returns default",
			ctx:    context.Background(),
			wantID: "default",
		},
		{
			name:   "context with valid session ID returns that ID",
			ctx:    context.WithValue(context.Background(), SessionIDContextKey, "my-session"),
			wantID: "my-session",
		},
		{
			name:   "context with empty session ID returns default",
			ctx:    context.WithValue(context.Background(), SessionIDContextKey, ""),
			wantID: "default",
		},
		{
			name:   "context with non-string value returns default",
			ctx:    context.WithValue(context.Background(), SessionIDContextKey, 99),
			wantID: "default",
		},
		{
			name:   "result matches SessionIDFromContext",
			ctx:    context.WithValue(context.Background(), SessionIDContextKey, "canonical-session"),
			wantID: SessionIDFromContext(context.WithValue(context.Background(), SessionIDContextKey, "canonical-session")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := us.getSessionID(tt.ctx)
			assert.Equal(t, tt.wantID, got)
		})
	}
}

// TestEnsureSessionDirectory verifies that ensureSessionDirectory creates the
// expected per-session subdirectory inside payloadDir.
func TestEnsureSessionDirectory(t *testing.T) {
	us := newMinimalUnifiedServerForSessionTest(t)

	t.Run("creates session directory under payloadDir", func(t *testing.T) {
		us.payloadDir = t.TempDir()

		err := us.ensureSessionDirectory("test-session")
		require.NoError(t, err)

		assert.DirExists(t, filepath.Join(us.payloadDir, "test-session"))
	})

	t.Run("idempotent: second call does not error", func(t *testing.T) {
		us.payloadDir = t.TempDir()

		require.NoError(t, us.ensureSessionDirectory("idempotent-session"))
		assert.NoError(t, us.ensureSessionDirectory("idempotent-session"))
	})

	t.Run("each session gets its own subdirectory", func(t *testing.T) {
		us.payloadDir = t.TempDir()

		require.NoError(t, us.ensureSessionDirectory("session-alpha"))
		require.NoError(t, us.ensureSessionDirectory("session-beta"))

		assert.DirExists(t, filepath.Join(us.payloadDir, "session-alpha"))
		assert.DirExists(t, filepath.Join(us.payloadDir, "session-beta"))
	})
}

// TestRequireSession verifies that requireSession auto-creates a new Session
// the first time a session ID is seen and reuses the same Session on subsequent calls.
func TestRequireSession_SessionManagement(t *testing.T) {
	t.Run("auto-creates session for new session ID", func(t *testing.T) {
		us := newMinimalUnifiedServerForSessionTest(t)
		us.payloadDir = t.TempDir()

		ctx := context.WithValue(context.Background(), SessionIDContextKey, "brand-new-session")
		require.NoError(t, us.requireSession(ctx))

		us.sessionMu.RLock()
		_, exists := us.sessions["brand-new-session"]
		us.sessionMu.RUnlock()

		assert.True(t, exists, "session should have been auto-created by requireSession")
	})

	t.Run("uses default session ID when none in context", func(t *testing.T) {
		us := newMinimalUnifiedServerForSessionTest(t)
		us.payloadDir = t.TempDir()

		require.NoError(t, us.requireSession(context.Background()))

		us.sessionMu.RLock()
		_, exists := us.sessions["default"]
		us.sessionMu.RUnlock()

		assert.True(t, exists, "default session should be created when no ID is in context")
	})

	t.Run("returns same session on repeated calls", func(t *testing.T) {
		us := newMinimalUnifiedServerForSessionTest(t)
		us.payloadDir = t.TempDir()

		ctx := context.WithValue(context.Background(), SessionIDContextKey, "stable-session")

		require.NoError(t, us.requireSession(ctx))
		us.sessionMu.RLock()
		first := us.sessions["stable-session"]
		us.sessionMu.RUnlock()

		require.NoError(t, us.requireSession(ctx))
		us.sessionMu.RLock()
		second := us.sessions["stable-session"]
		us.sessionMu.RUnlock()

		assert.Same(t, first, second, "requireSession should return the same *Session on repeated calls")
	})

	t.Run("concurrent calls create the session exactly once", func(t *testing.T) {
		us := newMinimalUnifiedServerForSessionTest(t)
		us.payloadDir = t.TempDir()

		ctx := context.WithValue(context.Background(), SessionIDContextKey, "concurrent-session")

		var wg sync.WaitGroup
		const goroutines = 20
		wg.Add(goroutines)
		for range goroutines {
			go func() {
				defer wg.Done()
				assert.NoError(t, us.requireSession(ctx))
			}()
		}
		wg.Wait()

		us.sessionMu.RLock()
		count := len(us.sessions)
		us.sessionMu.RUnlock()
		assert.Equal(t, 1, count, "concurrent requireSession calls should create exactly one session")
	})
}

// TestGetSessionKeys verifies that getSessionKeys returns all currently active
// session IDs and is consistent with the sessions map.
func TestGetSessionKeys(t *testing.T) {
	t.Run("returns empty slice when no sessions exist", func(t *testing.T) {
		us := newMinimalUnifiedServerForSessionTest(t)
		assert.Empty(t, us.getSessionKeys())
	})

	t.Run("returns all session IDs after creation", func(t *testing.T) {
		us := newMinimalUnifiedServerForSessionTest(t)
		us.payloadDir = t.TempDir()

		sessionIDs := []string{"session-a", "session-b", "session-c"}
		for _, id := range sessionIDs {
			ctx := context.WithValue(context.Background(), SessionIDContextKey, id)
			require.NoError(t, us.requireSession(ctx))
		}

		keys := us.getSessionKeys()
		assert.Len(t, keys, len(sessionIDs))
		assert.ElementsMatch(t, sessionIDs, keys)
	})

	t.Run("count matches sessions map length", func(t *testing.T) {
		us := newMinimalUnifiedServerForSessionTest(t)
		us.payloadDir = t.TempDir()

		for _, id := range []string{"x", "y", "z"} {
			ctx := context.WithValue(context.Background(), SessionIDContextKey, id)
			require.NoError(t, us.requireSession(ctx))
		}

		keys := us.getSessionKeys()

		us.sessionMu.RLock()
		mapLen := len(us.sessions)
		us.sessionMu.RUnlock()

		assert.Len(t, keys, mapLen)
	})
}
