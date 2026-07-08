package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startMockBackend starts a simple HTTP server that returns 500 for all requests.
// This allows the gateway to start without needing Docker.
func startMockBackend(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"mock backend"}`))
	}))
}

// startMockGitHubAPI starts a mock GitHub API that returns the specified visibility.
func startMockGitHubAPI(t *testing.T, visibility string, private bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"visibility": visibility,
				"private":    private,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// TestSinkVisibility_ConfigAccepted verifies the gateway starts successfully
// with various sink-visibility configurations in the write-sink guard policy.
func TestSinkVisibility_ConfigAccepted(t *testing.T) {
	binary := binaryPath(t)

	tests := []struct {
		name           string
		sinkVisibility string
		accept         string
		wantInLog      string
	}{
		{
			name:           "public visibility",
			sinkVisibility: `"public"`,
			accept:         `["*"]`,
			wantInLog:      "write-sink guard",
		},
		{
			name:           "private visibility",
			sinkVisibility: `"private"`,
			accept:         `["private:owner/repo"]`,
			wantInLog:      "write-sink guard",
		},
		{
			name:           "internal visibility",
			sinkVisibility: `"internal"`,
			accept:         `["private:owner/repo"]`,
			wantInLog:      "write-sink guard",
		},
		{
			name:           "omitted visibility (backward compat)",
			sinkVisibility: "", // will not include the field
			accept:         `["*"]`,
			wantInLog:      "write-sink guard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := getFreePort(t)
			logDir := t.TempDir()

			backend := startMockBackend(t)
			defer backend.Close()

			var writeSinkJSON string
			if tt.sinkVisibility == "" {
				writeSinkJSON = fmt.Sprintf(`{"accept": %s}`, tt.accept)
			} else {
				writeSinkJSON = fmt.Sprintf(`{"accept": %s, "sink-visibility": %s}`, tt.accept, tt.sinkVisibility)
			}

			config := fmt.Sprintf(`{
				"mcpServers": {
					"safe-outputs": {
						"type": "http",
						"url": "%s",
						"guard-policies": {
							"write-sink": %s
						}
					}
				},
				"gateway": {
					"port": %d,
					"domain": "localhost",
					"agentId": "test-key"
				}
			}`, backend.URL, writeSinkJSON, port)

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, binary, "--config-stdin", "--log-dir", logDir)
			cmd.Stdin = strings.NewReader(config)
			// Remove GITHUB_REPOSITORY to skip runtime check
			filteredEnv := filterEnv(os.Environ(), "GITHUB_REPOSITORY")
			filteredEnv = append(filteredEnv, "MCP_GATEWAY_WASM_GUARDS_DIR=")
			cmd.Env = filteredEnv

			var stderr syncBuffer
			cmd.Stdout = &bytes.Buffer{}
			cmd.Stderr = &stderr

			err := cmd.Start()
			require.NoError(t, err, "Failed to start gateway")

			ok := waitForStderr(&stderr, "Starting MCPG", 12*time.Second)
			require.Truef(t, ok, "timeout waiting for startup; stderr:\n%s", stderr.String())

			cmd.Process.Kill()
			cmd.Wait()

			logContent := readUnifiedLog(logDir)
			assert.Contains(t, logContent, tt.wantInLog,
				"Log should contain write-sink guard registration")
			t.Logf("✓ sink-visibility=%s accepted", tt.sinkVisibility)
		})
	}
}

// TestSinkVisibility_InvalidValue verifies the gateway rejects invalid
// sink-visibility values via schema validation and exits with an error.
func TestSinkVisibility_InvalidValue(t *testing.T) {
	binary := binaryPath(t)

	tests := []struct {
		name           string
		sinkVisibility string
	}{
		{
			name:           "invalid value foo",
			sinkVisibility: `"foo"`,
		},
		{
			name:           "invalid value restricted",
			sinkVisibility: `"restricted"`,
		},
		{
			name:           "invalid numeric value",
			sinkVisibility: `"123"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := getFreePort(t)
			logDir := t.TempDir()

			backend := startMockBackend(t)
			defer backend.Close()

			config := fmt.Sprintf(`{
				"mcpServers": {
					"safe-outputs": {
						"type": "http",
						"url": "%s",
						"guard-policies": {
							"write-sink": {
								"accept": ["*"],
								"sink-visibility": %s
							}
						}
					}
				},
				"gateway": {
					"port": %d,
					"domain": "localhost",
					"agentId": "test-key"
				}
			}`, backend.URL, tt.sinkVisibility, port)

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, binary, "--config-stdin", "--log-dir", logDir)
			cmd.Stdin = strings.NewReader(config)
			filteredEnv := filterEnv(os.Environ(), "GITHUB_REPOSITORY")
			filteredEnv = append(filteredEnv, "MCP_GATEWAY_WASM_GUARDS_DIR=")
			cmd.Env = filteredEnv

			var stderr syncBuffer
			cmd.Stdout = &bytes.Buffer{}
			cmd.Stderr = &stderr

			err := cmd.Start()
			require.NoError(t, err, "Failed to start gateway")

			// Gateway should exit due to schema validation failure
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()

			select {
			case err := <-done:
				// Gateway exited — expected due to schema validation failure
				assert.Error(t, err, "Gateway should exit with error on invalid sink-visibility")
				output := stderr.String()
				assert.Contains(t, output, "sink-visibility",
					"Stderr should mention the invalid sink-visibility field")
				t.Logf("✓ Invalid sink-visibility=%s rejected by schema validation", tt.sinkVisibility)
			case <-time.After(12 * time.Second):
				cmd.Process.Kill()
				cmd.Wait()
				t.Fatalf("Gateway did not exit within timeout; stderr:\n%s", stderr.String())
			}
		})
	}
}

// TestSinkVisibility_RuntimeOverride verifies that the gateway overrides
// sink-visibility when the GitHub API reports the repo is public but config
// says private. Uses a mock GitHub API server.
func TestSinkVisibility_RuntimeOverride(t *testing.T) {
	binary := binaryPath(t)

	tests := []struct {
		name              string
		configuredVis     string
		apiVisibility     string
		apiPrivate        bool
		wantOverrideLog   string
		wantNoOverrideLog string
	}{
		{
			name:            "private config but public repo — override with warning",
			configuredVis:   "private",
			apiVisibility:   "public",
			apiPrivate:      false,
			wantOverrideLog: "SINK VISIBILITY OVERRIDE",
		},
		{
			name:            "internal config but public repo — override with warning",
			configuredVis:   "internal",
			apiVisibility:   "public",
			apiPrivate:      false,
			wantOverrideLog: "SINK VISIBILITY OVERRIDE",
		},
		{
			name:              "public config and public repo — no override",
			configuredVis:     "public",
			apiVisibility:     "public",
			apiPrivate:        false,
			wantNoOverrideLog: "runtime verification passed",
		},
		{
			name:              "private config and private repo — no override",
			configuredVis:     "private",
			apiVisibility:     "private",
			apiPrivate:        true,
			wantNoOverrideLog: "runtime verification passed",
		},
		{
			name:              "public config but private repo — keep public (more restrictive)",
			configuredVis:     "public",
			apiVisibility:     "private",
			apiPrivate:        true,
			wantNoOverrideLog: "runtime verification passed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := getFreePort(t)
			logDir := t.TempDir()

			backend := startMockBackend(t)
			defer backend.Close()

			mockAPI := startMockGitHubAPI(t, tt.apiVisibility, tt.apiPrivate)
			defer mockAPI.Close()

			writeSinkJSON := fmt.Sprintf(`{"accept": ["*"], "sink-visibility": "%s"}`, tt.configuredVis)

			config := fmt.Sprintf(`{
				"mcpServers": {
					"safe-outputs": {
						"type": "http",
						"url": "%s",
						"guard-policies": {
							"write-sink": %s
						}
					}
				},
				"gateway": {
					"port": %d,
					"domain": "localhost",
					"agentId": "test-key"
				}
			}`, backend.URL, writeSinkJSON, port)

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, binary, "--config-stdin", "--log-dir", logDir)
			cmd.Stdin = strings.NewReader(config)
			// Remove all GitHub token vars and set our mock values
			filteredEnv := filterEnv(os.Environ(),
				"GITHUB_REPOSITORY", "GITHUB_TOKEN", "GITHUB_MCP_SERVER_TOKEN",
				"GITHUB_PERSONAL_ACCESS_TOKEN", "GH_TOKEN", "GITHUB_API_URL",
			)
			filteredEnv = append(filteredEnv,
				"GITHUB_REPOSITORY=test-owner/test-repo",
				"GITHUB_TOKEN=mock-token-for-testing",
				fmt.Sprintf("GITHUB_API_URL=%s", mockAPI.URL),
				"MCP_GATEWAY_WASM_GUARDS_DIR=",
			)
			cmd.Env = filteredEnv

			var stderr syncBuffer
			cmd.Stdout = &bytes.Buffer{}
			cmd.Stderr = &stderr

			err := cmd.Start()
			require.NoError(t, err, "Failed to start gateway")

			ok := waitForStderr(&stderr, "Starting MCPG", 12*time.Second)
			require.Truef(t, ok, "timeout waiting for startup; stderr:\n%s", stderr.String())

			cmd.Process.Kill()
			cmd.Wait()

			logContent := readUnifiedLog(logDir)

			if tt.wantOverrideLog != "" {
				assert.Contains(t, logContent, tt.wantOverrideLog,
					"Log should contain override warning when repo is more public than configured")
				t.Logf("✓ Override warning emitted: configured=%q, actual=%s", tt.configuredVis, tt.apiVisibility)
			}
			if tt.wantNoOverrideLog != "" {
				assert.Contains(t, logContent, tt.wantNoOverrideLog,
					"Log should confirm verification passed when no override needed")
				assert.NotContains(t, logContent, "SINK VISIBILITY OVERRIDE",
					"Should NOT contain override warning")
				t.Logf("✓ No override: configured=%q, actual=%s", tt.configuredVis, tt.apiVisibility)
			}
		})
	}
}

// TestSinkVisibility_RuntimeCheckSkippedWithoutRepo verifies that when
// GITHUB_REPOSITORY is not set, the runtime check is skipped gracefully.
func TestSinkVisibility_RuntimeCheckSkippedWithoutRepo(t *testing.T) {
	binary := binaryPath(t)
	port := getFreePort(t)
	logDir := t.TempDir()

	backend := startMockBackend(t)
	defer backend.Close()

	config := fmt.Sprintf(`{
		"mcpServers": {
			"safe-outputs": {
				"type": "http",
				"url": "%s",
				"guard-policies": {
					"write-sink": {
						"accept": ["*"],
						"sink-visibility": "private"
					}
				}
			}
		},
		"gateway": {
			"port": %d,
			"domain": "localhost",
			"agentId": "test-key"
		}
	}`, backend.URL, port)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "--config-stdin", "--log-dir", logDir)
	cmd.Stdin = strings.NewReader(config)

	// Explicitly remove GITHUB_REPOSITORY from env
	filteredEnv := filterEnv(os.Environ(), "GITHUB_REPOSITORY")
	filteredEnv = append(filteredEnv, "MCP_GATEWAY_WASM_GUARDS_DIR=")
	cmd.Env = filteredEnv

	var stderr syncBuffer
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &stderr

	err := cmd.Start()
	require.NoError(t, err, "Failed to start gateway")

	ok := waitForStderr(&stderr, "Starting MCPG", 12*time.Second)
	require.Truef(t, ok, "timeout waiting for startup; stderr:\n%s", stderr.String())

	cmd.Process.Kill()
	cmd.Wait()

	logContent := readUnifiedLog(logDir)
	assert.Contains(t, logContent, "write-sink guard",
		"Write-sink guard should still be created without GITHUB_REPOSITORY")
	assert.NotContains(t, logContent, "SINK VISIBILITY OVERRIDE",
		"Should not override when no runtime check was performed")
	t.Log("✓ Runtime check gracefully skipped without GITHUB_REPOSITORY")
}

// TestSinkVisibility_RuntimeCheckSkippedWithoutToken verifies that when
// no GitHub token is available, the runtime check is skipped gracefully.
func TestSinkVisibility_RuntimeCheckSkippedWithoutToken(t *testing.T) {
	binary := binaryPath(t)
	port := getFreePort(t)
	logDir := t.TempDir()

	backend := startMockBackend(t)
	defer backend.Close()

	config := fmt.Sprintf(`{
		"mcpServers": {
			"safe-outputs": {
				"type": "http",
				"url": "%s",
				"guard-policies": {
					"write-sink": {
						"accept": ["*"],
						"sink-visibility": "private"
					}
				}
			}
		},
		"gateway": {
			"port": %d,
			"domain": "localhost",
			"agentId": "test-key"
		}
	}`, backend.URL, port)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "--config-stdin", "--log-dir", logDir)
	cmd.Stdin = strings.NewReader(config)

	// Remove all GitHub token env vars but keep GITHUB_REPOSITORY
	filteredEnv := filterEnv(os.Environ(),
		"GITHUB_TOKEN", "GITHUB_MCP_SERVER_TOKEN",
		"GITHUB_PERSONAL_ACCESS_TOKEN", "GH_TOKEN",
	)
	filteredEnv = append(filteredEnv,
		"GITHUB_REPOSITORY=test-owner/test-repo",
		"MCP_GATEWAY_WASM_GUARDS_DIR=",
	)
	cmd.Env = filteredEnv

	var stderr syncBuffer
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &stderr

	err := cmd.Start()
	require.NoError(t, err, "Failed to start gateway")

	ok := waitForStderr(&stderr, "Starting MCPG", 12*time.Second)
	require.Truef(t, ok, "timeout waiting for startup; stderr:\n%s", stderr.String())

	cmd.Process.Kill()
	cmd.Wait()

	logContent := readUnifiedLog(logDir)
	assert.Contains(t, logContent, "write-sink guard",
		"Write-sink guard should still be created without a token")
	assert.NotContains(t, logContent, "SINK VISIBILITY OVERRIDE",
		"Should not override when no runtime check was performed")
	t.Log("✓ Runtime check gracefully skipped without GitHub token")
}

// TestSinkVisibility_RuntimeCheckAPIFailure verifies that when the GitHub API
// returns an error, the gateway falls back to the configured value with a warning.
func TestSinkVisibility_RuntimeCheckAPIFailure(t *testing.T) {
	binary := binaryPath(t)
	port := getFreePort(t)
	logDir := t.TempDir()

	backend := startMockBackend(t)
	defer backend.Close()

	// Mock API that always returns 500
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockAPI.Close()

	config := fmt.Sprintf(`{
		"mcpServers": {
			"safe-outputs": {
				"type": "http",
				"url": "%s",
				"guard-policies": {
					"write-sink": {
						"accept": ["*"],
						"sink-visibility": "private"
					}
				}
			}
		},
		"gateway": {
			"port": %d,
			"domain": "localhost",
			"agentId": "test-key"
		}
	}`, backend.URL, port)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "--config-stdin", "--log-dir", logDir)
	cmd.Stdin = strings.NewReader(config)
	filteredEnv := filterEnv(os.Environ(),
		"GITHUB_REPOSITORY", "GITHUB_TOKEN", "GITHUB_MCP_SERVER_TOKEN",
		"GITHUB_PERSONAL_ACCESS_TOKEN", "GH_TOKEN", "GITHUB_API_URL",
	)
	filteredEnv = append(filteredEnv,
		"GITHUB_REPOSITORY=test-owner/test-repo",
		"GITHUB_TOKEN=mock-token",
		fmt.Sprintf("GITHUB_API_URL=%s", mockAPI.URL),
		"MCP_GATEWAY_WASM_GUARDS_DIR=",
	)
	cmd.Env = filteredEnv

	var stderr syncBuffer
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &stderr

	err := cmd.Start()
	require.NoError(t, err, "Failed to start gateway")

	ok := waitForStderr(&stderr, "Starting MCPG", 12*time.Second)
	require.Truef(t, ok, "timeout waiting for startup; stderr:\n%s", stderr.String())

	cmd.Process.Kill()
	cmd.Wait()

	logContent := readUnifiedLog(logDir)
	// Should log a warning about the failed check but still start
	assert.Contains(t, logContent, "write-sink guard",
		"Write-sink guard should still be created on API failure")
	assert.Contains(t, logContent, "runtime verification failed",
		"Should log warning about failed verification")
	assert.NotContains(t, logContent, "SINK VISIBILITY OVERRIDE",
		"Should not override on API failure — keeps configured value")
	t.Log("✓ API failure handled gracefully — falls back to configured value with warning")
}

// TestSinkVisibility_WriteSinkGuardLogsSinkVisibility verifies that the
// write-sink guard logs include the effective sink-visibility value.
func TestSinkVisibility_WriteSinkGuardLogsSinkVisibility(t *testing.T) {
	binary := binaryPath(t)

	tests := []struct {
		name      string
		config    string
		wantInLog string
	}{
		{
			name:      "public visibility logged",
			config:    `{"accept": ["*"], "sink-visibility": "public"}`,
			wantInLog: `sink-visibility="public"`,
		},
		{
			name:      "private visibility logged",
			config:    `{"accept": ["private:org/repo"], "sink-visibility": "private"}`,
			wantInLog: `sink-visibility="private"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := getFreePort(t)
			logDir := t.TempDir()

			backend := startMockBackend(t)
			defer backend.Close()

			config := fmt.Sprintf(`{
				"mcpServers": {
					"safe-outputs": {
						"type": "http",
						"url": "%s",
						"guard-policies": {
							"write-sink": %s
						}
					}
				},
				"gateway": {
					"port": %d,
					"domain": "localhost",
					"agentId": "test-key"
				}
			}`, backend.URL, tt.config, port)

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, binary, "--config-stdin", "--log-dir", logDir)
			cmd.Stdin = strings.NewReader(config)
			// Remove GITHUB_REPOSITORY to skip runtime check
			filteredEnv := filterEnv(os.Environ(), "GITHUB_REPOSITORY")
			filteredEnv = append(filteredEnv, "MCP_GATEWAY_WASM_GUARDS_DIR=")
			cmd.Env = filteredEnv

			var stderr syncBuffer
			cmd.Stdout = &bytes.Buffer{}
			cmd.Stderr = &stderr

			err := cmd.Start()
			require.NoError(t, err, "Failed to start gateway")

			ok := waitForStderr(&stderr, "Starting MCPG", 12*time.Second)
			require.Truef(t, ok, "timeout waiting for startup; stderr:\n%s", stderr.String())

			cmd.Process.Kill()
			cmd.Wait()

			logContent := readUnifiedLog(logDir)
			assert.Contains(t, logContent, tt.wantInLog,
				"Log should contain the effective sink-visibility")
		})
	}
}

// filterEnv returns a copy of env with the specified keys removed.
func filterEnv(env []string, keys ...string) []string {
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		key := strings.SplitN(e, "=", 2)[0]
		if !keySet[key] {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
