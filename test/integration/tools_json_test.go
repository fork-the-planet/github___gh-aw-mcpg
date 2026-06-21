package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startGatewayWithJSONConfigAndLogDir starts the gateway with JSON config and custom log directory
func startGatewayWithJSONConfigAndLogDir(ctx context.Context, t *testing.T, jsonConfig string, logDir string) *exec.Cmd {
	t.Helper()

	// Find the binary
	binaryPath := findBinary(t)
	t.Logf("Using binary: %s", binaryPath)

	// Extract port from config if possible, otherwise use default
	port := "13120" // Default port
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(jsonConfig), &configMap); err == nil {
		if gateway, ok := configMap["gateway"].(map[string]interface{}); ok {
			if portNum, ok := gateway["port"].(float64); ok {
				port = fmt.Sprintf("%d", int(portNum))
			}
		}
	}

	cmd := exec.CommandContext(ctx, binaryPath,
		"--config-stdin",
		"--listen", "127.0.0.1:"+port,
		"--log-dir", logDir,
		"--routed",
	)

	// Set stdin to the JSON config
	cmd.Stdin = bytes.NewBufferString(jsonConfig)

	// Capture output for debugging
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start gateway: %v\nSTDOUT: %s\nSTDERR: %s", err, stdout.String(), stderr.String())
	}

	// Start a goroutine to log output if test fails
	go func() {
		<-ctx.Done()
		if t.Failed() {
			t.Logf("Gateway STDOUT: %s", stdout.String())
			t.Logf("Gateway STDERR: %s", stderr.String())
		}
	}()

	return cmd
}

// TestToolsJSONLogging tests that tools.json is created and populated correctly
func TestToolsJSONLogging(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping tools.json integration test in short mode")
	}

	assert := assert.New(t)
	require := require.New(t)

	// Create a mock MCP backend that returns tools
	mockBackend := createMockToolsServer(t)
	defer mockBackend.Close()

	t.Logf("✓ Mock MCP backend started at %s", mockBackend.URL)

	// Create a temporary directory for logs
	tmpDir := t.TempDir()

	// Create JSON config for the gateway
	configContent := fmt.Sprintf(`{
  "mcpServers": {
    "test-server": {
      "type": "http",
      "url": "%s"
    },
    "another-server": {
      "type": "http",
      "url": "%s"
    }
  },
  "gateway": {
    "port": 13120,
    "domain": "localhost",
    "agentId": "test-tools-key"
  }
}`, mockBackend.URL, mockBackend.URL)

	t.Logf("✓ Created config with log directory: %s", tmpDir)

	// Start the gateway with the config via stdin
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start gateway with JSON config via stdin and custom log directory
	gatewayCmd := startGatewayWithJSONConfigAndLogDir(ctx, t, configContent, tmpDir)
	defer gatewayCmd.Process.Kill()

	// Wait for gateway to start
	gatewayURL := "http://127.0.0.1:13120"
	if !waitForServer(t, gatewayURL+"/health", 15*time.Second) {
		t.Fatal("Gateway did not start in time")
	}
	t.Logf("✓ Gateway started at %s", gatewayURL)

	// Give the gateway time to initialize and register tools
	time.Sleep(2 * time.Second)

	// Check that tools.json was created
	toolsPath := filepath.Join(tmpDir, "tools.json")
	toolsData, err := os.ReadFile(toolsPath)
	require.NoError(err, "tools.json should exist")
	t.Logf("✓ tools.json found at %s", toolsPath)

	// Parse the tools.json file
	var tools struct {
		Servers map[string][]struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"servers"`
	}
	err = json.Unmarshal(toolsData, &tools)
	require.NoError(err, "tools.json should be valid JSON")

	// Verify both servers are present
	assert.Contains(tools.Servers, "test-server", "test-server should be in tools.json")
	assert.Contains(tools.Servers, "another-server", "another-server should be in tools.json")

	// Verify test-server has the expected tools
	testServerTools := tools.Servers["test-server"]
	require.Len(testServerTools, 3, "test-server should have 3 tools")

	toolNames := make(map[string]string)
	for _, tool := range testServerTools {
		toolNames[tool.Name] = tool.Description
	}

	assert.Contains(toolNames, "tool1", "tool1 should be present")
	assert.Contains(toolNames, "tool2", "tool2 should be present")
	assert.Contains(toolNames, "tool3", "tool3 should be present")
	assert.Equal("First test tool", toolNames["tool1"])
	assert.Equal("Second test tool", toolNames["tool2"])
	assert.Equal("Third test tool", toolNames["tool3"])

	t.Logf("✓ tools.json contains correct data")
}

// createMockToolsServer creates a mock HTTP MCP server that returns tools
func createMockToolsServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Logf("Failed to read request body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Parse the JSON-RPC request
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Logf("Failed to parse request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Handle different methods
		switch req.Method {
		case "initialize":
			response := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]interface{}{
						"name":    "mock-server",
						"version": "1.0.0",
					},
					"capabilities": map[string]interface{}{
						"tools": map[string]interface{}{},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case "tools/list":
			response := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"tools": []map[string]interface{}{
						{
							"name":        "tool1",
							"description": "First test tool",
							"inputSchema": map[string]interface{}{
								"type":       "object",
								"properties": map[string]interface{}{},
							},
						},
						{
							"name":        "tool2",
							"description": "Second test tool",
							"inputSchema": map[string]interface{}{
								"type":       "object",
								"properties": map[string]interface{}{},
							},
						},
						{
							"name":        "tool3",
							"description": "Third test tool",
							"inputSchema": map[string]interface{}{
								"type":       "object",
								"properties": map[string]interface{}{},
							},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		default:
			// Return success for any other method
			response := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  map[string]interface{}{},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}
	}))
}
