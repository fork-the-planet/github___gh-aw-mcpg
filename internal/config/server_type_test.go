package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsStdioServerType(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"", true},
		{"stdio", true},
		{"local", true},
		{"http", false},
		{"custom", false},
		{"STDIO", false},
		{"LOCAL", false},
	}

	for _, tc := range tests {
		t.Run("type="+tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, IsStdioServerType(tc.input))
		})
	}
}

func TestNormalizeServerType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "stdio"},
		{"local", "stdio"},
		{"stdio", "stdio"},
		{"http", "http"},
		{"custom", "custom"},
	}

	for _, tc := range tests {
		t.Run("type="+tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, NormalizeServerType(tc.input))
		})
	}
}
