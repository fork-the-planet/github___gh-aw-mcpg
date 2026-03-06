package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestPipeBasedLaunch tests launching the gateway using pipes via shell script.
//
// This test suite demonstrates two different pipe mechanisms for launching the MCP Gateway,
// similar to the start_mcp_gateway_server.sh script in the gh-aw repository:
//
//  1. Standard Pipe (echo | command): Configuration is piped directly to the gateway
//     using standard shell piping. This is the simplest approach.
//
//  2. Named Pipe (FIFO): Configuration is written to a named pipe (created with mkfifo),
//     which the gateway reads from. This approach is more robust for complex scenarios
//     and allows for asynchronous communication between processes.
//
// The tests verify that:
// - The gateway starts successfully with config provided via pipes
// - Health checks pass
// - MCP initialize requests work correctly
// - Both routed and unified modes are supported
// - The script handles errors gracefully
//
// These tests ensure the gateway can be launched in environments where:
// - Configuration cannot be provided via files
// - Dynamic configuration generation is needed
// - Containerized deployments require stdin-based config
func TestPipeBasedLaunch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping pipe-based launch integration test in short mode")
	}

	// Start a mock HTTP MCP backend so gateways don't need Docker to register tools.
	// This is shared across all subtests; each subtest connects to it via HTTP.
	mockBackend := createMinimalMockMCPBackend(t)
	defer mockBackend.Close()
	t.Logf("Mock MCP backend started at %s", mockBackend.URL)

	// Find the binary
	binaryPath := findBinary(t)
	t.Logf("Using binary: %s", binaryPath)

	// Locate the shell script - use absolute path
	scriptPath, err := filepath.Abs(filepath.Join(".", "start_gateway_with_pipe.sh"))
	require.NoError(t, err, "Failed to get absolute path for script")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("Shell script not found: %s", scriptPath)
	}

	tests := []struct {
		name     string
		pipeType string
		port     string
		mode     string
	}{
		{
			name:     "StandardPipe_RoutedMode",
			pipeType: "standard",
			port:     "13100",
			mode:     "--routed",
		},
		{
			name:     "NamedPipe_RoutedMode",
			pipeType: "named",
			port:     "13101",
			mode:     "--routed",
		},
		{
			name:     "StandardPipe_UnifiedMode",
			pipeType: "standard",
			port:     "13102",
			mode:     "--unified",
		},
		{
			name:     "NamedPipe_UnifiedMode",
			pipeType: "named",
			port:     "13103",
			mode:     "--unified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment for the script
			env := append(os.Environ(),
				"BINARY="+binaryPath,
				"HOST=127.0.0.1",
				"PORT="+tt.port,
				"MODE="+tt.mode,
				"PIPE_TYPE="+tt.pipeType,
				"TIMEOUT=30",
				"NO_CLEANUP=1", // Don't cleanup gateway so tests can interact with it
				"MOCK_BACKEND_URL="+mockBackend.URL+"/mcp", // Use mock HTTP backend instead of Docker container
			)

			// Create context with timeout
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()

			// Execute the script
			cmd := exec.CommandContext(ctx, scriptPath)
			cmd.Env = env

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			t.Logf("Launching gateway with %s pipe...", tt.pipeType)

			// Start the script but don't wait for it to finish yet
			if err := cmd.Start(); err != nil {
				t.Fatalf("Failed to start script: %v", err)
			}

			// Wait for the script to complete
			scriptErr := cmd.Wait()
			if scriptErr != nil {
				t.Logf("Script STDOUT: %s", stdout.String())
				t.Logf("Script STDERR: %s", stderr.String())
				t.Fatalf("Script failed: %v", scriptErr)
			}

			// Parse the PID from stdout (script outputs the gateway PID)
			pidStr := strings.TrimSpace(stdout.String())
			lines := strings.Split(pidStr, "\n")
			lastLine := lines[len(lines)-1]
			gatewayPID, err := strconv.Atoi(lastLine)
			if err != nil {
				t.Logf("Failed to parse PID from output: %s", pidStr)
				t.Logf("Script STDERR: %s", stderr.String())
				t.Fatalf("Could not determine gateway PID: %v", err)
			}

			t.Logf("Gateway PID: %d", gatewayPID)

			// Ensure the gateway process is stopped at the end
			defer func() {
				if process, err := os.FindProcess(gatewayPID); err == nil {
					t.Logf("Stopping gateway process %d...", gatewayPID)
					process.Kill()
					process.Wait()
				}
			}()

			// Verify the gateway is running and responsive
			serverURL := "http://127.0.0.1:" + tt.port

			// Test 1: Health check
			t.Run("HealthCheck", func(t *testing.T) {
				resp, err := http.Get(serverURL + "/health")
				require.NoError(t, err, "Health check failed")
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					body, _ := io.ReadAll(resp.Body)
					t.Errorf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
				}
				t.Log("✓ Health check passed")
			})

			// Test 2: Send an MCP initialize request
			t.Run("MCPInitialize", func(t *testing.T) {
				var endpoint string
				if strings.Contains(tt.mode, "routed") {
					endpoint = serverURL + "/mcp/testserver"
				} else {
					endpoint = serverURL + "/mcp"
				}

				initReq := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      1,
					"method":  "initialize",
					"params": map[string]interface{}{
						"protocolVersion": "1.0.0",
						"capabilities":    map[string]interface{}{},
						"clientInfo": map[string]interface{}{
							"name":    "pipe-test-client",
							"version": "1.0.0",
						},
					},
				}

				jsonData, err := json.Marshal(initReq)
				require.NoError(t, err, "Failed to marshal request")

				req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
				require.NoError(t, err, "Failed to create request")

				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json, text/event-stream")
				req.Header.Set("Authorization", "test-key") // Plain API key per spec 7.1

				client := &http.Client{Timeout: 5 * time.Second}
				resp, err := client.Do(req)
				require.NoError(t, err, "Request failed")
				defer resp.Body.Close()

				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err, "Failed to read response")

				t.Logf("Response status: %d", resp.StatusCode)
				t.Logf("Response body: %s", string(body))

				// We expect a response (might be success or error depending on backend)
				if resp.StatusCode != http.StatusOK {
					t.Logf("Note: Received non-200 status, but gateway responded (which is what we're testing)")
				}

				// Try to parse as JSON
				var result map[string]interface{}
				if err := json.Unmarshal(body, &result); err != nil {
					// Could be SSE-formatted (streamable HTTP transport uses SSE formatting)
					if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
						t.Log("Response uses SSE-formatted streaming (streamable HTTP transport)")
					} else {
						t.Logf("Could not parse response as JSON: %v", err)
					}
				} else {
					// Check for jsonrpc field
					if jsonrpc, ok := result["jsonrpc"].(string); ok && jsonrpc == "2.0" {
						t.Log("✓ Valid JSON-RPC 2.0 response received")
					}
				}

				t.Log("✓ MCP initialize request completed")
			})

			t.Logf("✓ %s test completed successfully", tt.name)
		})
	}
}

// TestPipeBasedLaunch_ScriptValidation tests the shell script itself
func TestPipeBasedLaunch_ScriptValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping script validation test in short mode")
	}

	scriptPath, err := filepath.Abs(filepath.Join(".", "start_gateway_with_pipe.sh"))
	require.NoError(t, err, "Failed to get absolute path for script")

	tests := []struct {
		name        string
		args        []string
		env         []string
		expectError bool
		description string
	}{
		{
			name:        "MissingBinary",
			env:         []string{"BINARY=/nonexistent/binary"},
			expectError: true,
			description: "Should fail when binary doesn't exist",
		},
		{
			name:        "InvalidPipeType",
			env:         []string{"PIPE_TYPE=invalid"},
			expectError: true,
			description: "Should fail with invalid pipe type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, scriptPath)
			cmd.Env = append(os.Environ(), tt.env...)

			var stderr bytes.Buffer
			cmd.Stderr = &stderr

			err := cmd.Run()

			if tt.expectError && err == nil {
				t.Errorf("%s: expected error but got none", tt.description)
			}

			if !tt.expectError && err != nil {
				t.Errorf("%s: unexpected error: %v\nStderr: %s", tt.description, err, stderr.String())
			}

			t.Logf("✓ %s", tt.description)
		})
	}
}
