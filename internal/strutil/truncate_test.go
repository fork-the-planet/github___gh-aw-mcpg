package strutil

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateWithSuffix(tt.input, tt.maxLen, tt.suffix)
			assert.Equal(t, tt.expected, result)
		})
	}
}
