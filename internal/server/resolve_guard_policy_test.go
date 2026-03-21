package server

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validWriteSinkPolicy returns a valid WriteSink guard policy for use in tests.
func validWriteSinkPolicy() *config.GuardPolicy {
	return &config.GuardPolicy{
		WriteSink: &config.WriteSinkPolicy{
			Accept: []string{"private:myorg"},
		},
	}
}

// ---- normalizeScopeKind tests ----

func TestNormalizeScopeKind_NilInput(t *testing.T) {
	result := normalizeScopeKind(nil)
	assert.Nil(t, result, "nil input should return nil")
}

func TestNormalizeScopeKind_EmptyMap(t *testing.T) {
	result := normalizeScopeKind(map[string]interface{}{})
	require.NotNil(t, result)
	assert.Empty(t, result, "empty map should return empty copy")
}

func TestNormalizeScopeKind_NoScopeKindField(t *testing.T) {
	input := map[string]interface{}{
		"other_field": "value",
		"count":       42,
	}
	result := normalizeScopeKind(input)
	require.NotNil(t, result)
	assert.Equal(t, "value", result["other_field"])
	assert.Equal(t, 42, result["count"])
	_, hasScopeKind := result["scope_kind"]
	assert.False(t, hasScopeKind, "scope_kind should not be present when not in input")
}

func TestNormalizeScopeKind_ScopeKindAlreadyLowercase(t *testing.T) {
	input := map[string]interface{}{
		"scope_kind": "scoped",
	}
	result := normalizeScopeKind(input)
	require.NotNil(t, result)
	assert.Equal(t, "scoped", result["scope_kind"])
}

func TestNormalizeScopeKind_ScopeKindUppercase(t *testing.T) {
	input := map[string]interface{}{
		"scope_kind": "SCOPED",
	}
	result := normalizeScopeKind(input)
	require.NotNil(t, result)
	assert.Equal(t, "scoped", result["scope_kind"])
}

func TestNormalizeScopeKind_ScopeKindWithLeadingTrailingSpaces(t *testing.T) {
	input := map[string]interface{}{
		"scope_kind": "  Public  ",
	}
	result := normalizeScopeKind(input)
	require.NotNil(t, result)
	assert.Equal(t, "public", result["scope_kind"])
}

func TestNormalizeScopeKind_ScopeKindUppercaseWithSpaces(t *testing.T) {
	input := map[string]interface{}{
		"scope_kind": "  OWNER_SCOPED  ",
	}
	result := normalizeScopeKind(input)
	require.NotNil(t, result)
	assert.Equal(t, "owner_scoped", result["scope_kind"])
}

func TestNormalizeScopeKind_ScopeKindNonString(t *testing.T) {
	// When scope_kind is not a string, it should be preserved as-is (no normalization)
	input := map[string]interface{}{
		"scope_kind": 123,
	}
	result := normalizeScopeKind(input)
	require.NotNil(t, result)
	assert.Equal(t, 123, result["scope_kind"], "non-string scope_kind should be preserved unchanged")
}

func TestNormalizeScopeKind_PreservesOtherFieldsWithScopeKind(t *testing.T) {
	input := map[string]interface{}{
		"scope_kind":    "REPO_SCOPED",
		"scope_values":  []string{"github/repo"},
		"min-integrity": "approved",
	}
	result := normalizeScopeKind(input)
	require.NotNil(t, result)
	assert.Equal(t, "repo_scoped", result["scope_kind"])
	assert.Equal(t, []string{"github/repo"}, result["scope_values"])
	assert.Equal(t, "approved", result["min-integrity"])
}

func TestNormalizeScopeKind_DoesNotMutateInput(t *testing.T) {
	// Verify normalizeScopeKind returns a new map and doesn't mutate the input
	input := map[string]interface{}{
		"scope_kind": "UPPER",
	}
	result := normalizeScopeKind(input)
	assert.Equal(t, "UPPER", input["scope_kind"], "input should not be mutated")
	assert.Equal(t, "upper", result["scope_kind"])
}

// ---- resolveGuardPolicy tests ----

func TestResolveGuardPolicy_NilConfig(t *testing.T) {
	us := &UnifiedServer{cfg: nil}

	policy, source, err := us.resolveGuardPolicy("github")

	require.NoError(t, err)
	assert.Nil(t, policy, "nil config should return nil policy")
	assert.Equal(t, "legacy", source)
}

func TestResolveGuardPolicy_GlobalPolicyOverride_ValidAllowOnly(t *testing.T) {
	policy := validAllowOnlyPolicy()
	cfg := &config.Config{
		GuardPolicy:       policy,
		GuardPolicySource: "",
	}
	us := &UnifiedServer{cfg: cfg}

	result, source, err := us.resolveGuardPolicy("any-server")

	require.NoError(t, err)
	assert.Equal(t, policy, result)
	assert.Equal(t, "override", source, "empty GuardPolicySource should default to 'override'")
}

func TestResolveGuardPolicy_GlobalPolicyOverride_CustomSource(t *testing.T) {
	policy := validAllowOnlyPolicy()
	cfg := &config.Config{
		GuardPolicy:       policy,
		GuardPolicySource: "cli",
	}
	us := &UnifiedServer{cfg: cfg}

	result, source, err := us.resolveGuardPolicy("any-server")

	require.NoError(t, err)
	assert.Equal(t, policy, result)
	assert.Equal(t, "cli", source)
}

func TestResolveGuardPolicy_GlobalPolicyOverride_EnvSource(t *testing.T) {
	policy := validWriteSinkPolicy()
	cfg := &config.Config{
		GuardPolicy:       policy,
		GuardPolicySource: "env",
	}
	us := &UnifiedServer{cfg: cfg}

	result, source, err := us.resolveGuardPolicy("any-server")

	require.NoError(t, err)
	assert.Equal(t, policy, result)
	assert.Equal(t, "env", source)
}

func TestResolveGuardPolicy_GlobalPolicyOverride_InvalidPolicy(t *testing.T) {
	// A GuardPolicy with neither AllowOnly nor WriteSink is invalid
	invalidPolicy := &config.GuardPolicy{}
	cfg := &config.Config{
		GuardPolicy: invalidPolicy,
	}
	us := &UnifiedServer{cfg: cfg}

	result, source, err := us.resolveGuardPolicy("any-server")

	require.Error(t, err, "invalid policy should return error")
	assert.Nil(t, result)
	assert.Empty(t, source)
}

func TestResolveGuardPolicy_ServerNotInConfig(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"other-server": {Type: "http"},
		},
	}
	us := &UnifiedServer{cfg: cfg}

	policy, source, err := us.resolveGuardPolicy("nonexistent-server")

	require.NoError(t, err)
	assert.Nil(t, policy)
	assert.Equal(t, "legacy", source)
}

func TestResolveGuardPolicy_NilServerConfig(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": nil,
		},
	}
	us := &UnifiedServer{cfg: cfg}

	policy, source, err := us.resolveGuardPolicy("github")

	require.NoError(t, err)
	assert.Nil(t, policy)
	assert.Equal(t, "legacy", source)
}

func TestResolveGuardPolicy_ServerWithValidGuardPolicies(t *testing.T) {
	// Already tested in guard_policy_parsing_test.go, but adding a write-sink variant
	cfg := &config.Config{
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
	}
	us := &UnifiedServer{cfg: cfg}

	policy, source, err := us.resolveGuardPolicy("github")

	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "server", source)
	require.NotNil(t, policy.AllowOnly)
	assert.Equal(t, "approved", policy.AllowOnly.MinIntegrity)
}

func TestResolveGuardPolicy_ServerWithInvalidGuardPolicies(t *testing.T) {
	// GuardPolicies that ParseServerGuardPolicy rejects
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type: "stdio",
				GuardPolicies: map[string]interface{}{
					"allow-only": map[string]interface{}{
						// missing min-integrity → invalid
						"repos": "github/gh-aw*",
					},
				},
			},
		},
	}
	us := &UnifiedServer{cfg: cfg}

	policy, source, err := us.resolveGuardPolicy("github")

	require.Error(t, err, "missing min-integrity should cause an error")
	assert.Nil(t, policy)
	assert.Empty(t, source)
}

func TestResolveGuardPolicy_NoGuardPolicies_NoGuardField(t *testing.T) {
	// No GuardPolicies, no Guard field → legacy
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type:  "http",
				Guard: "",
			},
		},
	}
	us := &UnifiedServer{cfg: cfg}

	policy, source, err := us.resolveGuardPolicy("github")

	require.NoError(t, err)
	assert.Nil(t, policy)
	assert.Equal(t, "legacy", source)
}

func TestResolveGuardPolicy_GuardFieldSet_GuardNotInConfig(t *testing.T) {
	// Guard field set but the named guard doesn't exist in cfg.Guards
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type:  "http",
				Guard: "my-wasm-guard",
			},
		},
		Guards: map[string]*config.GuardConfig{},
	}
	us := &UnifiedServer{cfg: cfg}

	policy, source, err := us.resolveGuardPolicy("github")

	require.NoError(t, err)
	assert.Nil(t, policy)
	assert.Equal(t, "legacy", source)
}

func TestResolveGuardPolicy_GuardFieldSet_NilGuardConfig(t *testing.T) {
	// Guard field set but cfg.Guards[name] is nil
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type:  "http",
				Guard: "my-guard",
			},
		},
		Guards: map[string]*config.GuardConfig{
			"my-guard": nil,
		},
	}
	us := &UnifiedServer{cfg: cfg}

	policy, source, err := us.resolveGuardPolicy("github")

	require.NoError(t, err)
	assert.Nil(t, policy)
	assert.Equal(t, "legacy", source)
}

func TestResolveGuardPolicy_GuardFieldSet_NilGuardPolicy(t *testing.T) {
	// Guard exists in cfg.Guards but has no Policy set
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type:  "http",
				Guard: "my-guard",
			},
		},
		Guards: map[string]*config.GuardConfig{
			"my-guard": {
				Type:   "wasm",
				Path:   "/path/to/guard.wasm",
				Policy: nil,
			},
		},
	}
	us := &UnifiedServer{cfg: cfg}

	policy, source, err := us.resolveGuardPolicy("github")

	require.NoError(t, err)
	assert.Nil(t, policy)
	assert.Equal(t, "legacy", source)
}

func TestResolveGuardPolicy_GuardFieldSet_ValidGuardPolicy(t *testing.T) {
	// Guard exists and has a valid AllowOnly policy
	guardPolicy := validAllowOnlyPolicy()
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type:  "http",
				Guard: "my-guard",
			},
		},
		Guards: map[string]*config.GuardConfig{
			"my-guard": {
				Type:   "wasm",
				Policy: guardPolicy,
			},
		},
	}
	us := &UnifiedServer{cfg: cfg}

	policy, source, err := us.resolveGuardPolicy("github")

	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "config", source)
	require.NotNil(t, policy.AllowOnly)
	assert.Equal(t, config.IntegrityNone, policy.AllowOnly.MinIntegrity)
}

func TestResolveGuardPolicy_GuardFieldSet_WriteSinkGuardPolicy(t *testing.T) {
	// Guard exists and has a valid WriteSink policy
	guardPolicy := validWriteSinkPolicy()
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type:  "http",
				Guard: "sink-guard",
			},
		},
		Guards: map[string]*config.GuardConfig{
			"sink-guard": {
				Type:   "wasm",
				Policy: guardPolicy,
			},
		},
	}
	us := &UnifiedServer{cfg: cfg}

	policy, source, err := us.resolveGuardPolicy("github")

	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "config", source)
	require.NotNil(t, policy.WriteSink)
	assert.Equal(t, []string{"private:myorg"}, policy.WriteSink.Accept)
}

func TestResolveGuardPolicy_GuardFieldSet_InvalidGuardPolicy(t *testing.T) {
	// Guard exists but has an invalid policy (neither AllowOnly nor WriteSink set)
	invalidPolicy := &config.GuardPolicy{}
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type:  "http",
				Guard: "bad-guard",
			},
		},
		Guards: map[string]*config.GuardConfig{
			"bad-guard": {
				Type:   "wasm",
				Policy: invalidPolicy,
			},
		},
	}
	us := &UnifiedServer{cfg: cfg}

	policy, source, err := us.resolveGuardPolicy("github")

	require.Error(t, err, "empty policy (no AllowOnly or WriteSink) should be invalid")
	assert.Nil(t, policy)
	assert.Empty(t, source)
}

func TestResolveGuardPolicy_EmptyServersMap(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{},
	}
	us := &UnifiedServer{cfg: cfg}

	policy, source, err := us.resolveGuardPolicy("github")

	require.NoError(t, err)
	assert.Nil(t, policy)
	assert.Equal(t, "legacy", source)
}

// ---- resolveWriteSinkPolicy tests ----

func TestResolveWriteSinkPolicy_NoPolicy(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http"},
		},
	}
	us := &UnifiedServer{cfg: cfg}

	result := us.resolveWriteSinkPolicy("github")
	assert.Nil(t, result, "no guard policy should return nil write-sink policy")
}

func TestResolveWriteSinkPolicy_WriteSinkPolicy(t *testing.T) {
	guardPolicy := validWriteSinkPolicy()
	cfg := &config.Config{
		GuardPolicy:       guardPolicy,
		GuardPolicySource: "cli",
	}
	us := &UnifiedServer{cfg: cfg}

	result := us.resolveWriteSinkPolicy("github")
	require.NotNil(t, result)
	assert.Equal(t, []string{"private:myorg"}, result.Accept)
}

func TestResolveWriteSinkPolicy_AllowOnlyPolicyReturnsNilWriteSink(t *testing.T) {
	guardPolicy := validAllowOnlyPolicy()
	cfg := &config.Config{
		GuardPolicy:       guardPolicy,
		GuardPolicySource: "cli",
	}
	us := &UnifiedServer{cfg: cfg}

	result := us.resolveWriteSinkPolicy("github")
	assert.Nil(t, result, "allow-only policy has no write-sink")
}

func TestResolveWriteSinkPolicy_ErrorReturnsNil(t *testing.T) {
	// Invalid global policy causes resolveGuardPolicy to return an error;
	// resolveWriteSinkPolicy should return nil in that case.
	invalidPolicy := &config.GuardPolicy{}
	cfg := &config.Config{
		GuardPolicy: invalidPolicy,
	}
	us := &UnifiedServer{cfg: cfg}

	result := us.resolveWriteSinkPolicy("github")
	assert.Nil(t, result, "error from resolveGuardPolicy should result in nil write-sink policy")
}

func TestResolveWriteSinkPolicy_NilConfig(t *testing.T) {
	us := &UnifiedServer{cfg: nil}
	result := us.resolveWriteSinkPolicy("github")
	assert.Nil(t, result)
}
