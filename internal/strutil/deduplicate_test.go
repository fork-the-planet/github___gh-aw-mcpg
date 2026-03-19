package strutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
