package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyToolCallLimits(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]int
		want  map[string]int
	}{
		{
			name:  "nil input returns nil",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty map returns nil",
			input: map[string]int{},
			want:  nil,
		},
		{
			name:  "single entry copied",
			input: map[string]int{"get_file": 10},
			want:  map[string]int{"get_file": 10},
		},
		{
			name:  "multiple entries copied",
			input: map[string]int{"get_file": 10, "create_issue": 5, "search_code": 100},
			want:  map[string]int{"get_file": 10, "create_issue": 5, "search_code": 100},
		},
		{
			name:  "keys with surrounding whitespace are trimmed",
			input: map[string]int{"  get_file  ": 10, "\tsearch_code\t": 5},
			want:  map[string]int{"get_file": 10, "search_code": 5},
		},
		{
			name:  "zero limit values are preserved",
			input: map[string]int{"get_file": 0},
			want:  map[string]int{"get_file": 0},
		},
		{
			name:  "negative limit values are preserved",
			input: map[string]int{"get_file": -1},
			want:  map[string]int{"get_file": -1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := copyToolCallLimits(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestCopyToolCallLimits_IsolatesFromOriginal verifies that the returned map is a
// defensive copy — mutations to the copy do not affect the original.
func TestCopyToolCallLimits_IsolatesFromOriginal(t *testing.T) {
	original := map[string]int{"get_file": 10, "create_issue": 5}
	copied := copyToolCallLimits(original)
	require.NotNil(t, copied)

	// Mutate the copy.
	copied["get_file"] = 999
	copied["new_tool"] = 1
	// Original must be unchanged.
	assert.Equal(t, 10, original["get_file"], "original should not be mutated")
	assert.NotContains(t, original, "new_tool", "original should not gain new keys")
}

// TestCopyToolCallLimits_IsolatesOriginalFromCopy verifies that mutations to the
// original after copying do not affect the returned copy.
func TestCopyToolCallLimits_IsolatesOriginalFromCopy(t *testing.T) {
	original := map[string]int{"get_file": 10}
	result := copyToolCallLimits(original)
	require.NotNil(t, result)

	// Mutate the original after copying.
	original["get_file"] = 999
	original["new_tool"] = 1

	assert.Equal(t, 10, result["get_file"], "copy should not reflect original mutations")
	assert.NotContains(t, result, "new_tool", "copy should not gain new keys from original")
}
