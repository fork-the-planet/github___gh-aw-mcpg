package server

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestHasServerGuardPolicies(t *testing.T) {
	allowOnlyPolicy := map[string]interface{}{
		"allow-only": map[string]interface{}{
			"min-integrity": "approved",
			"repos":         []interface{}{"github/gh-aw*"},
		},
	}

	tests := []struct {
		name     string
		cfg      *config.Config
		expected bool
	}{
		{
			name: "single server with guard-policies returns true",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"github": {
						Type:          "stdio",
						GuardPolicies: allowOnlyPolicy,
					},
				},
			},
			expected: true,
		},
		{
			name: "single server without guard-policies returns false",
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
			name: "single server with empty guard-policies map returns false",
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
		{
			name: "no servers returns false",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{},
			},
			expected: false,
		},
		{
			name: "multiple servers all without guard-policies returns false",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"github": {Type: "stdio"},
					"slack":  {Type: "stdio"},
					"jira":   {Type: "stdio"},
				},
			},
			expected: false,
		},
		{
			name: "multiple servers where one has guard-policies returns true",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"github": {Type: "stdio"},
					"slack": {
						Type:          "stdio",
						GuardPolicies: allowOnlyPolicy,
					},
				},
			},
			expected: true,
		},
		{
			name: "multiple servers all with guard-policies returns true",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"github": {
						Type:          "stdio",
						GuardPolicies: allowOnlyPolicy,
					},
					"slack": {
						Type:          "stdio",
						GuardPolicies: allowOnlyPolicy,
					},
				},
			},
			expected: true,
		},
		{
			name: "mix of servers with and without empty guard-policies returns false",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"github": {Type: "stdio"},
					"slack": {
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
			assert.Equal(t, tt.expected, result)
		})
	}
}
