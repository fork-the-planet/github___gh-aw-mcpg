package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "string shorter than max",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "string equal to max",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "string longer than max",
			input:    "hello world",
			maxLen:   5,
			expected: "hello...",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "zero maxLen with non-empty string",
			input:    "hello",
			maxLen:   0,
			expected: "...",
		},
		{
			name:     "zero maxLen with empty string",
			input:    "",
			maxLen:   0,
			expected: "",
		},
		{
			name:     "negative maxLen",
			input:    "hello",
			maxLen:   -1,
			expected: "hello",
		},
		{
			name:     "very long string",
			input:    "this is a very long string that should be truncated to a reasonable length",
			maxLen:   20,
			expected: "this is a very long ...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Truncate(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTruncateWithSuffix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		suffix   string
		expected string
	}{
		{
			name:     "custom suffix",
			input:    "hello world",
			maxLen:   5,
			suffix:   " (truncated)",
			expected: "hello (truncated)",
		},
		{
			name:     "empty suffix",
			input:    "hello world",
			maxLen:   5,
			suffix:   "",
			expected: "hello",
		},
		{
			name:     "no truncation needed",
			input:    "hi",
			maxLen:   10,
			suffix:   "...",
			expected: "hi",
		},
		{
			name:     "string equal to max",
			input:    "hello",
			maxLen:   5,
			suffix:   "...",
			expected: "hello",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			suffix:   "...",
			expected: "",
		},
		{
			// Unlike Truncate(s, 0) which returns "..." for non-empty strings,
			// TruncateWithSuffix returns the original string unchanged when maxLen <= 0.
			name:     "zero maxLen with non-empty string returns original",
			input:    "hello",
			maxLen:   0,
			suffix:   "...",
			expected: "hello",
		},
		{
			name:     "negative maxLen returns original",
			input:    "hello",
			maxLen:   -1,
			suffix:   "...",
			expected: "hello",
		},
		{
			name:     "very long string",
			input:    "this is a very long string that should be truncated to a reasonable length",
			maxLen:   20,
			suffix:   " (truncated)",
			expected: "this is a very long  (truncated)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateWithSuffix(tt.input, tt.maxLen, tt.suffix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		expected string
	}{
		{
			name:     "ASCII string within limit",
			input:    "hello",
			maxRunes: 10,
			expected: "hello",
		},
		{
			name:     "ASCII string exactly at limit",
			input:    "hello",
			maxRunes: 5,
			expected: "hello",
		},
		{
			name:     "ASCII string exceeds limit",
			input:    "hello world",
			maxRunes: 5,
			expected: "hello",
		},
		{
			name:     "multibyte runes within limit",
			input:    "日本語",
			maxRunes: 5,
			expected: "日本語",
		},
		{
			name:     "multibyte runes truncated",
			input:    "日本語テスト",
			maxRunes: 3,
			expected: "日本語",
		},
		{
			name:     "emoji truncated",
			input:    "😀😁😂😃😄",
			maxRunes: 3,
			expected: "😀😁😂",
		},
		{
			name:     "zero maxRunes returns empty",
			input:    "hello",
			maxRunes: 0,
			expected: "",
		},
		{
			name:     "negative maxRunes returns empty",
			input:    "hello",
			maxRunes: -1,
			expected: "",
		},
		{
			name:     "empty string",
			input:    "",
			maxRunes: 10,
			expected: "",
		},
		{
			name:     "malformed UTF-8 truncated normalizes to RuneError",
			input:    "\xffa",
			maxRunes: 1,
			expected: "\xef\xbf\xbd", // utf8.RuneError encoded as UTF-8
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateRunes(tt.input, tt.maxRunes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkTruncateRunes_NoTruncationASCII(b *testing.B) {
	s := "hello world this is a normal ASCII log message"
	for b.Loop() {
		_ = TruncateRunes(s, 80)
	}
}

func BenchmarkTruncateRunes_NoTruncationMultibyte(b *testing.B) {
	s := "日本語テスト用の文字列サンプル"
	for b.Loop() {
		_ = TruncateRunes(s, 80)
	}
}

func BenchmarkTruncateRunes_TruncationMultibyte(b *testing.B) {
	s := "日本語テスト用の文字列サンプルデータ長めのテキスト"
	for b.Loop() {
		_ = TruncateRunes(s, 5)
	}
}
