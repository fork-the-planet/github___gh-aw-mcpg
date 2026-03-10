package server

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestHasServerGuardPolicies(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected bool
	}{
		{
			name: "server with guard-policies should return true",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"github": {
						Type: "stdio",
						GuardPolicies: map[string]interface{}{
							"allow-only": map[string]interface{}{
								"min-integrity": "approved",
								"repos":         []interface{}{"github/gh-aw*"},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "server without guard-policies should return false",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"github": {
						Type: "stdio",
					},
				},
			},
			expected: false,
		},
		{
			name: "server with empty guard-policies should return false",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"github": {
						Type:          "stdio",
						GuardPolicies: map[string]interface{}{},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasServerGuardPolicies(tt.cfg)
			assert.Equal(t, tt.expected, result, "hasServerGuardPolicies should return %v", tt.expected)
		})
	}
}
