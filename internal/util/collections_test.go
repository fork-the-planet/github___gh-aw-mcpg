package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeduplicateStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		sorted   bool
		expected []string
	}{
		{
			name:     "nil input",
			input:    nil,
			sorted:   false,
			expected: []string{},
		},
		{
			name:     "empty input",
			input:    []string{},
			sorted:   false,
			expected: []string{},
		},
		{
			name:     "single element",
			input:    []string{"a"},
			sorted:   false,
			expected: []string{"a"},
		},
		{
			name:     "no duplicates unsorted",
			input:    []string{"c", "a", "b"},
			sorted:   false,
			expected: []string{"c", "a", "b"},
		},
		{
			name:     "no duplicates sorted",
			input:    []string{"c", "a", "b"},
			sorted:   true,
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "removes duplicates",
			input:    []string{"a", "b", "a", "c", "b"},
			sorted:   false,
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "removes duplicates sorted",
			input:    []string{"b", "a", "b", "c", "a"},
			sorted:   true,
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "trims whitespace",
			input:    []string{"  a  ", "\tb\t", " c"},
			sorted:   false,
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "trims and deduplicates",
			input:    []string{" a", "a ", "a"},
			sorted:   false,
			expected: []string{"a"},
		},
		{
			name:     "skips empty strings",
			input:    []string{"", "a", "", "b"},
			sorted:   false,
			expected: []string{"a", "b"},
		},
		{
			name:     "skips whitespace-only strings",
			input:    []string{"   ", "a", "\t", "b"},
			sorted:   false,
			expected: []string{"a", "b"},
		},
		{
			name:     "all duplicates",
			input:    []string{"a", "a", "a"},
			sorted:   false,
			expected: []string{"a"},
		},
		{
			name:     "all empty",
			input:    []string{"", "  ", "\t"},
			sorted:   false,
			expected: []string{},
		},
		{
			name:     "preserves first-seen order without sort",
			input:    []string{"z", "m", "a", "m", "z"},
			sorted:   false,
			expected: []string{"z", "m", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DeduplicateStrings(tt.input, tt.sorted)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStringsToAny(t *testing.T) {
	t.Parallel()

	t.Run("nil input returns empty (non-nil) slice", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []interface{}{}, StringsToAny(nil))
	})

	t.Run("empty input returns empty slice", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, StringsToAny([]string{}))
	})

	t.Run("converts all entries preserving order", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []interface{}{"octo", "hub", "bot"}, StringsToAny([]string{"octo", "hub", "bot"}))
	})
}

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
