package cmd

import (
	"os"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDIFCMode(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		wantErr bool
	}{
		{
			name:    "strict mode valid",
			mode:    "strict",
			wantErr: false,
		},
		{
			name:    "filter mode valid",
			mode:    "filter",
			wantErr: false,
		},
		{
			name:    "propagate mode valid",
			mode:    "propagate",
			wantErr: false,
		},
		{
			name:    "uppercase STRICT valid",
			mode:    "STRICT",
			wantErr: false,
		},
		{
			name:    "mixed case Filter valid",
			mode:    "Filter",
			wantErr: false,
		},
		{
			name:    "invalid mode",
			mode:    "invalid",
			wantErr: true,
		},
		{
			name:    "empty mode defaults to strict",
			mode:    "",
			wantErr: false,
		},
		{
			name:    "partial match should fail",
			mode:    "stric",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDIFCMode(tt.mode)
			if tt.wantErr {
				assert.Error(t, err, "expected error for mode %q", tt.mode)
			} else {
				assert.NoError(t, err, "unexpected error for mode %q", tt.mode)
			}
		})
	}
}

func TestGetDefaultDIFCMode(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     string
	}{
		{
			name:     "no env var returns strict",
			envValue: "",
			want:     "strict",
		},
		{
			name:     "env var strict",
			envValue: "strict",
			want:     "strict",
		},
		{
			name:     "env var filter",
			envValue: "filter",
			want:     "filter",
		},
		{
			name:     "env var propagate",
			envValue: "propagate",
			want:     "propagate",
		},
		{
			name:     "env var FILTER uppercase",
			envValue: "FILTER",
			want:     "filter",
		},
		{
			name:     "env var invalid falls back to strict",
			envValue: "invalid",
			want:     "strict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore original env
			originalEnv := os.Getenv("MCP_GATEWAY_GUARDS_MODE")
			defer func() {
				if originalEnv != "" {
					os.Setenv("MCP_GATEWAY_GUARDS_MODE", originalEnv)
				} else {
					os.Unsetenv("MCP_GATEWAY_GUARDS_MODE")
				}
			}()

			if tt.envValue != "" {
				os.Setenv("MCP_GATEWAY_GUARDS_MODE", tt.envValue)
			} else {
				os.Unsetenv("MCP_GATEWAY_GUARDS_MODE")
			}

			got := getDefaultDIFCMode()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidDIFCModes(t *testing.T) {
	require := require.New(t)

	// Verify all expected modes are valid using ValidateDIFCMode
	require.NoError(ValidateDIFCMode(difc.ModeStrict), "strict should be valid")
	require.NoError(ValidateDIFCMode(difc.ModeFilter), "filter should be valid")
	require.NoError(ValidateDIFCMode(difc.ModePropagate), "propagate should be valid")

	// Verify ValidModes slice has 3 entries
	require.Len(difc.ValidModes, 3, "should only have 3 valid modes")
}

func TestGetDefaultDIFCSinkServerIDs(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		expected string
	}{
		{
			name:     "no env var - returns empty string",
			setEnv:   false,
			expected: "",
		},
		{
			name:     "env var set - returns value",
			envValue: "safeoutputs,github",
			setEnv:   true,
			expected: "safeoutputs,github",
		},
		{
			name:     "empty env var - returns empty string",
			envValue: "",
			setEnv:   true,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("MCP_GATEWAY_GUARDS_SINK_SERVER_IDS", tt.envValue)
			} else {
				t.Setenv("MCP_GATEWAY_GUARDS_SINK_SERVER_IDS", "")
			}

			result := getDefaultDIFCSinkServerIDs()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseDIFCSinkServerIDs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		expect  []string
		wantErr bool
	}{
		{
			name:   "empty input",
			input:  "",
			expect: nil,
		},
		{
			name:   "single server id",
			input:  "safeoutputs",
			expect: []string{"safeoutputs"},
		},
		{
			name:   "multiple server ids",
			input:  "safeoutputs,github",
			expect: []string{"safeoutputs", "github"},
		},
		{
			name:   "trims whitespace around separators",
			input:  " safeoutputs , github ",
			expect: []string{"safeoutputs", "github"},
		},
		{
			name:   "deduplicates server ids",
			input:  "safeoutputs,github,safeoutputs",
			expect: []string{"safeoutputs", "github"},
		},
		{
			name:    "rejects embedded whitespace",
			input:   "safe outputs",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDIFCSinkServerIDs(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestBuildAllowOnlyPolicy(t *testing.T) {
	t.Run("public scope valid", func(t *testing.T) {
		policy, err := config.BuildAllowOnlyPolicy(true, "", "", "none")
		require.NoError(t, err)
		require.NotNil(t, policy)
		require.NotNil(t, policy.AllowOnly)
		assert.Equal(t, config.IntegrityNone, policy.AllowOnly.MinIntegrity)
		assert.Equal(t, "public", policy.AllowOnly.Repos)
	})

	t.Run("owner and repo scope valid", func(t *testing.T) {
		policy, err := config.BuildAllowOnlyPolicy(false, "lpcox", "gh-aw-mcpg", "unapproved")
		require.NoError(t, err)
		require.NotNil(t, policy)
		repos, ok := policy.AllowOnly.Repos.([]string)
		require.True(t, ok)
		assert.Equal(t, []string{"lpcox/gh-aw-mcpg"}, repos)
		assert.Equal(t, config.IntegrityUnapproved, policy.AllowOnly.MinIntegrity)
	})

	t.Run("repo without owner invalid", func(t *testing.T) {
		_, err := config.BuildAllowOnlyPolicy(false, "", "repo", "unapproved")
		require.Error(t, err)
	})

	t.Run("missing min integrity invalid", func(t *testing.T) {
		_, err := config.BuildAllowOnlyPolicy(true, "", "", "")
		require.Error(t, err)
	})
}

func TestGetDefaultGuardPolicyInputs(t *testing.T) {
	t.Setenv("MCP_GATEWAY_GUARD_POLICY_JSON", `{"allow-only":{"repos":"public","min-integrity":"none"}}`)
	t.Setenv("MCP_GATEWAY_ALLOWONLY_SCOPE_PUBLIC", "1")
	t.Setenv("MCP_GATEWAY_ALLOWONLY_SCOPE_OWNER", "lpcox")
	t.Setenv("MCP_GATEWAY_ALLOWONLY_SCOPE_REPO", "gh-aw-mcpg")
	t.Setenv("MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY", "unapproved")

	assert.NotEmpty(t, getDefaultGuardPolicyJSON())
	assert.True(t, getDefaultAllowOnlyScopePublic())
	assert.Equal(t, "lpcox", getDefaultAllowOnlyOwner())
	assert.Equal(t, "gh-aw-mcpg", getDefaultAllowOnlyRepo())
	assert.Equal(t, "unapproved", getDefaultAllowOnlyMinIntegrity())
}
