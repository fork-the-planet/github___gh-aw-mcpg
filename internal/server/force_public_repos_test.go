package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── shouldForcePublicRepos ───────────────────────────────────────────────────

// TestShouldForcePublicRepos_PublicRepo verifies that shouldForcePublicRepos
// returns true when the workflow repository is public.
func TestShouldForcePublicRepos_PublicRepo(t *testing.T) {
	t.Setenv(guard.WASMGuardsDirEnvVar, "")
	apiServer := startMockRepoVisibilityServer(t, "public", false)

	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")
	t.Setenv("GITHUB_TOKEN", "mock-token")
	t.Setenv("GITHUB_API_URL", apiServer.URL)

	us := newMinimalUnifiedServerForGuardTest(&config.Config{
		Gateway: &config.GatewayConfig{},
	})

	result := us.shouldForcePublicRepos()

	assert.True(t, result, "shouldForcePublicRepos should return true for a public repo")
}

// TestShouldForcePublicRepos_PrivateRepo verifies that shouldForcePublicRepos
// returns false when the workflow repository is private.
func TestShouldForcePublicRepos_PrivateRepo(t *testing.T) {
	t.Setenv(guard.WASMGuardsDirEnvVar, "")
	apiServer := startMockRepoVisibilityServer(t, "private", true)

	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")
	t.Setenv("GITHUB_TOKEN", "mock-token")
	t.Setenv("GITHUB_API_URL", apiServer.URL)

	us := newMinimalUnifiedServerForGuardTest(&config.Config{
		Gateway: &config.GatewayConfig{},
	})

	result := us.shouldForcePublicRepos()

	assert.False(t, result, "shouldForcePublicRepos should return false for a private repo")
}

// TestShouldForcePublicRepos_ConfigOptOut verifies that shouldForcePublicRepos
// returns false when explicitly disabled in gateway config.
func TestShouldForcePublicRepos_ConfigOptOut(t *testing.T) {
	t.Setenv(guard.WASMGuardsDirEnvVar, "")
	// Even if the API would return "public", the config opt-out takes precedence.
	apiServer := startMockRepoVisibilityServer(t, "public", false)

	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")
	t.Setenv("GITHUB_TOKEN", "mock-token")
	t.Setenv("GITHUB_API_URL", apiServer.URL)

	disabled := false
	us := newMinimalUnifiedServerForGuardTest(&config.Config{
		Gateway: &config.GatewayConfig{
			ForcePublicRepos: &disabled,
		},
	})

	result := us.shouldForcePublicRepos()

	assert.False(t, result, "shouldForcePublicRepos should return false when disabled in config")
}

// TestShouldForcePublicRepos_EnvVarOptOut verifies that shouldForcePublicRepos
// returns false when disabled via MCP_GATEWAY_FORCE_PUBLIC_REPOS=false.
func TestShouldForcePublicRepos_EnvVarOptOut(t *testing.T) {
	t.Setenv(guard.WASMGuardsDirEnvVar, "")
	apiServer := startMockRepoVisibilityServer(t, "public", false)

	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")
	t.Setenv("GITHUB_TOKEN", "mock-token")
	t.Setenv("GITHUB_API_URL", apiServer.URL)
	t.Setenv(config.EnvForcePublicRepos, "false")

	us := newMinimalUnifiedServerForGuardTest(&config.Config{
		Gateway: &config.GatewayConfig{},
	})

	result := us.shouldForcePublicRepos()

	assert.False(t, result, "shouldForcePublicRepos should return false when MCP_GATEWAY_FORCE_PUBLIC_REPOS=false")
}

// TestShouldForcePublicRepos_NoGitHubRepository verifies that shouldForcePublicRepos
// returns false when GITHUB_REPOSITORY is not set.
func TestShouldForcePublicRepos_NoGitHubRepository(t *testing.T) {
	t.Setenv(guard.WASMGuardsDirEnvVar, "")
	t.Setenv("GITHUB_TOKEN", "mock-token")
	// Unset GITHUB_REPOSITORY
	t.Setenv("GITHUB_REPOSITORY", "")

	us := newMinimalUnifiedServerForGuardTest(&config.Config{
		Gateway: &config.GatewayConfig{},
	})

	result := us.shouldForcePublicRepos()

	assert.False(t, result, "shouldForcePublicRepos should return false without GITHUB_REPOSITORY")
}

// TestShouldForcePublicRepos_NoToken verifies that shouldForcePublicRepos
// returns false when no GitHub token is available.
func TestShouldForcePublicRepos_NoToken(t *testing.T) {
	t.Setenv(guard.WASMGuardsDirEnvVar, "")
	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")
	// Remove all token env vars
	t.Setenv("GITHUB_MCP_SERVER_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	us := newMinimalUnifiedServerForGuardTest(&config.Config{
		Gateway: &config.GatewayConfig{},
	})

	result := us.shouldForcePublicRepos()

	assert.False(t, result, "shouldForcePublicRepos should return false without a GitHub token")
}

// TestShouldForcePublicRepos_APIError verifies that shouldForcePublicRepos
// returns false (fail-open) when the GitHub API returns an error.
func TestShouldForcePublicRepos_APIError(t *testing.T) {
	t.Setenv(guard.WASMGuardsDirEnvVar, "")
	// Start a server that returns an error
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(apiServer.Close)

	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")
	t.Setenv("GITHUB_TOKEN", "mock-token")
	t.Setenv("GITHUB_API_URL", apiServer.URL)

	us := newMinimalUnifiedServerForGuardTest(&config.Config{
		Gateway: &config.GatewayConfig{},
	})

	result := us.shouldForcePublicRepos()

	assert.False(t, result, "shouldForcePublicRepos should return false (fail-open) on API error")
}

// ─── overrideToPublicScope ────────────────────────────────────────────────────

// TestOverrideToPublicScope_GlobalPolicy_OverridesRepos verifies that
// overrideToPublicScope sets repos="public" in the global guard policy.
func TestOverrideToPublicScope_GlobalPolicy_OverridesRepos(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {Type: "http"},
		},
		GuardPolicy: &config.GuardPolicy{
			AllowOnly: &config.AllowOnlyPolicy{
				Repos:        "all",
				MinIntegrity: config.IntegrityNone,
			},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	us.overrideToPublicScope("github")

	require.NotNil(t, cfg.GuardPolicy.AllowOnly)
	assert.Equal(t, "public", cfg.GuardPolicy.AllowOnly.Repos,
		"overrideToPublicScope should set repos=public in global policy")
}

// TestOverrideToPublicScope_GlobalPolicy_NoAllowOnly_WriteSinkOnly_Skipped verifies
// that overrideToPublicScope does NOT add AllowOnly to a write-sink-only global policy
// because allow-only and write-sink are mutually exclusive in the GuardPolicy schema.
func TestOverrideToPublicScope_GlobalPolicy_WriteSinkOnly(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"safe-outputs": {Type: "http"},
		},
		GuardPolicy: &config.GuardPolicy{
			WriteSink: &config.WriteSinkPolicy{Accept: []string{"*"}},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	us.overrideToPublicScope("safe-outputs")

	// Write-sink-only global policy: AllowOnly should NOT be added (mutually exclusive).
	assert.Nil(t, cfg.GuardPolicy.AllowOnly,
		"overrideToPublicScope should NOT add AllowOnly to a write-sink-only global policy")
}

// TestOverrideToPublicScope_PerServerPolicy_OverridesRepos verifies that
// overrideToPublicScope sets repos="public" in per-server guard policies.
func TestOverrideToPublicScope_PerServerPolicy_OverridesRepos(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type: "http",
				GuardPolicies: map[string]interface{}{
					"allow-only": map[string]interface{}{
						"repos":         "all",
						"min-integrity": "none",
					},
				},
			},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	us.overrideToPublicScope("github")

	// Parse back the modified policy to verify the override
	policy, err := config.ParseServerGuardPolicy("github", cfg.Servers["github"].GuardPolicies)
	require.NoError(t, err)
	require.NotNil(t, policy)
	require.NotNil(t, policy.AllowOnly)
	assert.Equal(t, "public", policy.AllowOnly.Repos,
		"overrideToPublicScope should override per-server allow-only to repos=public")
}

// TestOverrideToPublicScope_PerServerPolicy_WriteSinkOnly_Skipped verifies that
// overrideToPublicScope does NOT add AllowOnly to a write-sink-only per-server policy
// (allow-only and write-sink are mutually exclusive in the GuardPolicy schema).
func TestOverrideToPublicScope_PerServerPolicy_WriteSinkOnly(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type: "http",
				GuardPolicies: map[string]interface{}{
					"write-sink": map[string]interface{}{
						"accept": []interface{}{"*"},
					},
				},
			},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	us.overrideToPublicScope("github")

	// Write-sink-only per-server policy: AllowOnly should NOT be added.
	_, hasAllowOnly := cfg.Servers["github"].GuardPolicies["allow-only"]
	assert.False(t, hasAllowOnly,
		"overrideToPublicScope should NOT add allow-only to a write-sink-only policy")
}

// TestOverrideToPublicScope_NoExistingPolicy_InjectsDefault verifies that
// overrideToPublicScope injects a default allow-only policy when none is configured.
func TestOverrideToPublicScope_NoExistingPolicy_InjectsDefault(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Type:          "http",
				GuardPolicies: map[string]interface{}{},
			},
		},
	}
	us := newMinimalUnifiedServerForGuardTest(cfg)

	us.overrideToPublicScope("github")

	// The server should now have an allow-only policy injected
	assert.Greater(t, len(cfg.Servers["github"].GuardPolicies), 0,
		"overrideToPublicScope should inject a default policy when none exists")

	policy, err := config.ParseServerGuardPolicy("github", cfg.Servers["github"].GuardPolicies)
	require.NoError(t, err)
	require.NotNil(t, policy)
	require.NotNil(t, policy.AllowOnly)
	assert.Equal(t, "public", policy.AllowOnly.Repos)
}

// TestOverrideToPublicScope_NoConfig_NoOp verifies that overrideToPublicScope
// does not panic or error when the config is nil or has no servers.
func TestOverrideToPublicScope_NoConfig_NoOp(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")

	// nil config
	us := &UnifiedServer{guardRegistry: guard.NewRegistry()}
	assert.NotPanics(t, func() {
		us.overrideToPublicScope("github")
	}, "overrideToPublicScope should not panic with nil config")
}

// TestShouldForcePublicRepos_ResultCached verifies that the API is called only
// once even when shouldForcePublicRepos is called multiple times.
func TestShouldForcePublicRepos_ResultCached(t *testing.T) {
	t.Setenv(guard.WASMGuardsDirEnvVar, "")
	callCount := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "" && len(r.URL.Path) > 1 {
			callCount++
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"visibility": "public",
			"private":    false,
		})
	}))
	t.Cleanup(apiServer.Close)

	t.Setenv("GITHUB_REPOSITORY", "test-owner/test-repo")
	t.Setenv("GITHUB_TOKEN", "mock-token")
	t.Setenv("GITHUB_API_URL", apiServer.URL)

	us := newMinimalUnifiedServerForGuardTest(&config.Config{
		Gateway: &config.GatewayConfig{},
	})

	// Call multiple times
	result1 := us.shouldForcePublicRepos()
	result2 := us.shouldForcePublicRepos()
	result3 := us.shouldForcePublicRepos()

	assert.True(t, result1)
	assert.True(t, result2)
	assert.True(t, result3)
	// API should only have been called once (via resolveWorkflowRepoVisibility cache)
	assert.Equal(t, 1, callCount, "GitHub API should be called only once due to caching")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// startMockRepoVisibilityServer starts a mock HTTP server that returns a repo
// visibility response for any /repos/{owner}/{repo} request.
func startMockRepoVisibilityServer(t *testing.T, visibility string, private bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"visibility": visibility,
			"private":    private,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}
