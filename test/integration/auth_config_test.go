package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"
)

// TestOutputConfigWithAuthHeaders tests that the gateway outputs configuration
// with auth headers per spec section 5.4
func TestOutputConfigWithAuthHeaders(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	binaryPath := findBinary(t)
	t.Logf("Using binary: %s", binaryPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use an in-process mock backend to avoid Docker dependency
	mockBackend := createMinimalMockMCPBackend(t)
	defer mockBackend.Close()

	// Prepare config JSON for stdin with API key
	apiKey := "test-secret-key-12345"
	port := 13010
	configJSON := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"echoserver": map[string]interface{}{
				"type": "http",
				"url":  mockBackend.URL + "/mcp",
			},
		},
		"gateway": map[string]interface{}{
			"port":   port,
			"domain": "localhost",
			"apiKey": apiKey,
		},
	}
	configBytes, err := json.Marshal(configJSON)
	require.NoError(t, err, "Failed to marshal config")

	cmd := exec.CommandContext(ctx, binaryPath,
		"--config-stdin",
		"--listen", fmt.Sprintf("127.0.0.1:%d", port),
		"--routed",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdin = bytes.NewReader(configBytes)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	// Wait for server to start
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !waitForServer(t, serverURL+"/health", 10*time.Second) {
		t.Logf("STDOUT: %s", stdout.String())
		t.Logf("STDERR: %s", stderr.String())
		t.Fatal("Server did not start in time")
	}

	t.Log("✓ Server started successfully")

	// Small delay to ensure stdout is written
	time.Sleep(200 * time.Millisecond)

	// Parse the JSON gateway configuration from stdout
	var gatewayConfig map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	if err := decoder.Decode(&gatewayConfig); err != nil {
		t.Fatalf("Failed to parse JSON from stdout: %v\nOutput: %s", err, stdout.String())
	}

	t.Logf("Gateway config output: %+v", gatewayConfig)

	// Test 1: Verify output config structure per spec section 5.4
	t.Run("OutputConfigStructure", func(t *testing.T) {
		mcpServers, ok := gatewayConfig["mcpServers"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected 'mcpServers' field in output")
		}

		echoserver, ok := mcpServers["echoserver"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected 'echoserver' in mcpServers")
		}

		// Verify type is "http"
		if serverType, ok := echoserver["type"].(string); !ok || serverType != "http" {
			t.Errorf("Expected type 'http', got: %v", echoserver["type"])
		}

		// Verify URL format
		url, ok := echoserver["url"].(string)
		if !ok || url == "" {
			t.Errorf("Expected non-empty url, got: %v", echoserver["url"])
		}

		expectedURL := fmt.Sprintf("http://localhost:%d/mcp/echoserver", port)
		assert.Equal(t, expectedURL, url, "url = %q, got: %q")

		// Verify headers object is present per spec section 5.4
		headers, ok := echoserver["headers"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected 'headers' object in server config per spec section 5.4")
		}

		// Verify Authorization header contains API key directly (not Bearer scheme)
		authHeader, ok := headers["Authorization"].(string)
		if !ok {
			t.Fatalf("Expected 'Authorization' header in headers object")
		}

		assert.Equal(t, apiKey, authHeader, "Authorization header = %q, got: %q")

		t.Log("✓ Output config structure is correct per spec section 5.4")
		t.Log("✓ Headers object includes Authorization with API key directly (not Bearer scheme)")
	})

	// Test 2: Verify auth is required (request without auth should fail)
	t.Run("AuthRequired", func(t *testing.T) {
		mcpServers := gatewayConfig["mcpServers"].(map[string]interface{})
		echoserver := mcpServers["echoserver"].(map[string]interface{})
		url := echoserver["url"].(string)

		// Try to make request without auth header
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]interface{}{
				"protocolVersion": "1.0.0",
				"capabilities":    map[string]interface{}{},
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		jsonData, _ := json.Marshal(initReq)
		req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		// Deliberately omit Authorization header

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err, "Request failed")
		defer resp.Body.Close()

		// Should get 401 Unauthorized
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401 Unauthorized without auth, got %d", resp.StatusCode)
		}

		t.Log("✓ Auth is properly required (401 without Authorization header)")
	})

	// Test 3: Verify auth with correct header format succeeds (authentication only, not server functionality)
	t.Run("AuthWithCorrectHeader", func(t *testing.T) {
		mcpServers := gatewayConfig["mcpServers"].(map[string]interface{})
		echoserver := mcpServers["echoserver"].(map[string]interface{})
		url := echoserver["url"].(string)
		headers := echoserver["headers"].(map[string]interface{})
		authHeader := headers["Authorization"].(string)

		// Try to make request with auth header from output config
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]interface{}{
				"protocolVersion": "1.0.0",
				"capabilities":    map[string]interface{}{},
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		jsonData, _ := json.Marshal(initReq)
		req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		// Use auth header directly per spec section 7.1 (not Bearer scheme)
		req.Header.Set("Authorization", authHeader)

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err, "Request failed")
		defer resp.Body.Close()

		// Should NOT get 401 (auth passed, though server may return error for other reasons)
		if resp.StatusCode == http.StatusUnauthorized {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("Got 401 with correct auth header - auth not working. Body: %s", string(body))
		}

		t.Log("✓ Auth header from output config works (no 401 Unauthorized)")
		t.Log("✓ Successfully used auth header directly per spec section 7.1 (not Bearer scheme)")
	})

	t.Logf("Server output:\nSTDOUT:\n%s\nSTDERR:\n%s", stdout.String(), stderr.String())
}

// TestOutputConfigUnifiedMode tests auth headers in unified mode
func TestOutputConfigUnifiedMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	binaryPath := findBinary(t)
	t.Logf("Using binary: %s", binaryPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use in-process mock backends to avoid Docker dependency
	mockBackend1 := createMinimalMockMCPBackend(t)
	defer mockBackend1.Close()
	mockBackend2 := createMinimalMockMCPBackend(t)
	defer mockBackend2.Close()

	// Prepare config JSON for stdin with API key
	apiKey := "unified-test-key"
	port := 13011
	configJSON := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"server1": map[string]interface{}{
				"type": "http",
				"url":  mockBackend1.URL + "/mcp",
			},
			"server2": map[string]interface{}{
				"type": "http",
				"url":  mockBackend2.URL + "/mcp",
			},
		},
		"gateway": map[string]interface{}{
			"port":   port,
			"domain": "localhost",
			"apiKey": apiKey,
		},
	}
	configBytes, err := json.Marshal(configJSON)
	require.NoError(t, err, "Failed to marshal config")

	cmd := exec.CommandContext(ctx, binaryPath,
		"--config-stdin",
		"--listen", fmt.Sprintf("127.0.0.1:%d", port),
		"--unified",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdin = bytes.NewReader(configBytes)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	// Wait for server to start
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !waitForServer(t, serverURL+"/health", 10*time.Second) {
		t.Logf("STDOUT: %s", stdout.String())
		t.Logf("STDERR: %s", stderr.String())
		t.Fatal("Server did not start in time")
	}

	t.Log("✓ Server started in unified mode")

	// Small delay to ensure stdout is written
	time.Sleep(200 * time.Millisecond)

	// Parse the JSON gateway configuration from stdout
	var gatewayConfig map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	if err := decoder.Decode(&gatewayConfig); err != nil {
		t.Fatalf("Failed to parse JSON from stdout: %v\nOutput: %s", err, stdout.String())
	}

	// Verify all servers have auth headers
	mcpServers, ok := gatewayConfig["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected 'mcpServers' field in output")
	}

	for serverName, serverConfig := range mcpServers {
		config := serverConfig.(map[string]interface{})

		// Verify headers object is present
		headers, ok := config["headers"].(map[string]interface{})
		if !ok {
			t.Errorf("Server %s missing 'headers' object", serverName)
			continue
		}

		// Verify Authorization header
		authHeader, ok := headers["Authorization"].(string)
		if !ok || authHeader != apiKey {
			t.Errorf("Server %s Authorization header = %q, want %q", serverName, authHeader, apiKey)
		}
	}

	t.Log("✓ All servers have correct auth headers in unified mode")
	t.Logf("Server output:\nSTDOUT:\n%s\nSTDERR:\n%s", stdout.String(), stderr.String())
}
