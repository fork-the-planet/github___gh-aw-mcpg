package server

import (
	"context"
	"testing"
	"time"

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
