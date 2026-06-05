package strutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyTrimmedStringIntMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    map[string]int
		expected map[string]int
	}{
		{
			name:     "nil input returns nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty map returns nil",
			input:    map[string]int{},
			expected: nil,
		},
		{
			name:     "single entry with clean key",
			input:    map[string]int{"key": 42},
			expected: map[string]int{"key": 42},
		},
		{
			name:     "leading whitespace trimmed from key",
			input:    map[string]int{"  key": 10},
			expected: map[string]int{"key": 10},
		},
		{
			name:     "trailing whitespace trimmed from key",
			input:    map[string]int{"key  ": 10},
			expected: map[string]int{"key": 10},
		},
		{
			name:     "leading and trailing whitespace trimmed from key",
			input:    map[string]int{"  key  ": 7},
			expected: map[string]int{"key": 7},
		},
		{
			name:     "tab characters trimmed from key",
			input:    map[string]int{"\tkey\t": 3},
			expected: map[string]int{"key": 3},
		},
		{
			name:     "newline characters trimmed from key",
			input:    map[string]int{"\nkey\n": 5},
			expected: map[string]int{"key": 5},
		},
		{
			name:     "internal whitespace preserved in key",
			input:    map[string]int{"hello world": 1},
			expected: map[string]int{"hello world": 1},
		},
		{
			name:     "multiple entries all trimmed",
			input:    map[string]int{"  a  ": 1, " b ": 2, "c": 3},
			expected: map[string]int{"a": 1, "b": 2, "c": 3},
		},
		{
			name:     "zero value preserved",
			input:    map[string]int{"key": 0},
			expected: map[string]int{"key": 0},
		},
		{
			name:     "negative value preserved",
			input:    map[string]int{"key": -5},
			expected: map[string]int{"key": -5},
		},
		{
			name:     "large positive value preserved",
			input:    map[string]int{"key": 1<<31 - 1},
			expected: map[string]int{"key": 1<<31 - 1},
		},
		{
			name:     "mixed clean and whitespace keys",
			input:    map[string]int{"clean": 1, "  padded  ": 2},
			expected: map[string]int{"clean": 1, "padded": 2},
		},
		{
			name:     "whitespace-only key becomes empty string key",
			input:    map[string]int{"   ": 99},
			expected: map[string]int{"": 99},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := CopyTrimmedStringIntMap(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCopyTrimmedStringIntMap_IsDefensiveCopy(t *testing.T) {
	t.Parallel()

	original := map[string]int{"key": 1}
	copied := CopyTrimmedStringIntMap(original)
	require.NotNil(t, copied)

	// Modifying the copy should not affect the original
	copied["key"] = 999
	assert.Equal(t, 1, original["key"], "original map should not be affected by changes to copy")

	// Modifying the original should not affect the copy
	original["key"] = 42
	assert.Equal(t, 999, copied["key"], "copy should not be affected by changes to original")
}

func TestCopyTrimmedStringIntMap_NilAndEmptyReturnNil(t *testing.T) {
	t.Parallel()

	// Both nil and empty input produce nil output
	nilResult := CopyTrimmedStringIntMap(nil)
	emptyResult := CopyTrimmedStringIntMap(map[string]int{})

	assert.Nil(t, nilResult, "nil input should return nil")
	assert.Nil(t, emptyResult, "empty map input should return nil")
}
