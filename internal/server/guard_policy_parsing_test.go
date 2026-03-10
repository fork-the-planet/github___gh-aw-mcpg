package server

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseServerGuardPolicy_AllowOnly tests parsing guard-policies with allow-only format
func TestParseServerGuardPolicy_AllowOnly(t *testing.T) {
	// This is the exact format from smoke-allowonly.lock.yml
	guardPolicies := map[string]interface{}{
		"allow-only": map[string]interface{}{
			"min-integrity": "approved",
			"repos":         []interface{}{"github/gh-aw*"},
		},
	}

	policy, err := parseServerGuardPolicy("github", guardPolicies)
	require.NoError(t, err, "parseServerGuardPolicy should not return error")
	require.NotNil(t, policy, "policy should not be nil")
	require.NotNil(t, policy.AllowOnly, "policy.AllowOnly should not be nil")

	assert.Equal(t, "approved", policy.AllowOnly.MinIntegrity, "MinIntegrity should be 'approved'")

	// Repos can be either a string or a slice after parsing
	// Verify the repos field is present and has the expected value
	reposSlice, ok := policy.AllowOnly.Repos.([]interface{})
	if ok {
		require.Len(t, reposSlice, 1, "repos should have 1 element")
		assert.Equal(t, "github/gh-aw*", reposSlice[0], "repos[0] should be 'github/gh-aw*'")
	} else {
		t.Fatalf("repos is not a []interface{}, got %T: %v", policy.AllowOnly.Repos, policy.AllowOnly.Repos)
	}
}

// TestResolveGuardPolicy_ServerGuardPolicies tests resolving policy from server guard-policies
func TestResolveGuardPolicy_ServerGuardPolicies(t *testing.T) {
	cfg := &config.Config{
		DIFCMode: "strict",
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

	policy, source, err := us.resolveGuardPolicy("github")
	require.NoError(t, err, "resolveGuardPolicy should not return error")
	require.NotNil(t, policy, "policy should not be nil")
	assert.Equal(t, "server", source, "source should be 'server'")
	require.NotNil(t, policy.AllowOnly, "policy.AllowOnly should not be nil")
	assert.Equal(t, "approved", policy.AllowOnly.MinIntegrity, "MinIntegrity should be 'approved'")
}
