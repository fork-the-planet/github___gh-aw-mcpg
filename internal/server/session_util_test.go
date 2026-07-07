package server

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/github/gh-aw-mcpg/internal/util"
)

// TestIsSinglePathSegmentSessionID verifies that isSinglePathSegmentSessionID
// accepts normal session identifiers and rejects empty, dot, dot-dot, absolute,
// and path-traversal inputs that could enable directory traversal attacks.
func TestIsSinglePathSegmentSessionID(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		want      bool
	}{
		// Dot-special inputs — first guard
		{name: "empty string", sessionID: "", want: false},
		{name: "single dot", sessionID: ".", want: false},
		{name: "double dot", sessionID: "..", want: false},

		// Absolute paths — second guard
		{name: "absolute path", sessionID: "/etc/passwd", want: false},
		{name: "root slash", sessionID: "/", want: false},

		// Path-separator inputs — third guard
		{name: "forward slash traversal", sessionID: "path/traversal", want: false},
		{name: "relative traversal", sessionID: "../etc", want: false},
		{name: "current-dir prefix", sessionID: "./session", want: false},
		{name: "backslash traversal", sessionID: `path\traversal`, want: false},

		// Valid single-segment identifiers — happy path
		{name: "simple session ID", sessionID: "my-session", want: true},
		{name: "UUID format", sessionID: "550e8400-e29b-41d4-a716-446655440000", want: true},
		{name: "API key format", sessionID: "ghp_abcdefghijklmnopqrstuvwxyz012345", want: true},
		{name: "hex token", sessionID: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", want: true},
		{name: "single character", sessionID: "x", want: true},
		{name: "numeric string", sessionID: "12345", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isSinglePathSegmentSessionID(tt.sessionID))
		})
	}
}

func TestTruncateSessionID(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		expected  string
	}{
		{name: "empty session ID returns (none)", sessionID: "", expected: "(none)"},
		{name: "short session ID returned as-is", sessionID: "abc123", expected: "abc123"},
		{name: "single character returned as-is", sessionID: "a", expected: "a"},
		{name: "5-char session ID returned as-is", sessionID: "abc12", expected: "abc12"},
		{name: "exactly 8 chars returned as-is", sessionID: "abcd1234", expected: "abcd1234"},
		{name: "exactly 9 chars truncated", sessionID: "abcd12345", expected: "abcd1234..."},
		{
			name:      "very long session ID truncated to 8 chars with ellipsis",
			sessionID: "my-super-long-session-id-with-many-characters-12345678901234567890",
			expected:  "my-super...",
		},
		{name: "session ID with special characters truncated", sessionID: "key!@#$%^&*()", expected: "key!@#$%..."},
		{name: "session ID with unicode truncates by bytes", sessionID: "session-émojis-🔑", expected: "session-..."},
		{
			name:      "UUID format truncated to first 8 chars with ellipsis",
			sessionID: "550e8400-e29b-41d4-a716-446655440000",
			expected:  "550e8400...",
		},
		{name: "whitespace only under 8 chars returned as-is", sessionID: "   ", expected: "   "},
		{name: "whitespace only over 8 chars truncated", sessionID: "         ", expected: "        ..."},
		{
			name:      "long session ID truncated to 8 chars with ellipsis",
			sessionID: "abcdefgh-1234-5678-abcd-ef1234567890",
			expected:  "abcdefgh...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := util.FormatSessionIDForLog(tt.sessionID)
			assert.Equal(t, tt.expected, result)
		})
	}
}
