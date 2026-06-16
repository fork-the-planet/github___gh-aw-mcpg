package strutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionSuffix(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		want      string
	}{
		{
			name:      "with session ID",
			sessionID: "test-session-123",
			want:      " for session 'test-session-123'",
		},
		{
			name:      "empty session ID",
			sessionID: "",
			want:      "",
		},
		{
			name:      "session ID with special characters",
			sessionID: "session-with-dashes_and_underscores.123",
			want:      " for session 'session-with-dashes_and_underscores.123'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SessionSuffix(tt.sessionID)
			assert.Equal(t, tt.want, got)
		})
	}
}
