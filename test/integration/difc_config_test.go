package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// binaryPath returns the path to the compiled awmg binary
func binaryPath(t *testing.T) string {
	// Look for binary in project root
	wd, err := os.Getwd()
	require.NoError(t, err)

	// Navigate up from test/integration to project root
	projectRoot := filepath.Join(wd, "..", "..")
	binary := filepath.Join(projectRoot, "awmg")

	if _, err := os.Stat(binary); os.IsNotExist(err) {
		t.Skip("Binary not found. Run 'make build' first.")
	}

	return binary
}

// getFreePort asks the OS for a free ephemeral TCP port and returns it.
// The listener is closed before the port number is returned, so there is a
// small TOCTOU window where another process could bind the same port before
// the gateway subprocess does. This is the standard approach when a port must
// be passed to a child process; callers should be aware of this limitation.
func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "failed to find a free port")
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestDIFCEnvironmentVariables tests that all guards-related environment variables are recognized
func TestDIFCEnvironmentVariables(t *testing.T) {
	binary := binaryPath(t)

	tests := []struct {
		name    string
		envVars map[string]string
		// We verify the gateway starts without error, which means env vars are accepted
	}{
		{
			name: "MCP_GATEWAY_ENABLE_GUARDS",
			envVars: map[string]string{
				"MCP_GATEWAY_ENABLE_GUARDS": "true",
			},
		},
		{
			name: "MCP_GATEWAY_GUARDS_MODE_strict",
			envVars: map[string]string{
				"MCP_GATEWAY_ENABLE_GUARDS": "true",
				"MCP_GATEWAY_GUARDS_MODE":   "strict",
			},
		},
		{
			name: "MCP_GATEWAY_GUARDS_MODE_filter",
			envVars: map[string]string{
				"MCP_GATEWAY_ENABLE_GUARDS": "true",
				"MCP_GATEWAY_GUARDS_MODE":   "filter",
			},
		},
		{
			name: "MCP_GATEWAY_GUARDS_MODE_propagate",
			envVars: map[string]string{
				"MCP_GATEWAY_ENABLE_GUARDS": "true",
				"MCP_GATEWAY_GUARDS_MODE":   "propagate",
			},
		},
		{
			name: "MCP_GATEWAY_CONFIG_EXTENSIONS",
			envVars: map[string]string{
				"MCP_GATEWAY_CONFIG_EXTENSIONS": "true",
			},
		},
		{
			name: "MCP_GATEWAY_SESSION_SECRECY",
			envVars: map[string]string{
				"MCP_GATEWAY_CONFIG_EXTENSIONS": "true",
				"MCP_GATEWAY_SESSION_SECRECY":   "secret,confidential",
			},
		},
		{
			name: "MCP_GATEWAY_SESSION_INTEGRITY",
			envVars: map[string]string{
				"MCP_GATEWAY_CONFIG_EXTENSIONS": "true",
				"MCP_GATEWAY_SESSION_INTEGRITY": "trusted,verified",
			},
		},
		{
			name: "all_guards_env_vars",
			envVars: map[string]string{
				"MCP_GATEWAY_ENABLE_GUARDS":     "true",
				"MCP_GATEWAY_GUARDS_MODE":       "propagate",
				"MCP_GATEWAY_CONFIG_EXTENSIONS": "true",
				"MCP_GATEWAY_SESSION_SECRECY":   "internal,secret",
				"MCP_GATEWAY_SESSION_INTEGRITY": "agent,verified",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := getFreePort(t) // Allocate a unique free port for each subtest

			// Create minimal config with apiKey (required by JSON stdin schema)
			config := fmt.Sprintf(`{
				"mcpServers": {
					"test": {
						"type": "stdio",
						"container": "echo"
					}
				},
				"gateway": {
					"port": %d,
					"apiKey": "test-key"
				}
			}`, port)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, binary, "--config-stdin")
			cmd.Stdin = strings.NewReader(config)

			// Set environment variables
			cmd.Env = os.Environ()
			for k, v := range tt.envVars {
				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
			}

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			// Start the process
			err := cmd.Start()
			require.NoError(t, err, "Failed to start gateway")

			// Give it time to start
			time.Sleep(500 * time.Millisecond)

			// Kill the process (we just want to verify it starts without error)
			cmd.Process.Kill()
			cmd.Wait()

			// Check stderr for configuration errors
			stderrStr := stderr.String()

			// Should not contain "invalid" errors related to our env vars
			assert.NotContains(t, stderrStr, "invalid --guards-mode", "Guards mode should be valid")
			assert.NotContains(t, stderrStr, "require --enable-config-extensions", "Config extensions should be respected when set")

			t.Logf("✓ Environment variables accepted: %v", tt.envVars)
		})
	}
}

// TestDIFCConfigWithGuards tests the full JSON config schema with guards
func TestDIFCConfigWithGuards(t *testing.T) {
	binary := binaryPath(t)

	// Set required environment variables
	os.Setenv("GITHUB_MCP_IMAGE", "ghcr.io/github/github-mcp-server:latest")
	os.Setenv("GITHUB_TOKEN", "test-token-12345")
	os.Setenv("PLAYWRIGHT_MCP_IMAGE", "mcp/playwright:latest")
	defer func() {
		os.Unsetenv("GITHUB_MCP_IMAGE")
		os.Unsetenv("GITHUB_TOKEN")
		os.Unsetenv("PLAYWRIGHT_MCP_IMAGE")
	}()

	config := `{
		"mcpServers": {
			"github": {
				"type": "stdio",
				"container": "$GITHUB_MCP_IMAGE",
				"env": {
					"GITHUB_PERSONAL_ACCESS_TOKEN": "$GITHUB_TOKEN"
				},
				"guard": "github-guard"
			},
			"playwright": {
				"type": "stdio",
				"container": "$PLAYWRIGHT_MCP_IMAGE",
				"env": {
					"PLAYWRIGHT_MCP_HEADLESS": "true"
				}
			}
		},
		"guards": {
			"github-guard": {
				"type": "wasm",
				"path": "/guard/github-guard-rust.wasm"
			}
		},
		"gateway": {
			"port": 13300,
			"domain": "localhost",
			"apiKey": "test-api-key"
		}
	}`

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "--config-stdin")
	cmd.Stdin = strings.NewReader(config)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Start()
	require.NoError(t, err, "Failed to start gateway")

	// Wait for startup
	time.Sleep(1 * time.Second)

	// Try health check
	resp, err := http.Get("http://127.0.0.1:13300/health")
	if err == nil {
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode, "Health check should return 200")
		t.Log("✓ Gateway started successfully with guards config")
	}

	// Clean up
	cmd.Process.Kill()
	cmd.Wait()

	stderrStr := stderr.String()
	t.Logf("STDERR: %s", stderrStr)

	// Verify config was parsed (should see server names in output)
	assert.Contains(t, stderrStr, "github", "Should log github server")
	assert.Contains(t, stderrStr, "playwright", "Should log playwright server")
}

// TestDIFCSessionLabelsViaEnv tests session labels configured via environment variables
func TestDIFCSessionLabelsViaEnv(t *testing.T) {
	binary := binaryPath(t)

	config := `{
		"mcpServers": {
			"test": {
				"type": "stdio",
				"container": "test/echo:latest"
			}
		},
		"gateway": {
			"port": 13301,
			"apiKey": "test-key"
		}
	}`

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "--config-stdin")
	cmd.Stdin = strings.NewReader(config)
	cmd.Env = append(os.Environ(),
		"MCP_GATEWAY_CONFIG_EXTENSIONS=true",
		"MCP_GATEWAY_SESSION_SECRECY=secret,confidential",
		"MCP_GATEWAY_SESSION_INTEGRITY=trusted",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Start()
	require.NoError(t, err, "Failed to start gateway")

	time.Sleep(1 * time.Second)

	// Try health check
	resp, err := http.Get("http://127.0.0.1:13301/health")
	if err == nil {
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	cmd.Process.Kill()
	cmd.Wait()

	stderrStr := stderr.String()
	t.Logf("STDERR: %s", stderrStr)

	// Verify session labels are logged
	assert.Contains(t, stderrStr, "Session labels configured", "Session labels should be logged")
	assert.Contains(t, stderrStr, "secret", "Secrecy label should be logged")
	assert.Contains(t, stderrStr, "confidential", "Secrecy label should be logged")
	assert.Contains(t, stderrStr, "trusted", "Integrity label should be logged")
	t.Log("✓ Session labels configured via environment variables")
}

// TestDIFCModeFilterViaEnv tests guards filter mode via MCP_GATEWAY_GUARDS_MODE
func TestDIFCModeFilterViaEnv(t *testing.T) {
	binary := binaryPath(t)

	config := `{
		"mcpServers": {
			"test": {
				"type": "stdio",
				"container": "test/echo:latest"
			}
		},
		"gateway": {
			"port": 13302,
			"domain": "localhost",
			"apiKey": "test-key"
		}
	}`

	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()

	cmd := exec.CommandContext(ctx2, binary, "--config-stdin")
	cmd.Stdin = strings.NewReader(config)
	cmd.Env = append(os.Environ(),
		"MCP_GATEWAY_ENABLE_GUARDS=true",
		"MCP_GATEWAY_GUARDS_MODE=filter",
		"MCP_GATEWAY_WASM_GUARDS_DIR=",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Start()
	require.NoError(t, err, "Failed to start gateway")

	time.Sleep(1 * time.Second)

	cmd.Process.Kill()
	cmd.Wait()

	stderrStr := stderr.String()
	t.Logf("STDERR: %s", stderrStr)

	// Verify filter mode is active
	assert.Contains(t, stderrStr, "filter", "Filter mode should be logged")
	assert.Contains(t, stderrStr, "Guards enforcement enabled", "Guards should be enabled")
	t.Log("✓ Guards filter mode enabled via MCP_GATEWAY_GUARDS_MODE=filter")
}

// TestDIFCModePropagateViaEnv tests guards propagate mode via MCP_GATEWAY_GUARDS_MODE
func TestDIFCModePropagateViaEnv(t *testing.T) {
	binary := binaryPath(t)

	config := `{
		"mcpServers": {
			"test": {
				"type": "stdio",
				"container": "test/echo:latest"
			}
		},
		"gateway": {
			"port": 13303,
			"domain": "localhost",
			"apiKey": "test-key"
		}
	}`

	ctx3, cancel3 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel3()

	cmd := exec.CommandContext(ctx3, binary, "--config-stdin")
	cmd.Stdin = strings.NewReader(config)
	cmd.Env = append(os.Environ(),
		"MCP_GATEWAY_ENABLE_GUARDS=true",
		"MCP_GATEWAY_GUARDS_MODE=propagate",
		"MCP_GATEWAY_WASM_GUARDS_DIR=",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Start()
	require.NoError(t, err, "Failed to start gateway")

	time.Sleep(1 * time.Second)

	cmd.Process.Kill()
	cmd.Wait()

	stderrStr := stderr.String()
	t.Logf("STDERR: %s", stderrStr)

	// Verify propagate mode is active
	assert.Contains(t, stderrStr, "propagate", "Propagate mode should be logged")
	assert.Contains(t, stderrStr, "Guards enforcement enabled", "Guards should be enabled")
	t.Log("✓ Guards propagate mode enabled via MCP_GATEWAY_GUARDS_MODE=propagate")
}

// TestSessionLabelsRequireConfigExtensions verifies that session labels require config extensions
func TestSessionLabelsRequireConfigExtensions(t *testing.T) {
	binary := binaryPath(t)

	config := `{
		"mcpServers": {
			"test": {
				"type": "stdio",
				"container": "test/echo:latest"
			}
		},
		"gateway": {
			"port": 13304,
			"domain": "localhost",
			"apiKey": "test-key"
		}
	}`

	ctx4, cancel4 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel4()

	cmd := exec.CommandContext(ctx4, binary, "--config-stdin")
	cmd.Stdin = strings.NewReader(config)
	// Note: NOT setting MCP_GATEWAY_CONFIG_EXTENSIONS
	cmd.Env = append(os.Environ(),
		"MCP_GATEWAY_SESSION_SECRECY=secret",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Should fail because session labels require config extensions
	assert.Error(t, err, "Should fail when session labels set without config extensions")

	stderrStr := stderr.String()
	assert.Contains(t, stderrStr, "require --enable-config-extensions", "Error message should mention config extensions requirement")
	t.Log("✓ Session labels correctly require --enable-config-extensions")
}

// TestFullDIFCConfigFromJSON tests complete guards configuration from JSON
func TestFullDIFCConfigFromJSON(t *testing.T) {
	binary := binaryPath(t)

	config := `{
		"mcpServers": {
			"server1": {
				"type": "stdio",
				"container": "test/server1:latest",
				"guard": "guard1"
			},
			"server2": {
				"type": "http",
				"url": "http://localhost:9999",
				"guard": "guard2"
			}
		},
		"guards": {
			"guard1": {
				"type": "wasm",
				"path": "/guards/guard1.wasm"
			},
			"guard2": {
				"type": "noop"
			}
		},
		"gateway": {
			"port": 13305,
			"domain": "localhost",
			"apiKey": "test-key",
			"session": {
				"secrecy": ["internal", "secret"],
				"integrity": ["trusted"]
			}
		}
	}`

	ctx5, cancel5 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel5()

	cmd := exec.CommandContext(ctx5, binary, "--config-stdin", "--enable-config-extensions")
	cmd.Stdin = strings.NewReader(config)
	cmd.Env = append(os.Environ(),
		"MCP_GATEWAY_ENABLE_GUARDS=true",
		"MCP_GATEWAY_GUARDS_MODE=filter",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Start()
	require.NoError(t, err, "Failed to start gateway")

	time.Sleep(1 * time.Second)

	// Try health check
	resp, err := http.Get("http://127.0.0.1:13305/health")
	if err == nil {
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var health map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&health)
		t.Logf("Health response: %+v", health)
	}

	cmd.Process.Kill()
	cmd.Wait()

	stderrStr := stderr.String()
	t.Logf("STDERR: %s", stderrStr)

	// Verify configuration was loaded
	assert.Contains(t, stderrStr, "server1", "Should contain server1")
	assert.Contains(t, stderrStr, "server2", "Should contain server2")
	assert.Contains(t, stderrStr, "Guards enforcement enabled", "Guards should be enabled")
	assert.Contains(t, stderrStr, "filter", "Filter mode should be active")
	t.Log("✓ Full guards configuration loaded successfully")
}
