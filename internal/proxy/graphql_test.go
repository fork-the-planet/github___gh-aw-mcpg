package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTruncateForLog verifies all three branches of truncateForLog:
// the early-exit for non-positive maxRunes, the no-op for short strings,
// and the actual truncation path — including correct Unicode rune handling.
func TestTruncateForLog(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		maxRunes int
		expected string
	}{
		// ── maxRunes ≤ 0 always returns "" ───────────────────────────────────
		{
			name:     "maxRunes zero returns empty string",
			s:        "hello",
			maxRunes: 0,
			expected: "",
		},
		{
			name:     "maxRunes negative returns empty string",
			s:        "hello",
			maxRunes: -1,
			expected: "",
		},
		{
			name:     "maxRunes very negative returns empty string",
			s:        "hello",
			maxRunes: -100,
			expected: "",
		},
		{
			name:     "empty string with zero maxRunes returns empty string",
			s:        "",
			maxRunes: 0,
			expected: "",
		},

		// ── string fits within maxRunes (returned unchanged) ─────────────────
		{
			name:     "empty string with positive maxRunes returns empty string",
			s:        "",
			maxRunes: 10,
			expected: "",
		},
		{
			name:     "string shorter than maxRunes returned unchanged",
			s:        "hello",
			maxRunes: 10,
			expected: "hello",
		},
		{
			name:     "string exactly equal to maxRunes returned unchanged",
			s:        "hello",
			maxRunes: 5,
			expected: "hello",
		},
		{
			name:     "single character string within limit",
			s:        "a",
			maxRunes: 1,
			expected: "a",
		},
		{
			name:     "unicode string within limit unchanged",
			s:        "日本語",
			maxRunes: 10,
			expected: "日本語",
		},
		{
			name:     "unicode string exactly at limit unchanged",
			s:        "日本語",
			maxRunes: 3,
			expected: "日本語",
		},

		// ── truncation path ───────────────────────────────────────────────────
		{
			name:     "ASCII string truncated to first N chars",
			s:        "hello world",
			maxRunes: 5,
			expected: "hello",
		},
		{
			name:     "truncate to single rune",
			s:        "hello",
			maxRunes: 1,
			expected: "h",
		},
		{
			name:     "unicode string truncated by rune count not byte count",
			s:        "日本語テスト",
			maxRunes: 3,
			expected: "日本語",
		},
		{
			name:     "mixed ASCII and unicode truncated at rune boundary",
			s:        "hello日本語",
			maxRunes: 7,
			expected: "hello日本",
		},
		{
			name:     "multibyte euro sign truncated correctly",
			s:        "€€€€€",
			maxRunes: 3,
			expected: "€€€",
		},
		{
			name:     "4-byte emoji runes truncated correctly",
			s:        "😀😃😄😁",
			maxRunes: 2,
			expected: "😀😃",
		},
		{
			name:     "long string truncated at exactly maxRunes runes",
			s:        "abcdefghij",
			maxRunes: 7,
			expected: "abcdefg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateForLog(tt.s, tt.maxRunes)
			assert.Equal(t, tt.expected, got)
		})
	}
}
