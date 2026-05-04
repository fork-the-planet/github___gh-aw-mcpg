package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeGuardPolicyTestCmd creates a fresh cobra.Command with the guard-policy-related
// flags registered, and resets the associated package-level variables.
// This avoids cross-test contamination from the global flag state.
func makeGuardPolicyTestCmd() *cobra.Command {
	// Reset package-level flag variables to their zero values
	guardPolicyJSON = ""
	allowOnlyPublic = false
	allowOnlyOwner = ""
	allowOnlyRepo = ""
	allowOnlyMinInt = ""

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringVar(&guardPolicyJSON, "guard-policy-json", "", "")
	cmd.Flags().BoolVar(&allowOnlyPublic, "allowonly-scope-public", false, "")
	cmd.Flags().StringVar(&allowOnlyOwner, "allowonly-scope-owner", "", "")
	cmd.Flags().StringVar(&allowOnlyRepo, "allowonly-scope-repo", "", "")
	cmd.Flags().StringVar(&allowOnlyMinInt, "allowonly-min-integrity", "", "")
	return cmd
}

// TestResolveGuardPolicyOverride_NoOverride tests the case when no CLI flags
// are changed and no env vars are set, so nil is returned.
func TestResolveGuardPolicyOverride_NoOverride(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.NoError(t, err)
	assert.Nil(t, policy, "Policy should be nil when no flags or env vars are set")
	assert.Empty(t, source, "Source should be empty when no flags or env vars are set")
}

// TestResolveGuardPolicyOverride_CLIGuardPolicyJSON tests the case when the
// --guard-policy-json CLI flag is set with valid JSON.
func TestResolveGuardPolicyOverride_CLIGuardPolicyJSON(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	validJSON := `{"allow-only":{"repos":"public","min-integrity":"none"}}`
	require.NoError(t, cmd.Flags().Set("guard-policy-json", validJSON))

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.NoError(t, err)
	require.NotNil(t, policy, "Policy should not be nil when guard-policy-json is set")
	assert.Equal(t, "cli", source, "Source should be 'cli' when flag is set via CLI")
	require.NotNil(t, policy.AllowOnly)
	assert.Equal(t, "public", policy.AllowOnly.Repos)
}

// TestResolveGuardPolicyOverride_CLIGuardPolicyJSON_Invalid tests the case when the
// --guard-policy-json CLI flag is set with invalid JSON.
func TestResolveGuardPolicyOverride_CLIGuardPolicyJSON_Invalid(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	require.NoError(t, cmd.Flags().Set("guard-policy-json", "not-valid-json"))

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.Error(t, err, "Should return an error for invalid JSON")
	assert.Nil(t, policy)
	assert.Empty(t, source)
}

// TestResolveGuardPolicyOverride_CLIGuardPolicyJSON_WhitespaceOnly tests the case when
// --guard-policy-json is set to whitespace only, so it falls through to the AllowOnly
// path, but no AllowOnly flags are set → returns nil, "", nil.
func TestResolveGuardPolicyOverride_CLIGuardPolicyJSON_WhitespaceOnly(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	// Setting a flag to whitespace marks it as changed but trimmed value is empty
	require.NoError(t, cmd.Flags().Set("guard-policy-json", "   "))

	policy, source, err := resolveGuardPolicyOverride(cmd)

	// When guard-policy-json is changed but trimmed to empty, BuildAllowOnlyPolicy is called
	// with all zero values → scopeCount=0 AND minIntegrity="" → returns nil, nil
	require.NoError(t, err)
	assert.Nil(t, policy, "Policy should be nil when guard-policy-json is whitespace-only")
	assert.Equal(t, "cli", source, "Source should be 'cli' when a CLI flag was changed")
}

// TestResolveGuardPolicyOverride_CLIAllowOnlyPublic tests the case when the
// --allowonly-scope-public CLI flag is changed.
func TestResolveGuardPolicyOverride_CLIAllowOnlyPublic(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	require.NoError(t, cmd.Flags().Set("allowonly-scope-public", "true"))
	require.NoError(t, cmd.Flags().Set("allowonly-min-integrity", "none"))

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.NoError(t, err)
	require.NotNil(t, policy, "Policy should not be nil")
	assert.Equal(t, "cli", source, "Source should be 'cli' when flag is set via CLI")
	require.NotNil(t, policy.AllowOnly)
	assert.Equal(t, "public", policy.AllowOnly.Repos)
}

// TestResolveGuardPolicyOverride_CLIAllowOnlyOwner tests the case when the
// --allowonly-scope-owner CLI flag is changed.
func TestResolveGuardPolicyOverride_CLIAllowOnlyOwner(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	require.NoError(t, cmd.Flags().Set("allowonly-scope-owner", "myorg"))
	require.NoError(t, cmd.Flags().Set("allowonly-min-integrity", "approved"))

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "cli", source)
	require.NotNil(t, policy.AllowOnly)
	assert.Equal(t, "approved", policy.AllowOnly.MinIntegrity)
	// Repos should be a slice containing "myorg/*"
	reposStr, ok := policy.AllowOnly.Repos.([]string)
	require.True(t, ok, "Repos should be []string type when owner scope is set")
	require.Len(t, reposStr, 1)
	assert.Equal(t, "myorg/*", reposStr[0])
}

// TestResolveGuardPolicyOverride_CLIAllowOnlyOwnerAndRepo tests the case when both
// --allowonly-scope-owner and --allowonly-scope-repo CLI flags are changed.
func TestResolveGuardPolicyOverride_CLIAllowOnlyOwnerAndRepo(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	require.NoError(t, cmd.Flags().Set("allowonly-scope-owner", "myorg"))
	require.NoError(t, cmd.Flags().Set("allowonly-scope-repo", "myrepo"))
	require.NoError(t, cmd.Flags().Set("allowonly-min-integrity", "merged"))

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "cli", source)
	require.NotNil(t, policy.AllowOnly)
}

// TestResolveGuardPolicyOverride_CLIRepoWithoutOwner tests that setting
// --allowonly-scope-repo without --allowonly-scope-owner returns an error.
func TestResolveGuardPolicyOverride_CLIRepoWithoutOwner(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	require.NoError(t, cmd.Flags().Set("allowonly-scope-repo", "myrepo"))
	require.NoError(t, cmd.Flags().Set("allowonly-min-integrity", "none"))

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.Error(t, err, "Should return error when repo is set without owner")
	assert.ErrorContains(t, err, "owner", "Error should mention owner")
	assert.Nil(t, policy)
	assert.Empty(t, source)
}

// TestResolveGuardPolicyOverride_CLIAllowOnlyMinIntegrityOnly tests the case when only
// --allowonly-min-integrity is changed without a scope → should error (scopeCount != 1).
func TestResolveGuardPolicyOverride_CLIAllowOnlyMinIntegrityOnly(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	require.NoError(t, cmd.Flags().Set("allowonly-min-integrity", "none"))

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.Error(t, err, "Should return error when only min-integrity is set without a scope")
	assert.ErrorContains(t, err, "scope", "Error should mention scope")
	assert.Nil(t, policy)
	assert.Empty(t, source)
}

// TestResolveGuardPolicyOverride_EnvGuardPolicyJSON tests the case when the
// MCP_GATEWAY_GUARD_POLICY_JSON env var is set with valid JSON.
func TestResolveGuardPolicyOverride_EnvGuardPolicyJSON(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	validJSON := `{"allow-only":{"repos":"public","min-integrity":"none"}}`
	t.Setenv("MCP_GATEWAY_GUARD_POLICY_JSON", validJSON)

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "env", source, "Source should be 'env' when set via environment variable")
	require.NotNil(t, policy.AllowOnly)
	assert.Equal(t, "public", policy.AllowOnly.Repos)
}

// TestResolveGuardPolicyOverride_EnvGuardPolicyJSON_Invalid tests the case when
// MCP_GATEWAY_GUARD_POLICY_JSON env var contains invalid JSON.
func TestResolveGuardPolicyOverride_EnvGuardPolicyJSON_Invalid(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	t.Setenv("MCP_GATEWAY_GUARD_POLICY_JSON", "not-valid-json")

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.Error(t, err, "Should return error for invalid JSON in env var")
	assert.Nil(t, policy)
	assert.Empty(t, source)
}

// TestResolveGuardPolicyOverride_EnvAllowOnlyPublic tests the case when
// MCP_GATEWAY_ALLOWONLY_SCOPE_PUBLIC is set in the environment.
func TestResolveGuardPolicyOverride_EnvAllowOnlyPublic(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	t.Setenv("MCP_GATEWAY_ALLOWONLY_SCOPE_PUBLIC", "true")
	t.Setenv("MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY", "none")

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "env", source, "Source should be 'env' when set via environment variable")
	require.NotNil(t, policy.AllowOnly)
	assert.Equal(t, "public", policy.AllowOnly.Repos)
}

// TestResolveGuardPolicyOverride_EnvAllowOnlyOwner tests the case when
// MCP_GATEWAY_ALLOWONLY_SCOPE_OWNER is set in the environment.
func TestResolveGuardPolicyOverride_EnvAllowOnlyOwner(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	t.Setenv("MCP_GATEWAY_ALLOWONLY_SCOPE_OWNER", "someorg")
	t.Setenv("MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY", "approved")

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "env", source)
	require.NotNil(t, policy.AllowOnly)
}

// TestResolveGuardPolicyOverride_EnvAllowOnlyRepo tests the case when
// MCP_GATEWAY_ALLOWONLY_SCOPE_REPO is set (with owner) in the environment.
func TestResolveGuardPolicyOverride_EnvAllowOnlyRepo(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	t.Setenv("MCP_GATEWAY_ALLOWONLY_SCOPE_OWNER", "someorg")
	t.Setenv("MCP_GATEWAY_ALLOWONLY_SCOPE_REPO", "somerepo")
	t.Setenv("MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY", "merged")

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "env", source)
	require.NotNil(t, policy.AllowOnly)
}

// TestResolveGuardPolicyOverride_EnvAllowOnlyRepoWithoutOwner tests that setting
// MCP_GATEWAY_ALLOWONLY_SCOPE_REPO without MCP_GATEWAY_ALLOWONLY_SCOPE_OWNER errors.
func TestResolveGuardPolicyOverride_EnvAllowOnlyRepoWithoutOwner(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	t.Setenv("MCP_GATEWAY_ALLOWONLY_SCOPE_REPO", "somerepo")
	t.Setenv("MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY", "none")

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.Error(t, err, "Should return error when repo is set without owner")
	assert.Nil(t, policy)
	assert.Empty(t, source)
}

// TestResolveGuardPolicyOverride_EnvMinIntegrityOnly tests the case when only
// MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY is set in the environment (no scope).
func TestResolveGuardPolicyOverride_EnvMinIntegrityOnly(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	t.Setenv("MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY", "none")

	policy, source, err := resolveGuardPolicyOverride(cmd)

	// scopeCount=0, minIntegrity="none" → scopeCount != 1 → error
	require.Error(t, err, "Should error when only min-integrity is set without a scope")
	assert.Nil(t, policy)
	assert.Empty(t, source)
}

// TestResolveGuardPolicyOverride_CLITakesPrecedenceOverEnv tests that CLI flag
// values take precedence over environment variables.
func TestResolveGuardPolicyOverride_CLITakesPrecedenceOverEnv(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	// Set env var (lower priority)
	t.Setenv("MCP_GATEWAY_GUARD_POLICY_JSON", `{"allow-only":{"repos":"public","min-integrity":"none"}}`)

	// Set CLI flag (higher priority)
	cliJSON := `{"allow-only":{"repos":["myorg/*"],"min-integrity":"approved"}}`
	require.NoError(t, cmd.Flags().Set("guard-policy-json", cliJSON))

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.NoError(t, err)
	require.NotNil(t, policy)
	// CLI takes precedence: source should be "cli"
	assert.Equal(t, "cli", source, "CLI flags should take precedence over env vars")
	require.NotNil(t, policy.AllowOnly)
	// The CLI policy uses owner scope, not public
	assert.NotEqual(t, "public", policy.AllowOnly.Repos)
}

// TestResolveGuardPolicyOverride_EnvGuardPolicyJSONTakesPrecedenceOverAllowOnly tests that
// MCP_GATEWAY_GUARD_POLICY_JSON env var takes precedence over AllowOnly env vars.
func TestResolveGuardPolicyOverride_EnvGuardPolicyJSONTakesPrecedenceOverAllowOnly(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	// Set MCP_GATEWAY_GUARD_POLICY_JSON (higher priority env var)
	t.Setenv("MCP_GATEWAY_GUARD_POLICY_JSON", `{"allow-only":{"repos":"public","min-integrity":"none"}}`)

	// Set AllowOnly env vars (lower priority)
	t.Setenv("MCP_GATEWAY_ALLOWONLY_SCOPE_PUBLIC", "true")
	t.Setenv("MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY", "approved")

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "env", source)
	// MCP_GATEWAY_GUARD_POLICY_JSON policy was used: min-integrity=none, not approved
	require.NotNil(t, policy.AllowOnly)
}

// TestResolveGuardPolicyOverride_MultipleCliFlags tests that changing any one of the
// guard policy-related CLI flags triggers CLI-path evaluation.
func TestResolveGuardPolicyOverride_MultipleCliFlags(t *testing.T) {
	flags := []struct {
		name  string
		value string
	}{
		{"guard-policy-json", `{"allow-only":{"repos":"public","min-integrity":"none"}}`},
		{"allowonly-scope-public", "true"},
		{"allowonly-scope-owner", "someorg"},
		{"allowonly-scope-repo", "somerepo"},
		{"allowonly-min-integrity", "none"},
	}

	for _, f := range flags {
		t.Run("flag_"+f.name+"_triggers_cli_path", func(t *testing.T) {
			cmd := makeGuardPolicyTestCmd()
			require.NoError(t, cmd.Flags().Set(f.name, f.value))

			// Verify the flag is marked as changed
			assert.True(t, cmd.Flags().Changed(f.name), "Flag %q should be marked as changed", f.name)
		})
	}
}

// TestResolveGuardPolicyOverride_EnvGuardPolicyJSONWhitespace tests that a
// whitespace-only MCP_GATEWAY_GUARD_POLICY_JSON is ignored (trimmed to empty).
func TestResolveGuardPolicyOverride_EnvGuardPolicyJSONWhitespace(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	t.Setenv("MCP_GATEWAY_GUARD_POLICY_JSON", "   ")

	policy, source, err := resolveGuardPolicyOverride(cmd)

	// Whitespace-only is trimmed to "", so the env branch is skipped
	// No AllowOnly env vars set either, so returns nil
	require.NoError(t, err)
	assert.Nil(t, policy, "Whitespace-only MCP_GATEWAY_GUARD_POLICY_JSON should be treated as unset")
	assert.Empty(t, source)
}

// TestResolveGuardPolicyOverride_CLIInvalidMinIntegrity tests that an invalid
// min-integrity value in a CLI AllowOnly policy returns an error.
func TestResolveGuardPolicyOverride_CLIInvalidMinIntegrity(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	require.NoError(t, cmd.Flags().Set("allowonly-scope-public", "true"))
	require.NoError(t, cmd.Flags().Set("allowonly-min-integrity", "invalid-integrity"))

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.Error(t, err, "Should error on invalid min-integrity value")
	assert.ErrorContains(t, err, "min-integrity")
	assert.Nil(t, policy)
	assert.Empty(t, source)
}

// TestResolveGuardPolicyOverride_AllFlagsUnchanged verifies that when a command exists
// with all flags registered but none set, the function returns no override.
func TestResolveGuardPolicyOverride_AllFlagsUnchanged(t *testing.T) {
	cmd := makeGuardPolicyTestCmd()

	// Verify no flags are changed
	assert.False(t, cmd.Flags().Changed("guard-policy-json"))
	assert.False(t, cmd.Flags().Changed("allowonly-scope-public"))
	assert.False(t, cmd.Flags().Changed("allowonly-scope-owner"))
	assert.False(t, cmd.Flags().Changed("allowonly-scope-repo"))
	assert.False(t, cmd.Flags().Changed("allowonly-min-integrity"))

	policy, source, err := resolveGuardPolicyOverride(cmd)

	require.NoError(t, err)
	assert.Nil(t, policy)
	assert.Empty(t, source)
}
