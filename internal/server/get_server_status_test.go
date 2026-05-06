package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/config"
)

// mockMCPServer creates a minimal HTTP MCP server that responds to JSON-RPC
// initialize and tools/list requests, enabling HTTP-backend tests without Docker.
func mockMCPServer(tools []map[string]interface{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, id := decodeJSONRPCMethod(r)
		if method == "" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]interface{}{},
					"serverInfo":      map[string]interface{}{"name": "mock-server", "version": "1.0.0"},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  map[string]interface{}{"tools": tools},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestGetServerStatus_EmptyConfig verifies that no servers in config returns an empty map.
func TestGetServerStatus_EmptyConfig(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	t.Cleanup(func() { us.Close() })

	status := us.GetServerStatus()
	assert.Empty(status, "GetServerStatus should return an empty map when no servers are configured")
}

// TestGetServerStatus_RunningServer verifies that a successfully-started HTTP backend
// reports status "running" and a non-negative uptime (the !StartedAt.IsZero() branch).
func TestGetServerStatus_RunningServer(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	srv := mockMCPServer([]map[string]interface{}{
		{"name": "test_tool", "description": "A test tool", "inputSchema": map[string]interface{}{"type": "object"}},
	})
	defer srv.Close()

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"http-backend": {
				Type: "http",
				URL:  srv.URL,
			},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err, "NewUnified should succeed with a healthy HTTP backend")
	t.Cleanup(func() { us.Close() })

	status := us.GetServerStatus()
	require.Contains(status, "http-backend")

	s := status["http-backend"]
	assert.Equal("running", s.Status, "Successfully started HTTP backend should report status 'running'")
	assert.GreaterOrEqual(s.Uptime, 0, "Uptime must be >= 0 for a running server")
}

// TestGetServerStatus_ErrorServer verifies that a server that failed to start reports
// status "error" with uptime 0 (StartedAt is zero).
func TestGetServerStatus_ErrorServer(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			// Use a definitely non-existent executable so startup fails immediately and
			// deterministically without depending on Docker or runner configuration.
			"failing-server": {Type: "stdio", Command: "gh-aw-mcpg-test-nonexistent-binary", Args: []string{}},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err, "NewUnified must not error on partial backend failures")
	t.Cleanup(func() { us.Close() })

	status := us.GetServerStatus()
	require.Contains(status, "failing-server")

	s := status["failing-server"]
	assert.Equal("error", s.Status, "Failed server should report status 'error'")
	assert.Equal(0, s.Uptime, "Uptime should be 0 when StartedAt is zero (server never started)")
}

// TestGetServerStatus_MultipleServers verifies mixed-state status maps: one running server
// and one errored server are reported independently.
func TestGetServerStatus_MultipleServers(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	srv := mockMCPServer(nil)
	defer srv.Close()

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"ok-backend": {
				Type: "http",
				URL:  srv.URL,
			},
			"bad-backend": {Type: "stdio", Command: "docker", Args: []string{"run", "nonexistent:image"}},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	t.Cleanup(func() { us.Close() })

	status := us.GetServerStatus()
	require.Contains(status, "ok-backend")
	require.Contains(status, "bad-backend")

	assert.Equal("running", status["ok-backend"].Status, "HTTP backend should be running")
	assert.GreaterOrEqual(status["ok-backend"].Uptime, 0, "Running server uptime must be non-negative")

	assert.Equal("error", status["bad-backend"].Status, "Failing backend should be in error state")
	assert.Equal(0, status["bad-backend"].Uptime, "Error server uptime should be 0")
}

// TestGetServerStatus_NilServersField verifies GetServerStatus handles a unified server
// with a fully empty config (nil Servers field) gracefully.
func TestGetServerStatus_NilServersField(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	us, err := NewUnified(context.Background(), &config.Config{})
	require.NoError(err)
	t.Cleanup(func() { us.Close() })

	status := us.GetServerStatus()
	assert.NotNil(status, "GetServerStatus should return a non-nil map even with empty config")
	assert.Empty(status, "No servers configured means empty status map")
}

// TestGetServerIDs_MatchesConfig verifies that GetServerIDs returns exactly the keys
// from the configuration Servers map.
func TestGetServerIDs_MatchesConfig(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	srv := mockMCPServer(nil)
	defer srv.Close()

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"alpha": {Type: "http", URL: srv.URL},
			"beta":  {Type: "stdio", Command: "docker", Args: []string{"run", "nope"}},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	t.Cleanup(func() { us.Close() })

	ids := us.GetServerIDs()
	assert.ElementsMatch([]string{"alpha", "beta"}, ids, "GetServerIDs must return all configured server IDs")
}

// TestGetServerStatus_Keys_EqualGetServerIDs ensures GetServerStatus returns the same
// server set as GetServerIDs.
func TestGetServerStatus_Keys_EqualGetServerIDs(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	srv := mockMCPServer(nil)
	defer srv.Close()

	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"x": {Type: "http", URL: srv.URL},
			"y": {Type: "stdio", Command: "docker", Args: []string{"run", "nope"}},
		},
	}

	us, err := NewUnified(context.Background(), cfg)
	require.NoError(err)
	t.Cleanup(func() { us.Close() })

	ids := us.GetServerIDs()
	statusKeys := make([]string, 0)
	for k := range us.GetServerStatus() {
		statusKeys = append(statusKeys, k)
	}

	assert.ElementsMatch(ids, statusKeys, "GetServerStatus keys must equal GetServerIDs output")
}
