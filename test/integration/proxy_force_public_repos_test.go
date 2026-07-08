package integration

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startProxyWithEnv starts the awmg proxy with additional args and env vars.
func startProxyWithEnv(t *testing.T, policyJSON string, port string, extraArgs []string, extraEnv []string) *proxyTestEnv {
	t.Helper()

	binaryPath := findBinary(t)
	wasmPath := findWasmGuard(t)
	token := skipIfNoGitHubToken(t)

	logDir, err := os.MkdirTemp("", "awmg-proxy-force-public-*")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)

	listenAddr := "127.0.0.1:" + port

	args := []string{
		"proxy",
		"--guard-wasm", wasmPath,
		"--policy", policyJSON,
		"--github-token", token,
		"--listen", listenAddr,
		"--log-dir", logDir,
		"--guards-mode", "filter",
	}
	args = append(args, extraArgs...)

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = append(os.Environ(), extraEnv...)

	env := &proxyTestEnv{
		cmd:     cmd,
		port:    port,
		baseURL: "http://" + listenAddr,
		token:   token,
		cancel:  cancel,
		logDir:  logDir,
	}

	cmd.Stdout = &env.stdout
	cmd.Stderr = &env.stderr

	err = cmd.Start()
	require.NoError(t, err, "Failed to start proxy")

	healthURL := env.baseURL + "/api/v3/health"
	if !waitForServer(t, healthURL, 15*time.Second) {
		t.Logf("STDOUT: %s", env.stdout.String())
		t.Logf("STDERR: %s", env.stderr.String())
		t.Fatal("Proxy did not start in time")
	}

	t.Logf("✓ Proxy started at %s", listenAddr)
	return env
}

// readLogs reads the proxy's mcp-gateway.log for assertion.
func (e *proxyTestEnv) readLogs(t *testing.T) string {
	t.Helper()
	logPath := e.logDir + "/mcp-gateway.log"
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// ghAPIWithStatus is a convenience wrapper for ghAPI that also returns body as string.
func (e *proxyTestEnv) ghAPIWithBody(t *testing.T, method, path string) (int, string) {
	t.Helper()
	url := e.baseURL + "/api/v3" + path
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "token "+e.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// ============================================================================
// Test: Public repo workflow → forces repos="public"
// ============================================================================

// TestProxyForcePublicRepos_PublicWorkflowRepo verifies that when
// GITHUB_REPOSITORY identifies a public repo, the proxy overrides the
// allow-only policy to repos="public" — even if the compiled policy is
// more permissive (repos="all" or repos=["specific-repos"]).
func TestProxyForcePublicRepos_PublicWorkflowRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping proxy integration test in short mode")
	}

	// Permissive policy: repos="all" — would allow access to everything
	policy := `{"allow-only":{"repos":"all","min-integrity":"none"}}`

	// Set GITHUB_REPOSITORY to a known public repo
	env := startProxyWithEnv(t, policy, "18920", nil, []string{
		"GITHUB_REPOSITORY=octocat/Hello-World",
	})
	defer env.stop(t)

	// Public repo should be accessible (octocat/Hello-World is public)
	t.Run("PublicRepo_Allowed", func(t *testing.T) {
		status, _ := env.ghAPIWithBody(t, "GET", "/repos/octocat/Hello-World/commits?per_page=1")
		assert.Equal(t, 200, status, "Public repo should be accessible")
	})

	// Verify logs show the override was applied
	t.Run("LogShowsOverride", func(t *testing.T) {
		// Give a moment for logs to flush
		time.Sleep(500 * time.Millisecond)
		logs := env.readLogs(t)
		stderr := env.stderr.String()
		combined := logs + stderr
		assert.Contains(t, combined, "FORCED REPOS=PUBLIC",
			"Should log that repos=public was forced")
	})
}

// ============================================================================
// Test: --force-public-repos=false disables the override
// ============================================================================

// TestProxyForcePublicRepos_FlagDisabled verifies that passing
// --force-public-repos=false prevents the runtime override, even when
// GITHUB_REPOSITORY is a public repo.
func TestProxyForcePublicRepos_FlagDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping proxy integration test in short mode")
	}

	// Permissive policy allowing a specific public repo
	policy := `{"allow-only":{"repos":["octocat/hello-world"],"min-integrity":"none"}}`

	// Disable force-public-repos via flag
	env := startProxyWithEnv(t, policy, "18921",
		[]string{"--force-public-repos=false"},
		[]string{"GITHUB_REPOSITORY=octocat/Hello-World"},
	)
	defer env.stop(t)

	// The policy should work as compiled — specific repo allowed
	t.Run("SpecificRepo_Allowed", func(t *testing.T) {
		status, _ := env.ghAPIWithBody(t, "GET", "/repos/octocat/Hello-World/commits?per_page=1")
		assert.Equal(t, 200, status, "Specifically allowed repo should be accessible")
	})

	// Verify logs show the flag disabled the override
	t.Run("LogShowsDisabled", func(t *testing.T) {
		time.Sleep(500 * time.Millisecond)
		stderr := env.stderr.String()
		assert.Contains(t, stderr, "forcePublicRepos: disabled by flag",
			"Should log that force-public-repos was disabled")
	})
}

// ============================================================================
// Test: MCP_GATEWAY_FORCE_PUBLIC_REPOS=false env var disables the override
// ============================================================================

// TestProxyForcePublicRepos_EnvVarDisabled verifies that setting
// MCP_GATEWAY_FORCE_PUBLIC_REPOS=false disables the runtime override.
func TestProxyForcePublicRepos_EnvVarDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping proxy integration test in short mode")
	}

	policy := `{"allow-only":{"repos":["octocat/hello-world"],"min-integrity":"none"}}`

	// Disable via env var (flag defaults from this)
	env := startProxyWithEnv(t, policy, "18922", nil, []string{
		"GITHUB_REPOSITORY=octocat/Hello-World",
		"MCP_GATEWAY_FORCE_PUBLIC_REPOS=false",
	})
	defer env.stop(t)

	t.Run("SpecificRepo_Allowed", func(t *testing.T) {
		status, _ := env.ghAPIWithBody(t, "GET", "/repos/octocat/Hello-World/commits?per_page=1")
		assert.Equal(t, 200, status, "Specifically allowed repo should be accessible")
	})

	t.Run("LogShowsDisabled", func(t *testing.T) {
		time.Sleep(500 * time.Millisecond)
		stderr := env.stderr.String()
		assert.Contains(t, stderr, "forcePublicRepos: disabled by flag",
			"Should log that force-public-repos was disabled by env var default")
	})
}

// ============================================================================
// Test: No GITHUB_REPOSITORY → no override
// ============================================================================

// TestProxyForcePublicRepos_NoGithubRepo verifies that when
// GITHUB_REPOSITORY is not set, no override is applied.
func TestProxyForcePublicRepos_NoGithubRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping proxy integration test in short mode")
	}

	// Permissive policy
	policy := `{"allow-only":{"repos":["octocat/hello-world"],"min-integrity":"none"}}`

	// Explicitly unset GITHUB_REPOSITORY by filtering it out
	filteredEnv := []string{}
	for _, e := range os.Environ() {
		if !bytes.HasPrefix([]byte(e), []byte("GITHUB_REPOSITORY=")) {
			filteredEnv = append(filteredEnv, e)
		}
	}

	binaryPath := findBinary(t)
	wasmPath := findWasmGuard(t)
	token := skipIfNoGitHubToken(t)

	logDir, err := os.MkdirTemp("", "awmg-proxy-norepo-*")
	require.NoError(t, err)
	defer os.RemoveAll(logDir)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	listenAddr := "127.0.0.1:18923"
	args := []string{
		"proxy",
		"--guard-wasm", wasmPath,
		"--policy", policy,
		"--github-token", token,
		"--listen", listenAddr,
		"--log-dir", logDir,
		"--guards-mode", "filter",
	}

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = filteredEnv
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Start()
	require.NoError(t, err)
	defer cmd.Process.Kill()

	healthURL := "http://" + listenAddr + "/api/v3/health"
	if !waitForServer(t, healthURL, 15*time.Second) {
		t.Logf("STDERR: %s", stderr.String())
		t.Fatal("Proxy did not start in time")
	}

	// Verify logs show skipped check
	time.Sleep(500 * time.Millisecond)
	assert.Contains(t, stderr.String(), "GITHUB_REPOSITORY not set",
		"Should log that GITHUB_REPOSITORY is not set")
}

// ============================================================================
// Test: Private repo → no override
// ============================================================================

// TestProxyForcePublicRepos_PrivateRepo verifies that when
// GITHUB_REPOSITORY identifies a private repo, no override is applied.
func TestProxyForcePublicRepos_PrivateRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping proxy integration test in short mode")
	}

	// Use a private repo that the token has access to (github/gh-aw-mcpg)
	policy := `{"allow-only":{"repos":["github/gh-aw-mcpg"],"min-integrity":"none"}}`

	env := startProxyWithEnv(t, policy, "18924", nil, []string{
		"GITHUB_REPOSITORY=github/gh-aw-mcpg",
	})
	defer env.stop(t)

	t.Run("PrivateRepo_Allowed", func(t *testing.T) {
		status, _ := env.ghAPIWithBody(t, "GET", "/repos/github/gh-aw-mcpg/commits?per_page=1")
		assert.Equal(t, 200, status, "Private repo (in policy) should be accessible")
	})

	t.Run("NoOverrideLogged", func(t *testing.T) {
		time.Sleep(500 * time.Millisecond)
		logs := env.readLogs(t)
		stderr := env.stderr.String()
		combined := logs + stderr
		assert.NotContains(t, combined, "FORCED REPOS=PUBLIC",
			"Should NOT force repos=public for private repo")
	})
}
