package server

import (
	"context"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGuard is a simple mock guard for testing
type mockGuard struct {
	name string
}

func (m *mockGuard) Name() string {
	return m.name
}

func (m *mockGuard) LabelAgent(ctx context.Context, policy interface{}, backend guard.BackendCaller, caps *difc.Capabilities) (*guard.LabelAgentResult, error) {
	return &guard.LabelAgentResult{
		Agent: guard.AgentLabelsPayload{
			Secrecy:   []string{"mock"},
			Integrity: []string{"mock"},
		},
		DIFCMode: "filter",
		NormalizedPolicy: map[string]interface{}{
			"scope_kind": "scoped",
		},
	}, nil
}

func (m *mockGuard) LabelResource(ctx context.Context, toolName string, args interface{}, backend guard.BackendCaller, caps *difc.Capabilities) (*difc.LabeledResource, difc.OperationType, error) {
	return nil, difc.OperationRead, nil
}

func (m *mockGuard) LabelResponse(ctx context.Context, toolName string, result interface{}, backend guard.BackendCaller, caps *difc.Capabilities) (difc.LabeledData, error) {
	return nil, nil
}

// TestRequireGuardPolicyIfGuardEnabled_WithServerGuardPolicies tests that a non-noop guard
// is kept when guard policies are configured at the server level, even if resolveGuardPolicy
// returns nil (which can happen during initialization before policies are fully resolved)
func TestRequireGuardPolicyIfGuardEnabled_WithServerGuardPolicies(t *testing.T) {
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

	us := &UnifiedServer{
		cfg: cfg,
	}

	mockG := &mockGuard{name: "mock-guard"}

	// Call requireGuardPolicyIfGuardEnabled
	resultGuard, err := us.requireGuardPolicyIfGuardEnabled("github", mockG)

	require.NoError(t, err, "should not return error")
	require.NotNil(t, resultGuard, "should return a guard")

	// The guard should NOT be downgraded to noop because guard policies exist
	assert.Equal(t, "mock-guard", resultGuard.Name(), "should keep the non-noop guard")
	assert.NotEqual(t, "noop", resultGuard.Name(), "should not fallback to noop guard")
}

// TestRequireGuardPolicyIfGuardEnabled_WithoutPolicies tests that a non-noop guard
// is downgraded to noop when no policies are configured
func TestRequireGuardPolicyIfGuardEnabled_WithoutPolicies(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type: "stdio",
				// No GuardPolicies configured
			},
		},
	}

	us := &UnifiedServer{
		cfg: cfg,
	}

	mockG := &mockGuard{name: "mock-guard"}

	// Call requireGuardPolicyIfGuardEnabled
	resultGuard, err := us.requireGuardPolicyIfGuardEnabled("github", mockG)

	require.NoError(t, err, "should not return error")
	require.NotNil(t, resultGuard, "should return a guard")

	// The guard should be downgraded to noop because no policies exist
	assert.Equal(t, "noop", resultGuard.Name(), "should fallback to noop guard")
}

// TestRequireGuardPolicyIfGuardEnabled_WithEmptyPolicies tests that a non-noop guard
// is downgraded to noop when guard policies are empty
func TestRequireGuardPolicyIfGuardEnabled_WithEmptyPolicies(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type:          "stdio",
				GuardPolicies: map[string]interface{}{}, // Empty map
			},
		},
	}

	us := &UnifiedServer{
		cfg: cfg,
	}

	mockG := &mockGuard{name: "mock-guard"}

	// Call requireGuardPolicyIfGuardEnabled
	resultGuard, err := us.requireGuardPolicyIfGuardEnabled("github", mockG)

	require.NoError(t, err, "should not return error")
	require.NotNil(t, resultGuard, "should return a guard")

	// The guard should be downgraded to noop because policies are empty
	assert.Equal(t, "noop", resultGuard.Name(), "should fallback to noop guard")
}

// TestRequireGuardPolicyIfGuardEnabled_NilGuard tests the early-return path when
// the guard itself is nil. No policy lookup occurs; nil is returned immediately.
func TestRequireGuardPolicyIfGuardEnabled_NilGuard(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "stdio"},
		},
	}

	us := &UnifiedServer{cfg: cfg}

	resultGuard, err := us.requireGuardPolicyIfGuardEnabled("github", nil)

	require.NoError(t, err, "nil guard should not produce an error")
	assert.Nil(t, resultGuard, "nil guard should be returned as-is")
}

// TestRequireGuardPolicyIfGuardEnabled_NoopGuard tests the early-return path when
// the guard is already a noop guard. No policy lookup occurs; the noop guard is
// returned immediately without modification.
func TestRequireGuardPolicyIfGuardEnabled_NoopGuard(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "stdio"},
		},
	}

	us := &UnifiedServer{cfg: cfg}
	noopG := guard.NewNoopGuard()

	resultGuard, err := us.requireGuardPolicyIfGuardEnabled("github", noopG)

	require.NoError(t, err, "noop guard should not produce an error")
	require.NotNil(t, resultGuard, "should return a guard")
	assert.Equal(t, "noop", resultGuard.Name(), "noop guard should be returned as-is")
}

// TestRequireGuardPolicyIfGuardEnabled_WithValidGlobalPolicy tests that a non-noop
// guard is kept when a valid global GuardPolicy is configured (policy != nil path).
func TestRequireGuardPolicyIfGuardEnabled_WithValidGlobalPolicy(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "stdio"},
		},
		GuardPolicy: &config.GuardPolicy{
			AllowOnly: &config.AllowOnlyPolicy{
				MinIntegrity: "none",
				Repos:        "public",
			},
		},
	}

	us := &UnifiedServer{cfg: cfg}
	mockG := &mockGuard{name: "mock-guard"}

	resultGuard, err := us.requireGuardPolicyIfGuardEnabled("github", mockG)

	require.NoError(t, err, "valid global policy should not produce an error")
	require.NotNil(t, resultGuard, "should return a guard")
	assert.Equal(t, "mock-guard", resultGuard.Name(), "non-noop guard should be kept when valid policy is present")
}

// TestRequireGuardPolicyIfGuardEnabled_WithInvalidGlobalPolicy tests that an error
// is returned when the global GuardPolicy fails validation (error propagation path).
func TestRequireGuardPolicyIfGuardEnabled_WithInvalidGlobalPolicy(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "stdio"},
		},
		GuardPolicy: &config.GuardPolicy{
			AllowOnly: &config.AllowOnlyPolicy{
				MinIntegrity: "not-a-valid-level", // fails validation
				Repos:        "public",
			},
		},
	}

	us := &UnifiedServer{cfg: cfg}
	mockG := &mockGuard{name: "mock-guard"}

	resultGuard, err := us.requireGuardPolicyIfGuardEnabled("github", mockG)

	require.Error(t, err, "invalid policy should propagate an error")
	assert.Nil(t, resultGuard, "guard should be nil when policy validation fails")
	assert.ErrorContains(t, err, "min-integrity", "error should mention the invalid field")
}

// TestRequireGuardPolicyIfGuardEnabled_UnknownServerID tests that a non-noop guard
// is downgraded to noop when the serverID is not present in cfg.Servers
// (resolveGuardPolicy returns nil policy and the server lookup inside the nil-policy
// branch also finds no guard-policies entry).
func TestRequireGuardPolicyIfGuardEnabled_UnknownServerID(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"known-server": {Type: "stdio"},
		},
	}

	us := &UnifiedServer{cfg: cfg}
	mockG := &mockGuard{name: "mock-guard"}

	resultGuard, err := us.requireGuardPolicyIfGuardEnabled("unknown-server", mockG)

	require.NoError(t, err, "unknown server should not produce an error")
	require.NotNil(t, resultGuard, "should return a guard")
	assert.Equal(t, "noop", resultGuard.Name(), "should fallback to noop for an unknown server ID")
}
