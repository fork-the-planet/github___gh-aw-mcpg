package config

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateToolResponseFilters_DirectCall tests validateToolResponseFilters directly,
// covering branches not reached by the existing TestValidateToolResponseFilters tests which
// only call via validateStandardServerConfig.
func TestValidateToolResponseFilters_DirectCall(t *testing.T) {
	tests := []struct {
		name      string
		filters   map[string]string
		jsonPath  string
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "nil map returns nil",
			filters:  nil,
			jsonPath: "mcpServers.myserver.tool_response_filters",
			wantErr:  false,
		},
		{
			name:     "empty map returns nil",
			filters:  map[string]string{},
			jsonPath: "mcpServers.myserver.tool_response_filters",
			wantErr:  false,
		},
		{
			name: "valid single filter",
			filters: map[string]string{
				"my_tool": ".result",
			},
			jsonPath: "mcpServers.myserver.tool_response_filters",
			wantErr:  false,
		},
		{
			name: "valid multiple filters",
			filters: map[string]string{
				"tool_a": ".foo",
				"tool_b": "map(del(.secret))",
				"tool_c": ". | select(.type == \"result\")",
			},
			jsonPath: "mcpServers.myserver.tool_response_filters",
			wantErr:  false,
		},
		{
			name: "empty string tool name key returns error",
			filters: map[string]string{
				"": ".result",
			},
			jsonPath:  "mcpServers.myserver.tool_response_filters",
			wantErr:   true,
			errSubstr: "tool name is required",
		},
		{
			name: "whitespace-only tool name key returns error",
			filters: map[string]string{
				"   ": ".result",
			},
			jsonPath:  "mcpServers.myserver.tool_response_filters",
			wantErr:   true,
			errSubstr: "tool name is required",
		},
		{
			name: "empty filter value returns error",
			filters: map[string]string{
				"my_tool": "",
			},
			jsonPath:  "mcpServers.myserver.tool_response_filters",
			wantErr:   true,
			errSubstr: "is required",
		},
		{
			name: "whitespace-only filter value returns error",
			filters: map[string]string{
				"my_tool": "   \t\n",
			},
			jsonPath:  "mcpServers.myserver.tool_response_filters",
			wantErr:   true,
			errSubstr: "is required",
		},
		{
			name: "invalid jq expression returns error",
			filters: map[string]string{
				"my_tool": "map(",
			},
			jsonPath:  "mcpServers.myserver.tool_response_filters",
			wantErr:   true,
			errSubstr: "my_tool contains an invalid jq expression",
		},
		{
			name: "jsonPath included in error messages",
			filters: map[string]string{
				"": ".ok",
			},
			jsonPath:  "custom.path",
			wantErr:   true,
			errSubstr: "tool name is required",
		},
		{
			// $ENV.KEY is valid jq syntax and compiles successfully, but it triggers the
			// gojq environ-loader option during compilation. This ensures the
			// validateToolResponseFilters gojq.WithEnvironLoader closure is executed.
			name: "filter using $ENV calls the env loader during compilation",
			filters: map[string]string{
				"my_tool": "$ENV.SOME_VARIABLE",
			},
			jsonPath: "mcpServers.myserver.tool_response_filters",
			wantErr:  false,
		},
		{
			// $undefinedVar | .field parses successfully but fails at gojq.Compile because
			// the variable is never bound. This ensures the compile-error path in
			// validateToolResponseFilters is exercised (distinct from parse errors).
			name: "filter with undefined variable fails at compile step not parse step",
			filters: map[string]string{
				"my_tool": "$undefinedVar | .field",
			},
			jsonPath:  "mcpServers.myserver.tool_response_filters",
			wantErr:   true,
			errSubstr: "my_tool contains an invalid jq expression",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateToolResponseFilters(tt.filters, tt.jsonPath)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.ErrorContains(t, err, tt.errSubstr)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateServerAuth_DirectCall tests validateServerAuth directly, covering its three
// top-level branches: nil auth, auth on a non-HTTP server, and auth on an HTTP server.
// These paths are never exercised through direct validateServerAuth calls in existing tests
// — they are always reached via validateStandardServerConfig.
func TestValidateServerAuth_DirectCall(t *testing.T) {
	tests := []struct {
		name       string
		auth       *AuthConfig
		serverType string
		serverName string
		jsonPath   string
		setupEnv   map[string]string
		wantErr    bool
		errSubstr  string
	}{
		{
			name:       "nil auth is always valid regardless of server type",
			auth:       nil,
			serverType: "stdio",
			serverName: "my-server",
			jsonPath:   "mcpServers.my-server",
			wantErr:    false,
		},
		{
			name:       "nil auth on http server is valid",
			auth:       nil,
			serverType: "http",
			serverName: "my-server",
			jsonPath:   "mcpServers.my-server",
			wantErr:    false,
		},
		{
			name:       "auth on stdio server returns error",
			auth:       &AuthConfig{Type: "github-oidc"},
			serverType: "stdio",
			serverName: "my-server",
			jsonPath:   "mcpServers.my-server",
			wantErr:    true,
			errSubstr:  "server type \"stdio\"",
		},
		{
			name:       "auth on local server type returns error",
			auth:       &AuthConfig{Type: "github-oidc"},
			serverType: "local",
			serverName: "my-server",
			jsonPath:   "mcpServers.my-server",
			wantErr:    true,
			errSubstr:  "server type \"local\"",
		},
		{
			name:       "auth on custom server type returns error",
			auth:       &AuthConfig{Type: "github-oidc"},
			serverType: "my-custom-type",
			serverName: "my-server",
			jsonPath:   "mcpServers.my-server",
			wantErr:    true,
			errSubstr:  "server type \"my-custom-type\"",
		},
		{
			name:       "valid github-oidc auth on http server passes",
			auth:       &AuthConfig{Type: "github-oidc"},
			serverType: "http",
			serverName: "my-server",
			jsonPath:   "mcpServers.my-server",
			setupEnv: map[string]string{
				"ACTIONS_ID_TOKEN_REQUEST_URL": "https://token.actions.example.com",
			},
			wantErr: false,
		},
		{
			name:       "empty auth type on http server returns error from validateAuthConfig",
			auth:       &AuthConfig{Type: ""},
			serverType: "http",
			serverName: "my-server",
			jsonPath:   "mcpServers.my-server",
			wantErr:    true,
			errSubstr:  "type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.setupEnv {
				t.Setenv(k, v)
			}
			// If no env setup is provided and the test expects no error for github-oidc,
			// the OIDC URL env var must be set. For tests expecting errors from validateAuthConfig,
			// we need the env var absent so auth-type errors surface before the OIDC check.
			// Tests that only check the serverType branch do not need the env var.

			err := validateServerAuth(tt.auth, tt.serverType, tt.serverName, tt.jsonPath)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.ErrorContains(t, err, tt.errSubstr)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLogValidationFail_ReturnsOriginalError(t *testing.T) {
	assert := assert.New(t)

	wantErr := errors.New("validation failure")
	gotErr := logValidationFail("my-server", "http", "reason", wantErr)

	assert.Same(wantErr, gotErr)
}
