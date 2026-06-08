package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
			result := truncateSessionID(tt.sessionID)
			assert.Equal(t, tt.expected, result)
		})
	}
}
