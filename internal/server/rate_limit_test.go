package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsRateLimitText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "rate limit exceeded lowercase",
			text: "rate limit exceeded",
			want: true,
		},
		{
			name: "API rate limit exceeded",
			text: "API rate limit exceeded for user ID 12345",
			want: true,
		},
		{
			name: "rate limit with 403",
			text: "rate limit 403 Forbidden",
			want: true,
		},
		{
			name: "secondary rate limit",
			text: "You have exceeded a secondary rate limit",
			want: true,
		},
		{
			name: "too many requests",
			text: "too many requests, please slow down",
			want: true,
		},
		{
			name: "uppercase RATE LIMIT EXCEEDED",
			text: "RATE LIMIT EXCEEDED",
			want: true,
		},
		{
			name: "mixed case Rate Limit Exceeded",
			text: "Rate Limit Exceeded for this endpoint",
			want: true,
		},
		{
			name: "too many requests mixed case",
			text: "Too Many Requests",
			want: true,
		},
		{
			name: "normal error message",
			text: "repository not found",
			want: false,
		},
		{
			name: "empty string",
			text: "",
			want: false,
		},
		{
			name: "unrelated 403 error",
			text: "403 Forbidden: access denied",
			want: false,
		},
		{
			name: "partial match rate only",
			text: "rate of change is high",
			want: false,
		},
		{
			name: "limit only",
			text: "limit reached for this feature",
			want: false,
		},
		{
			name: "api rate limit without 403",
			text: "api rate limit reset in 60s",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isRateLimitText(tt.text)
			assert.Equal(t, tt.want, got)
		})
	}
}
