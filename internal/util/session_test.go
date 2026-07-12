package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatSessionIDForLog(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		expected  string
	}{
		{name: "empty session ID returns none", sessionID: "", expected: "(none)"},
		{name: "short session ID returned as-is", sessionID: "abc123", expected: "abc123"},
		{name: "exactly 8 chars returned as-is", sessionID: "abcd1234", expected: "abcd1234"},
		{name: "long session ID truncated", sessionID: "abcdefgh-1234-5678-abcd-ef1234567890", expected: "abcdefgh..."},
		{name: "unicode is truncated by bytes", sessionID: "session-émojis-🔑", expected: "session-..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, FormatSessionIDForLog(tt.sessionID))
		})
	}
}
