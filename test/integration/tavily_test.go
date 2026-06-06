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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startGatewayWithJSONConfig starts the gateway with JSON config via stdin
func startGatewayWithJSONConfig(ctx context.Context, t *testing.T, jsonConfig string) *exec.Cmd {
	t.Helper()

	// Find the binary
	binaryPath := findBinary(t)
	t.Logf("Using binary: %s", binaryPath)

	// Extract port from config if possible, otherwise use default
	port := "13099" // Default port
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

// waitForGateway waits for the gateway to start and returns its URL
func waitForGateway(t *testing.T, ctx context.Context) string {
	t.Helper()

	serverURL := "http://127.0.0.1:13099"
	if !waitForServer(t, serverURL+"/health", 15*time.Second) {
		t.Fatal("Gateway did not start in time")
	}
	return serverURL
}

// TestTavilyHTTPBackend tests connecting to a Tavily-like HTTP backend
// that returns SSE-formatted responses
func TestTavilyHTTPBackend(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a mock Tavily MCP backend that returns SSE-formatted responses
	tavilyBackend := createTavilyMockServer(t)
	defer tavilyBackend.Close()

	t.Logf("✓ Mock Tavily backend started at %s", tavilyBackend.URL)

	// Create JSON config for the gateway (including required gateway section with apiKey)
	configContent := `{
  "mcpServers": {
    "tavily": {
      "type": "http",
      "url": "` + tavilyBackend.URL + `"
    }
  },
  "gateway": {
    "port": 13099,
    "domain": "localhost",
    "agentId": "test-api-key"
  }
}`

	t.Logf("✓ Created config: %s", configContent)

	// Start the gateway with the config via stdin
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start gateway with JSON config via stdin
	gatewayCmd := startGatewayWithJSONConfig(ctx, t, configContent)
	defer gatewayCmd.Process.Kill()

	// Wait for gateway to start
	gatewayURL := waitForGateway(t, ctx)
	t.Logf("✓ Gateway started at %s", gatewayURL)

	// Test 1: Health check should show tavily backend
	t.Run("HealthCheck", func(t *testing.T) {
		resp, err := http.Get(gatewayURL + "/health")
		require.NoError(t, err, "Health check failed")
		defer resp.Body.Close()

		var health map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&health)
		require.NoError(t, err, "Failed to decode health response")

		assert.Equal(t, "healthy", health["status"])
		servers := health["servers"].(map[string]interface{})
		assert.Contains(t, servers, "tavily", "Tavily backend not found in health check")

		t.Log("✓ Health check passed - Tavily backend registered")
	})

	// Test 2: Initialize connection to Tavily backend
	t.Run("InitializeConnection", func(t *testing.T) {
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{},
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		jsonData, _ := json.Marshal(initReq)
		req, _ := http.NewRequest("POST", gatewayURL+"/mcp/tavily", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "test-api-key") // Use the gateway API key

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err, "Initialize request failed")
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		t.Logf("Initialize response: %s", string(body))

		require.Equal(t, http.StatusOK, resp.StatusCode, "Initialize failed with status %d: %s", resp.StatusCode, string(body))

		t.Log("✓ Successfully initialized connection to Tavily backend")
	})

	// Test 3: List tools from Tavily backend
	t.Run("ListTools", func(t *testing.T) {
		toolsReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/list",
			"params":  map[string]interface{}{},
		}

		jsonData, _ := json.Marshal(toolsReq)
		req, _ := http.NewRequest("POST", gatewayURL+"/mcp/tavily", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "test-api-key") // Use the gateway API key

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err, "Tools list request failed")
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		t.Logf("Tools list response: %s", string(body))

		require.Equal(t, http.StatusOK, resp.StatusCode, "Tools list failed with status %d: %s", resp.StatusCode, string(body))

		t.Log("✓ Successfully listed tools from Tavily backend")
	})

	t.Log("✓ Tavily HTTP backend integration test passed")
}

// TestTavilyAuthFailure tests that the gateway provides clear error messages
// when authentication to Tavily fails (missing or invalid API key)
func TestTavilyAuthFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a mock Tavily backend that validates authentication
	authValidatingBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for Authorization header (Tavily requires API key in Authorization header)
		authHeader := r.Header.Get("Authorization")

		if authHeader == "" {
			// Missing API key - return 401 with clear error
			w.WriteHeader(http.StatusUnauthorized)
			response := `{"error": {"type": "authentication_error", "message": "Missing API key. Please provide your Tavily API key in the Authorization header."}}`
			w.Write([]byte(response))
			t.Logf("Backend: Rejected request - missing Authorization header")
			return
		}

		if authHeader != "tvly-test-valid-key-123" {
			// Invalid API key - return 401 with clear error
			w.WriteHeader(http.StatusUnauthorized)
			response := `{"error": {"type": "invalid_request_error", "message": "Invalid API key provided. Please check your Tavily API key."}}`
			w.Write([]byte(response))
			t.Logf("Backend: Rejected request - invalid Authorization: %s", authHeader)
			return
		}

		// Valid key - should not reach here in this test
		w.WriteHeader(http.StatusOK)
	}))
	defer authValidatingBackend.Close()

	t.Logf("✓ Auth-validating Tavily backend started at %s", authValidatingBackend.URL)

	// Test 1: Missing API key configuration
	t.Run("MissingAPIKey", func(t *testing.T) {
		configContent := `{
  "mcpServers": {
    "tavily": {
      "type": "http",
      "url": "` + authValidatingBackend.URL + `"
    }
  },
  "gateway": {
    "port": 13098,
    "domain": "localhost",
    "agentId": "test-gateway-key"
  }
}`

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Capture gateway output to check error messages
		binaryPath := findBinary(t)
		cmd := exec.CommandContext(ctx, binaryPath,
			"--config-stdin",
			"--listen", "127.0.0.1:13098",
			"--routed",
		)
		cmd.Stdin = bytes.NewBufferString(configContent)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Start()
		require.NoError(t, err, "Failed to start gateway")
		defer cmd.Process.Kill()

		// Wait for gateway to start
		gatewayURL := "http://127.0.0.1:13098"
		if !waitForServer(t, gatewayURL+"/health", 15*time.Second) {
			t.Logf("Gateway stderr: %s", stderr.String())
			t.Fatal("Gateway did not start in time")
		}

		t.Logf("✓ Gateway started without Tavily API key")

		// Check gateway logs for clear authentication error message
		time.Sleep(1 * time.Second) // Give time for logs to be written
		stderrOutput := stderr.String()
		t.Logf("Gateway stderr output:\n%s", stderrOutput)

		// Verify error message clearly indicates authentication/API key issue
		assert.True(t,
			containsAny(stderrOutput, []string{
				"Missing API key",
				"authentication_error",
				"status=401",
				"FAILED to create HTTP connection",
			}),
			"Gateway logs should clearly indicate authentication/API key issue")

		assert.Contains(t, stderrOutput, "Missing API key",
			"Gateway should log the exact error from Tavily about missing API key")

		t.Log("✓ Clear error message provided in gateway logs for missing API key")
	})

	// Test 2: Invalid API key configuration
	t.Run("InvalidAPIKey", func(t *testing.T) {
		configContent := `{
  "mcpServers": {
    "tavily": {
      "type": "http",
      "url": "` + authValidatingBackend.URL + `",
      "headers": {
        "Authorization": "tvly-invalid-key-wrong"
      }
    }
  },
  "gateway": {
    "port": 13097,
    "domain": "localhost",
    "agentId": "test-gateway-key"
  }
}`

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Capture gateway output to check error messages
		binaryPath := findBinary(t)
		cmd := exec.CommandContext(ctx, binaryPath,
			"--config-stdin",
			"--listen", "127.0.0.1:13097",
			"--routed",
		)
		cmd.Stdin = bytes.NewBufferString(configContent)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Start()
		require.NoError(t, err, "Failed to start gateway")
		defer cmd.Process.Kill()

		// Wait for gateway to start
		gatewayURL := "http://127.0.0.1:13097"
		if !waitForServer(t, gatewayURL+"/health", 15*time.Second) {
			t.Logf("Gateway stderr: %s", stderr.String())
			t.Fatal("Gateway did not start in time")
		}

		t.Logf("✓ Gateway started with invalid Tavily API key")

		// Check gateway logs for clear authentication error message
		time.Sleep(1 * time.Second) // Give time for logs to be written
		stderrOutput := stderr.String()
		t.Logf("Gateway stderr output:\n%s", stderrOutput)

		// Verify error message clearly indicates invalid API key issue
		assert.True(t,
			containsAny(stderrOutput, []string{
				"Invalid API key",
				"invalid_request_error",
				"status=401",
				"FAILED to create HTTP connection",
			}),
			"Gateway logs should clearly indicate invalid API key issue")

		assert.Contains(t, stderrOutput, "Invalid API key",
			"Gateway should log the exact error from Tavily about invalid API key")

		t.Log("✓ Clear error message provided in gateway logs for invalid API key")
	})

	t.Log("✓ Tavily auth failure tests passed - errors are clearly communicated")
}

// containsAny checks if a string contains any of the given substrings
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// createTavilyMockServer creates a mock server that mimics Tavily's SSE response format
func createTavilyMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read request body
		var reqBody map[string]interface{}
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		// Ignore empty requests (e.g., from health checks)
		if len(bodyBytes) == 0 {
			w.WriteHeader(http.StatusOK)
			return
		}

		if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
			// Ignore unmarshal errors for non-JSON requests
			w.WriteHeader(http.StatusOK)
			return
		}

		method, _ := reqBody["method"].(string)
		id := reqBody["id"]

		var response string
		switch method {
		case "initialize":
			// Return SSE-formatted initialize response (like real Tavily)
			response = `event: message
data: {"jsonrpc":"2.0","id":` + jsonNumber(id) + `,"result":{"protocolVersion":"2024-11-05","capabilities":{"experimental":{},"prompts":{"listChanged":true},"resources":{"subscribe":false,"listChanged":true},"tools":{"listChanged":true}},"serverInfo":{"name":"tavily-mcp","version":"2.14.2"}}}

`
		case "tools/list":
			// Return SSE-formatted tools list response
			response = `event: message
data: {"jsonrpc":"2.0","id":` + jsonNumber(id) + `,"result":{"tools":[{"name":"search_web","description":"Search the web using Tavily","inputSchema":{"type":"object","properties":{"query":{"type":"string","description":"Search query"}},"required":["query"]}}]}}

`
		default:
			// Generic SSE-formatted response
			response = `event: message
data: {"jsonrpc":"2.0","id":` + jsonNumber(id) + `,"result":{}}

`
		}

		// Set headers to indicate SSE format
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	}))
}

// jsonNumber converts an interface{} to a JSON number string
func jsonNumber(v interface{}) string {
	switch n := v.(type) {
	case float64:
		return json.Number(string(rune(int(n) + '0'))).String()
	case int:
		return json.Number(string(rune(n + '0'))).String()
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}

// TestRealTavilyConnection tests connection to the actual Tavily MCP server
// This test requires TAVILY_API_KEY environment variable to be set
func TestRealTavilyConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real Tavily connection test in short mode")
	}

	apiKey := os.Getenv("TAVILY_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping real Tavily test: TAVILY_API_KEY not set")
	}

	// Create JSON config for the gateway (including required gateway section with apiKey)
	configContent := `{
  "mcpServers": {
    "tavily": {
      "type": "http",
      "url": "https://mcp.tavily.com/mcp/",
      "headers": {
        "Authorization": "` + apiKey + `"
      }
    }
  },
  "gateway": {
    "port": 13099,
    "domain": "localhost",
    "agentId": "test-api-key"
  }
}`

	t.Logf("✓ Created config for real Tavily connection")

	// Start the gateway with the config via stdin
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Start gateway with JSON config via stdin
	gatewayCmd := startGatewayWithJSONConfig(ctx, t, configContent)
	defer gatewayCmd.Process.Kill()

	// Wait for gateway to start
	gatewayURL := waitForGateway(t, ctx)
	t.Logf("✓ Gateway started at %s", gatewayURL)

	// Test 1: Health check
	t.Run("HealthCheck", func(t *testing.T) {
		resp, err := http.Get(gatewayURL + "/health")
		require.NoError(t, err, "Health check failed")
		defer resp.Body.Close()

		var health map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&health)
		require.NoError(t, err, "Failed to decode health response")

		assert.Equal(t, "healthy", health["status"])
		servers := health["servers"].(map[string]interface{})
		assert.Contains(t, servers, "tavily", "Tavily backend not found in health check")

		t.Log("✓ Health check passed - Real Tavily backend registered")
	})

	// Test 2: Initialize connection
	t.Run("InitializeConnection", func(t *testing.T) {
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{},
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		jsonData, _ := json.Marshal(initReq)
		req, _ := http.NewRequest("POST", gatewayURL+"/mcp/tavily", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "test-api-key") // Use the gateway API key

		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err, "Initialize request failed")
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		t.Logf("Initialize response: %s", string(body))

		require.Equal(t, http.StatusOK, resp.StatusCode, "Initialize failed with status %d: %s", resp.StatusCode, string(body))

		t.Log("✓ Successfully initialized connection to real Tavily backend")
	})

	t.Log("✓ Real Tavily connection test passed")
}
