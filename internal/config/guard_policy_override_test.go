package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetGatewaySessionTimeoutFromEnv(t *testing.T) {
	t.Run("reads duration from env", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_SESSION_TIMEOUT", "2h")
		assert.Equal(t, 2*time.Hour, GetGatewaySessionTimeoutFromEnv())
	})

	t.Run("defaults to 6h", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_SESSION_TIMEOUT", "")
		assert.Equal(t, 6*time.Hour, GetGatewaySessionTimeoutFromEnv())
	})
}

func TestResolveGuardPolicyOverride(t *testing.T) {
	t.Run("no override", func(t *testing.T) {
		policy, source, err := ResolveGuardPolicyOverride(false, "", false, "", "", "")
		require.NoError(t, err)
		assert.Nil(t, policy)
		assert.Empty(t, source)
	})

	t.Run("cli policy json has highest priority", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_GUARD_POLICY_JSON", `{"allow-only":{"repos":"public","min-integrity":"none"}}`)

		policy, source, err := ResolveGuardPolicyOverride(
			true,
			`{"allow-only":{"repos":["myorg/*"],"min-integrity":"approved"}}`,
			false,
			"",
			"",
			"",
		)

		require.NoError(t, err)
		require.NotNil(t, policy)
		assert.Equal(t, "cli", source)
		assert.Equal(t, "approved", policy.AllowOnly.MinIntegrity)
	})

	t.Run("env policy json has priority over env allowonly vars", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_GUARD_POLICY_JSON", `{"allow-only":{"repos":"public","min-integrity":"none"}}`)
		t.Setenv("MCP_GATEWAY_ALLOWONLY_SCOPE_OWNER", "myorg")
		t.Setenv("MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY", "approved")

		policy, source, err := ResolveGuardPolicyOverride(false, "", false, "", "", "")
		require.NoError(t, err)
		require.NotNil(t, policy)
		assert.Equal(t, "env", source)
		assert.Equal(t, "none", policy.AllowOnly.MinIntegrity)
		assert.Equal(t, "public", policy.AllowOnly.Repos)
	})

	t.Run("env allowonly vars are used when guard policy json env is unset", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_ALLOWONLY_SCOPE_PUBLIC", "true")
		t.Setenv("MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY", "merged")

		policy, source, err := ResolveGuardPolicyOverride(false, "", false, "", "", "")
		require.NoError(t, err)
		require.NotNil(t, policy)
		assert.Equal(t, "env", source)
		assert.Equal(t, "public", policy.AllowOnly.Repos)
		assert.Equal(t, "merged", policy.AllowOnly.MinIntegrity)
	})

	t.Run("cli changed with allowonly flags builds policy from cli args", func(t *testing.T) {
		policy, source, err := ResolveGuardPolicyOverride(
			true,
			"",
			true,
			"",
			"",
			"none",
		)
		require.NoError(t, err)
		require.NotNil(t, policy)
		assert.Equal(t, "cli", source)
		assert.Equal(t, "public", policy.AllowOnly.Repos)
		assert.Equal(t, "none", policy.AllowOnly.MinIntegrity)
	})

	t.Run("cli changed with owner and repo builds scoped policy", func(t *testing.T) {
		policy, source, err := ResolveGuardPolicyOverride(
			true,
			"",
			false,
			"myorg",
			"myrepo",
			"approved",
		)
		require.NoError(t, err)
		require.NotNil(t, policy)
		assert.Equal(t, "cli", source)
		require.NotNil(t, policy.AllowOnly)
		assert.Equal(t, "myorg/myrepo", policy.AllowOnly.Repos)
		assert.Equal(t, "approved", policy.AllowOnly.MinIntegrity)
	})

	t.Run("cli changed with invalid json returns error", func(t *testing.T) {
		policy, source, err := ResolveGuardPolicyOverride(
			true,
			`{invalid json}`,
			false,
			"",
			"",
			"",
		)
		require.Error(t, err)
		assert.Nil(t, policy)
		assert.Empty(t, source)
	})

	t.Run("cli changed with invalid allowonly args returns error", func(t *testing.T) {
		// repo without owner is invalid
		policy, source, err := ResolveGuardPolicyOverride(
			true,
			"",
			false,
			"",
			"myrepo",
			"approved",
		)
		require.Error(t, err)
		assert.Nil(t, policy)
		assert.Empty(t, source)
	})

	t.Run("env policy json invalid returns error", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_GUARD_POLICY_JSON", `{not valid json}`)

		policy, source, err := ResolveGuardPolicyOverride(false, "", false, "", "", "")
		require.Error(t, err)
		assert.Nil(t, policy)
		assert.Empty(t, source)
	})

	t.Run("env allowonly vars with invalid config returns error", func(t *testing.T) {
		// repo without owner is invalid via env vars
		t.Setenv("MCP_GATEWAY_ALLOWONLY_SCOPE_REPO", "myrepo")

		policy, source, err := ResolveGuardPolicyOverride(false, "", false, "", "", "")
		require.Error(t, err)
		assert.Nil(t, policy)
		assert.Empty(t, source)
	})
}
