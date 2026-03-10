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
