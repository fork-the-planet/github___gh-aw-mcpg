package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/config"
)

// errWriter is an io.Writer that always returns an error, used to test error propagation.
type errWriter struct{}

func (errWriter) Write(_ []byte) (int, error) {
	return 0, fmt.Errorf("write error")
}

func TestWriteGatewayConfigToStdout(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		listenAddr string
		mode       string
		wantHost   string
		wantPort   string
		wantAPIKey string
	}{
		{
			name: "routed mode with single server",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"github": {
						Command: "docker",
						Args:    []string{"run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"},
					},
				},
				Gateway: &config.GatewayConfig{
					APIKey: "test-api-key",
				},
			},
			listenAddr: "127.0.0.1:8080",
			mode:       "routed",
			wantHost:   "127.0.0.1",
			wantPort:   "8080",
			wantAPIKey: "test-api-key",
		},
		{
			name: "unified mode with multiple servers and wildcard bind",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"github": {
						Command: "docker",
					},
					"fetch": {
						Command: "docker",
					},
				},
				Gateway: &config.GatewayConfig{
					APIKey: "unified-api-key",
				},
			},
			listenAddr: "0.0.0.0:3000",
			mode:       "unified",
			wantHost:   "127.0.0.1",
			wantPort:   "3000",
			wantAPIKey: "unified-api-key",
		},
		{
			name: "default port when address has no port",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"test": {
						Command: "echo",
					},
				},
			},
			listenAddr: "localhost",
			mode:       "routed",
			wantHost:   "127.0.0.1",
			wantPort:   "3000",
			wantAPIKey: "",
		},
		{
			name: "IPv6 address with port",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"test": {
						Command: "echo",
					},
				},
			},
			listenAddr: "[::1]:8080",
			mode:       "routed",
			wantHost:   "::1",
			wantPort:   "8080",
			wantAPIKey: "",
		},
		{
			name: "IPv6 address with full notation",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"github": {
						Command: "docker",
					},
				},
				Gateway: &config.GatewayConfig{
					APIKey: "ipv6-key",
				},
			},
			listenAddr: "[2001:db8::1]:3000",
			mode:       "unified",
			wantHost:   "2001:db8::1",
			wantPort:   "3000",
			wantAPIKey: "ipv6-key",
		},
		{
			name: "domain config overrides listen host",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"github": {Command: "docker"},
				},
				Gateway: &config.GatewayConfig{
					APIKey: "domain-key",
					Domain: "my-gateway.local",
				},
			},
			listenAddr: "0.0.0.0:8080",
			mode:       "routed",
			wantHost:   "my-gateway.local",
			wantPort:   "8080",
			wantAPIKey: "domain-key",
		},
		{
			name: "IPv6 wildcard mapped to 127.0.0.1",
			cfg: &config.Config{
				Servers: map[string]*config.ServerConfig{
					"test": {Command: "echo"},
				},
			},
			listenAddr: "[::]:9090",
			mode:       "routed",
			wantHost:   "127.0.0.1",
			wantPort:   "9090",
			wantAPIKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := writeGatewayConfig(tt.cfg, tt.listenAddr, tt.mode, &buf)
			require.NoError(t, err)

			var result map[string]interface{}
			require.NoError(t, json.Unmarshal(buf.Bytes(), &result), "Output should be valid JSON")

			mcpServers, ok := result["mcpServers"].(map[string]interface{})
			require.True(t, ok, "Output should have mcpServers field of correct type")
			assert.Len(t, mcpServers, len(tt.cfg.Servers))

			for serverName := range tt.cfg.Servers {
				t.Run("server:"+serverName, func(t *testing.T) {
					serverConfig, ok := mcpServers[serverName].(map[string]interface{})
					require.True(t, ok, "Server '%s' should exist and be a map", serverName)

					assert.Equal(t, "http", serverConfig["type"])

					url, ok := serverConfig["url"].(string)
					require.True(t, ok, "Server '%s' url should be a string", serverName)

					expectedBase := "http://" + tt.wantHost + ":" + tt.wantPort + "/mcp"
					if tt.mode == "routed" {
						assert.Equal(t, expectedBase+"/"+serverName, url)
					} else {
						assert.Equal(t, expectedBase, url)
					}

					// Verify auth headers per MCP Gateway Specification Section 5.4
					if tt.wantAPIKey != "" {
						headers, ok := serverConfig["headers"].(map[string]interface{})
						require.True(t, ok, "Server '%s' should have headers when API key is configured", serverName)
						assert.Equal(t, tt.wantAPIKey, headers["Authorization"])
					} else {
						assert.Nil(t, serverConfig["headers"], "Server '%s' should not have headers when no API key", serverName)
					}
				})
			}
		})
	}
}

func TestWriteGatewayConfigToStdout_EmptyConfig(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{},
	}

	var buf bytes.Buffer
	err := writeGatewayConfig(cfg, "127.0.0.1:8080", "routed", &buf)
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result), "Output should be valid JSON")

	mcpServers, ok := result["mcpServers"].(map[string]interface{})
	require.True(t, ok, "Output should have mcpServers field")
	assert.Empty(t, mcpServers, "mcpServers should be empty")
}

func TestWriteGatewayConfigToStdout_JSONFormat(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"test": {
				Command: "echo",
			},
		},
	}

	var buf bytes.Buffer
	err := writeGatewayConfig(cfg, "localhost:3000", "routed", &buf)
	require.NoError(t, err)

	// Verify it's valid JSON
	var result interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result), "Output should be valid JSON")

	// Verify output is pretty-printed (contains newlines)
	assert.Contains(t, buf.String(), "\n", "Output should be pretty-printed with indentation")
}

func TestWriteGatewayConfigToStdout_WithPipe(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {
				Command: "docker",
				Args:    []string{"run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"},
			},
		},
	}

	// Create a pipe (simulates writing to /dev/stdout in containerized environment)
	r, w, err := os.Pipe()
	require.NoError(t, err, "Failed to create pipe")
	defer r.Close()
	defer w.Close()

	// Write configuration to pipe in a goroutine
	errCh := make(chan error, 1)
	go func() {
		writeErr := writeGatewayConfig(cfg, "127.0.0.1:3000", "unified", w)
		w.Close() // Close writer to signal EOF
		errCh <- writeErr
	}()

	// Read from pipe
	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err, "Failed to read from pipe")

	// Check for errors from write operation
	require.NoError(t, <-errCh)

	// Verify output is valid JSON
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result), "Output should be valid JSON")

	// Verify structure
	mcpServers, ok := result["mcpServers"].(map[string]interface{})
	require.True(t, ok, "Output should have mcpServers field")
	assert.Contains(t, mcpServers, "github", "github server should be present in output")
}

// TestWriteGatewayConfig_WriteError tests that encoding errors are propagated correctly.
// This covers the error return path of writeGatewayConfig when the writer fails.
func TestWriteGatewayConfig_WriteError(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"test": {Command: "echo"},
		},
	}

	err := writeGatewayConfig(cfg, "127.0.0.1:8080", "routed", errWriter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to encode configuration")
}

// TestWriteGatewayConfig_PortOnlyAddress tests that a ":port" style address
// (where host is empty after parsing) falls back to the default host.
func TestWriteGatewayConfig_PortOnlyAddress(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"test": {Command: "echo"},
		},
	}

	var buf bytes.Buffer
	err := writeGatewayConfig(cfg, ":8080", "unified", &buf)
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))

	mcpServers, ok := result["mcpServers"].(map[string]interface{})
	require.True(t, ok)

	serverConfig, ok := mcpServers["test"].(map[string]interface{})
	require.True(t, ok)

	url, ok := serverConfig["url"].(string)
	require.True(t, ok)

	// Empty host from SplitHostPort should fall back to DefaultListenIPv4
	assert.Contains(t, url, DefaultListenIPv4, "Should use default IPv4 host when address has no host")
	assert.Contains(t, url, "8080", "Should preserve the port")
}
