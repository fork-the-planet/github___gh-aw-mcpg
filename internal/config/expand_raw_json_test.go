package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExpandRawJSONVariables tests the exported ExpandRawJSONVariables function directly.
// This function is the primary entry point for expanding ${VAR} expressions in raw JSON
// before schema validation, and is called by LoadFromStdin.
func TestExpandRawJSONVariables(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		envVars   map[string]string
		expected  string
		shouldErr bool
		errMsg    string
	}{
		// Happy path: variables that are defined
		{
			name:  "single variable in JSON string value",
			input: `{"key": "${MY_VAR}"}`,
			envVars: map[string]string{
				"MY_VAR": "hello",
			},
			expected: `{"key": "hello"}`,
		},
		{
			name:  "multiple variables in different fields",
			input: `{"host": "${HOST}", "port": "${PORT}"}`,
			envVars: map[string]string{
				"HOST": "example.com",
				"PORT": "8080",
			},
			expected: `{"host": "example.com", "port": "8080"}`,
		},
		{
			name:  "variable in nested JSON object",
			input: `{"gateway": {"domain": "${DOMAIN}", "port": 3000}}`,
			envVars: map[string]string{
				"DOMAIN": "api.example.com",
			},
			expected: `{"gateway": {"domain": "api.example.com", "port": 3000}}`,
		},
		{
			name:  "variable in JSON array element",
			input: `{"args": ["run", "--rm", "${IMAGE}"]}`,
			envVars: map[string]string{
				"IMAGE": "ghcr.io/org/image:latest",
			},
			expected: `{"args": ["run", "--rm", "ghcr.io/org/image:latest"]}`,
		},
		{
			name:     "no variables - JSON passes through unchanged",
			input:    `{"method": "tools/list", "id": 1}`,
			envVars:  map[string]string{},
			expected: `{"method": "tools/list", "id": 1}`,
		},
		{
			name:  "empty variable value expands to empty string",
			input: `{"key": "${EMPTY_VAR}"}`,
			envVars: map[string]string{
				"EMPTY_VAR": "",
			},
			expected: `{"key": ""}`,
		},
		{
			name:  "variable with underscore in name",
			input: `{"token": "${GITHUB_PERSONAL_ACCESS_TOKEN}"}`,
			envVars: map[string]string{
				"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_token123",
			},
			expected: `{"token": "ghp_token123"}`,
		},
		{
			name:  "variable value containing JSON special characters",
			input: `{"path": "${CONFIG_PATH}"}`,
			envVars: map[string]string{
				"CONFIG_PATH": "/home/user/.config/gh",
			},
			expected: `{"path": "/home/user/.config/gh"}`,
		},
		{
			name:  "multiple occurrences of the same variable",
			input: `{"a": "${VAR}", "b": "${VAR}"}`,
			envVars: map[string]string{
				"VAR": "expanded",
			},
			expected: `{"a": "expanded", "b": "expanded"}`,
		},
		{
			name:  "deeply nested JSON with variables",
			input: `{"level1": {"level2": {"level3": "${DEEP_VAR}"}}}`,
			envVars: map[string]string{
				"DEEP_VAR": "deep_value",
			},
			expected: `{"level1": {"level2": {"level3": "deep_value"}}}`,
		},
		{
			name:  "variable in env map within servers config",
			input: `{"mcpServers": {"github": {"type": "stdio", "container": "image", "env": {"TOKEN": "${GITHUB_TOKEN}"}}}}`,
			envVars: map[string]string{
				"GITHUB_TOKEN": "my-token-value",
			},
			expected: `{"mcpServers": {"github": {"type": "stdio", "container": "image", "env": {"TOKEN": "my-token-value"}}}}`,
		},
		{
			name:  "variable alongside literal text",
			input: `{"url": "https://${DOMAIN}/api/v1"}`,
			envVars: map[string]string{
				"DOMAIN": "api.example.com",
			},
			expected: `{"url": "https://api.example.com/api/v1"}`,
		},
		{
			name:     "empty JSON input",
			input:    `{}`,
			envVars:  map[string]string{},
			expected: `{}`,
		},
		{
			name:  "JSON array as root",
			input: `["${ITEM1}", "${ITEM2}"]`,
			envVars: map[string]string{
				"ITEM1": "first",
				"ITEM2": "second",
			},
			expected: `["first", "second"]`,
		},

		// Error path: undefined variables
		{
			name:      "single undefined variable",
			input:     `{"key": "${UNDEFINED_VAR}"}`,
			envVars:   map[string]string{},
			shouldErr: true,
			errMsg:    "UNDEFINED_VAR",
		},
		{
			name:  "mixed defined and undefined - errors on first undefined",
			input: `{"defined": "${DEFINED_VAR}", "undefined": "${MISSING_VAR}"}`,
			envVars: map[string]string{
				"DEFINED_VAR": "value",
			},
			shouldErr: true,
			errMsg:    "MISSING_VAR",
		},
		{
			name:      "undefined variable in nested structure",
			input:     `{"gateway": {"domain": "${MISSING_DOMAIN}"}}`,
			envVars:   map[string]string{},
			shouldErr: true,
			errMsg:    "MISSING_DOMAIN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variables
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			result, err := ExpandRawJSONVariables([]byte(tt.input))

			if tt.shouldErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.ErrorContains(t, err, tt.errMsg, "Error message should mention the undefined variable")
				}
				assert.Nil(t, result, "Result should be nil on error")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, string(result), "Expanded JSON should match expected")
			}
		})
	}
}

// TestExpandRawJSONVariables_PatternBoundaries tests that the regex pattern
// correctly matches valid variable names and rejects invalid patterns.
func TestExpandRawJSONVariables_PatternBoundaries(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		envVars         map[string]string
		shouldContain   string
		shouldNotExpand bool // if true, input should pass through unchanged
	}{
		{
			name:            "pattern starting with digit is NOT expanded",
			input:           `{"key": "${1INVALID}"}`,
			envVars:         map[string]string{"1INVALID": "value"},
			shouldNotExpand: true, // regex requires [A-Za-z_] as first char
		},
		{
			name:            "pattern with hyphen is NOT expanded",
			input:           `{"key": "${INVALID-VAR}"}`,
			envVars:         map[string]string{"INVALID-VAR": "value"},
			shouldNotExpand: true, // hyphens not in [A-Za-z0-9_]
		},
		{
			name:  "variable starting with underscore is valid",
			input: `{"key": "${_PRIVATE_VAR}"}`,
			envVars: map[string]string{
				"_PRIVATE_VAR": "private_value",
			},
			shouldContain: "private_value",
		},
		{
			name:  "variable with mixed case and digits",
			input: `{"key": "${MyVar123}"}`,
			envVars: map[string]string{
				"MyVar123": "mixed_value",
			},
			shouldContain: "mixed_value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			result, err := ExpandRawJSONVariables([]byte(tt.input))

			if tt.shouldNotExpand {
				// The pattern should pass through unchanged (and no error since it's not recognized as a variable)
				require.NoError(t, err, "Non-matching patterns should not cause errors")
				assert.Equal(t, tt.input, string(result), "Non-matching patterns should pass through unchanged")
			} else {
				require.NoError(t, err)
				assert.Contains(t, string(result), tt.shouldContain)
			}
		})
	}
}

// TestExpandVariablesCore tests the unexported expandVariablesCore function directly.
// This function is the shared implementation for both expandVariables and ExpandRawJSONVariables.
func TestExpandVariablesCore(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		contextDesc       string
		envVars           map[string]string
		expectedOutput    string
		expectedUndefined []string
	}{
		{
			name:              "all variables defined - no undefined",
			input:             `{"key": "${VAR1}", "other": "${VAR2}"}`,
			contextDesc:       "test context",
			envVars:           map[string]string{"VAR1": "val1", "VAR2": "val2"},
			expectedOutput:    `{"key": "val1", "other": "val2"}`,
			expectedUndefined: nil,
		},
		{
			name:              "single undefined variable",
			input:             `{"key": "${MISSING}"}`,
			contextDesc:       "test context",
			envVars:           map[string]string{},
			expectedOutput:    `{"key": "${MISSING}"}`, // kept as-is
			expectedUndefined: []string{"MISSING"},
		},
		{
			name:              "multiple undefined variables - all tracked",
			input:             "${FIRST} and ${SECOND} and ${THIRD}",
			contextDesc:       "multi-var test",
			envVars:           map[string]string{},
			expectedOutput:    "${FIRST} and ${SECOND} and ${THIRD}", // all kept as-is
			expectedUndefined: []string{"FIRST", "SECOND", "THIRD"},
		},
		{
			name:              "mix of defined and undefined",
			input:             "${DEFINED} and ${UNDEFINED}",
			contextDesc:       "mixed test",
			envVars:           map[string]string{"DEFINED": "value"},
			expectedOutput:    "value and ${UNDEFINED}",
			expectedUndefined: []string{"UNDEFINED"},
		},
		{
			name:              "no variables in input",
			input:             `{"static": "value", "count": 42}`,
			contextDesc:       "no-vars test",
			envVars:           map[string]string{},
			expectedOutput:    `{"static": "value", "count": 42}`,
			expectedUndefined: nil,
		},
		{
			name:              "empty input",
			input:             "",
			contextDesc:       "empty test",
			envVars:           map[string]string{},
			expectedOutput:    "",
			expectedUndefined: nil,
		},
		{
			name:              "same undefined variable appears multiple times - counted once per occurrence",
			input:             "${MISSING} and ${MISSING}",
			contextDesc:       "duplicate test",
			envVars:           map[string]string{},
			expectedOutput:    "${MISSING} and ${MISSING}",
			expectedUndefined: []string{"MISSING", "MISSING"}, // appears twice in undefined list
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			result, undefinedVars, err := expandVariablesCore([]byte(tt.input), tt.contextDesc)

			require.NoError(t, err, "expandVariablesCore should never return an error itself")
			assert.Equal(t, tt.expectedOutput, string(result), "Expanded output should match")

			if tt.expectedUndefined == nil {
				assert.Empty(t, undefinedVars, "Should have no undefined variables")
			} else {
				assert.Len(t, undefinedVars, len(tt.expectedUndefined), "Undefined variable count should match")
				for _, v := range tt.expectedUndefined {
					assert.Contains(t, undefinedVars, v, "Expected undefined variable %q should be tracked", v)
				}
			}
		})
	}
}

// TestExpandRawJSONVariables_PreservesNonVariablePatterns tests that text that looks
// similar to variables but doesn't match the pattern is preserved unchanged.
func TestExpandRawJSONVariables_PreservesNonVariablePatterns(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "dollar sign without braces",
			input: `{"price": "$100"}`,
		},
		{
			name:  "braces without dollar sign",
			input: `{"key": "{not_a_var}"}`,
		},
		{
			name:  "partial pattern - dollar only",
			input: `{"key": "prefix$suffix"}`,
		},
		{
			name:  "unclosed brace",
			input: `{"key": "${UNCLOSED"}`,
		},
		{
			name:  "empty braces",
			input: `{"key": "${}"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExpandRawJSONVariables([]byte(tt.input))

			require.NoError(t, err, "Non-variable patterns should not cause errors")
			assert.Equal(t, tt.input, string(result), "Non-variable patterns should pass through unchanged")
		})
	}
}

// TestExpandRawJSONVariables_RealWorldConfig tests realistic MCP gateway config scenarios.
func TestExpandRawJSONVariables_RealWorldConfig(t *testing.T) {
	t.Run("github server with token expansion", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "ghp_mytoken123")
		t.Setenv("GITHUB_CONFIG_DIR", "/home/user/.config/gh")

		input := `{
			"mcpServers": {
				"github": {
					"type": "stdio",
					"container": "ghcr.io/github/github-mcp-server:latest",
					"env": {
						"GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}",
						"CONFIG_PATH": "${GITHUB_CONFIG_DIR}"
					}
				}
			}
		}`

		result, err := ExpandRawJSONVariables([]byte(input))

		require.NoError(t, err)
		resultStr := string(result)
		assert.Contains(t, resultStr, "ghp_mytoken123", "Token should be expanded")
		assert.Contains(t, resultStr, "/home/user/.config/gh", "Config path should be expanded")
		assert.NotContains(t, resultStr, "${GITHUB_TOKEN}", "Variable placeholder should be removed")
		assert.NotContains(t, resultStr, "${GITHUB_CONFIG_DIR}", "Variable placeholder should be removed")
	})

	t.Run("gateway config with domain expansion", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_DOMAIN", "gateway.example.com")

		input := `{
			"gateway": {
				"port": 3000,
				"domain": "${MCP_GATEWAY_DOMAIN}",
				"apiKey": "secret-key"
			}
		}`

		result, err := ExpandRawJSONVariables([]byte(input))

		require.NoError(t, err)
		assert.Contains(t, string(result), "gateway.example.com")
		assert.NotContains(t, string(result), "${MCP_GATEWAY_DOMAIN}")
	})

	t.Run("config with missing required token returns error", func(t *testing.T) {
		// Ensure the variable is NOT set
		t.Setenv("REQUIRED_TOKEN", "")

		// Remove the env var entirely (t.Setenv sets it to empty string, we need it absent)
		// We'll use a truly undefined variable name instead
		input := `{
			"mcpServers": {
				"github": {
					"type": "stdio",
					"container": "image",
					"env": {
						"TOKEN": "${TOTALLY_UNDEFINED_TOKEN_XYZ}"
					}
				}
			}
		}`

		_, err := ExpandRawJSONVariables([]byte(input))

		require.Error(t, err)
		assert.ErrorContains(t, err, "TOTALLY_UNDEFINED_TOKEN_XYZ")
	})
}

// TestValidateMounts tests the validateMounts function directly.
// This function was previously only tested indirectly through validateStandardServerConfig.
func TestValidateMounts(t *testing.T) {
	tests := []struct {
		name      string
		mounts    []string
		jsonPath  string
		shouldErr bool
		errMsg    string
	}{
		// Happy path
		{
			name:      "empty mounts slice - no validation needed",
			mounts:    []string{},
			jsonPath:  "mcpServers.test",
			shouldErr: false,
		},
		{
			name:      "nil mounts slice",
			mounts:    nil,
			jsonPath:  "mcpServers.test",
			shouldErr: false,
		},
		{
			name:      "single valid mount with ro mode",
			mounts:    []string{"/host/path:/container/path:ro"},
			jsonPath:  "mcpServers.test",
			shouldErr: false,
		},
		{
			name:      "single valid mount with rw mode",
			mounts:    []string{"/host/data:/app/data:rw"},
			jsonPath:  "mcpServers.test",
			shouldErr: false,
		},
		{
			name: "multiple valid mounts",
			mounts: []string{
				"/host/path1:/container/path1:ro",
				"/host/path2:/container/path2:rw",
			},
			jsonPath:  "mcpServers.test",
			shouldErr: false,
		},
		{
			name:      "valid mount with deep paths",
			mounts:    []string{"/var/run/docker.sock:/var/run/docker.sock:rw"},
			jsonPath:  "mcpServers.test",
			shouldErr: false,
		},

		// Error cases
		{
			name:      "mount without mode (only 2 parts)",
			mounts:    []string{"/host:/container"},
			jsonPath:  "mcpServers.test",
			shouldErr: true,
			errMsg:    "invalid mount format",
		},
		{
			name:      "mount with too many parts",
			mounts:    []string{"/host:/container:ro:extra"},
			jsonPath:  "mcpServers.test",
			shouldErr: true,
			errMsg:    "invalid mount format",
		},
		{
			name:      "mount with invalid mode",
			mounts:    []string{"/host:/container:rw,ro"},
			jsonPath:  "mcpServers.test",
			shouldErr: true,
			errMsg:    "invalid mount",
		},
		{
			name:      "mount with empty source",
			mounts:    []string{":/container:ro"},
			jsonPath:  "mcpServers.test",
			shouldErr: true,
			errMsg:    "empty",
		},
		{
			name:      "mount with relative source path",
			mounts:    []string{"relative/path:/container:ro"},
			jsonPath:  "mcpServers.test",
			shouldErr: true,
			errMsg:    "absolute path",
		},
		{
			name:      "mount with relative destination path",
			mounts:    []string{"/host/path:relative/container:ro"},
			jsonPath:  "mcpServers.test",
			shouldErr: true,
			errMsg:    "absolute path",
		},
		{
			name:      "mount with invalid mode value",
			mounts:    []string{"/host:/container:invalid_mode"},
			jsonPath:  "mcpServers.test",
			shouldErr: true,
			errMsg:    "invalid mount mode",
		},

		// Error on first invalid when multiple mounts
		{
			name: "first mount valid but second invalid - errors on second",
			mounts: []string{
				"/host/path1:/container/path1:ro",
				"relative/path:/container:ro", // invalid
			},
			jsonPath:  "mcpServers.test",
			shouldErr: true,
			errMsg:    "absolute path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMounts(tt.mounts, tt.jsonPath)

			if tt.shouldErr {
				require.Error(t, err, "Expected an error but got none")
				if tt.errMsg != "" {
					assert.ErrorContains(t, err, tt.errMsg,
						"Error message %q should contain %q", err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err, "Expected no error but got: %v", err)
			}
		})
	}
}

// TestValidateMounts_JSONPathInError tests that the jsonPath parameter
// is correctly included in the error output.
func TestValidateMounts_JSONPathInError(t *testing.T) {
	jsonPath := "mcpServers.my-server"
	mounts := []string{"invalid-mount-format"} // no colons at all

	err := validateMounts(mounts, jsonPath)

	require.Error(t, err)
	// The error should reference the JSON path
	assert.ErrorContains(t, err, "my-server",
		"Error should reference the server path to help users locate the issue")
}

// TestValidateMounts_IndexInError tests that mount index is correctly reported in errors.
func TestValidateMounts_IndexInError(t *testing.T) {
	// Second mount (index 1) is invalid
	mounts := []string{
		"/host/valid:/container/valid:ro",
		"invalid", // index 1
	}

	err := validateMounts(mounts, "mcpServers.test")

	require.Error(t, err)
	// Error should indicate which mount index failed
	assert.ErrorContains(t, err, "[1]",
		"Error should indicate the mount index that failed")
}
