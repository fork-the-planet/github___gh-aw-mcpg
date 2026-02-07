package config

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStdinConfigWithGuards tests that the StdinConfig struct parses JSON with guards section
func TestStdinConfigWithGuards(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"github": {
				"type": "stdio",
				"container": "ghcr.io/github/github-mcp-server:latest",
				"env": {
					"GITHUB_PERSONAL_ACCESS_TOKEN": "test-token"
				},
				"guard": "github-guard"
			},
			"playwright": {
				"type": "stdio",
				"container": "mcp/playwright:latest",
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
			"port": 3001,
			"domain": "localhost",
			"apiKey": "test-api-key"
		}
	}`

	var stdinCfg StdinConfig
	err := json.Unmarshal([]byte(jsonConfig), &stdinCfg)
	require.NoError(t, err, "JSON unmarshal failed")

	// Verify servers
	assert.Len(t, stdinCfg.MCPServers, 2, "Expected 2 servers")

	// Check github server has guard field
	github, ok := stdinCfg.MCPServers["github"]
	require.True(t, ok, "Server 'github' not found")
	assert.Equal(t, "github-guard", github.Guard, "github server should have guard")

	// Check playwright server has no guard
	playwright, ok := stdinCfg.MCPServers["playwright"]
	require.True(t, ok, "Server 'playwright' not found")
	assert.Empty(t, playwright.Guard, "playwright server should have no guard")

	// Check guards section
	require.NotNil(t, stdinCfg.Guards, "Guards should not be nil")
	assert.Len(t, stdinCfg.Guards, 1, "Expected 1 guard")

	guard, ok := stdinCfg.Guards["github-guard"]
	require.True(t, ok, "Guard 'github-guard' not found")
	assert.Equal(t, "wasm", guard.Type, "Guard type should be wasm")
	assert.Equal(t, "/guard/github-guard-rust.wasm", guard.Path, "Guard path mismatch")

	// Check gateway
	assert.Equal(t, 3001, *stdinCfg.Gateway.Port, "Port should be 3001")
	assert.Equal(t, "localhost", stdinCfg.Gateway.Domain, "Domain should be localhost")
	assert.Equal(t, "test-api-key", stdinCfg.Gateway.APIKey, "API key mismatch")
}

// TestStdinConfigWithSessionLabels tests session label configuration parsing
func TestStdinConfigWithSessionLabels(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"test": {
				"type": "stdio",
				"container": "test/container:latest"
			}
		},
		"gateway": {
			"port": 3000,
			"session": {
				"secrecy": ["secret", "confidential"],
				"integrity": ["trusted", "verified"]
			}
		}
	}`

	var stdinCfg StdinConfig
	err := json.Unmarshal([]byte(jsonConfig), &stdinCfg)
	require.NoError(t, err, "JSON unmarshal failed")

	// Verify session config
	require.NotNil(t, stdinCfg.Gateway, "Gateway should not be nil")
	require.NotNil(t, stdinCfg.Gateway.Session, "Session config should not be nil")
	assert.Equal(t, []string{"secret", "confidential"}, stdinCfg.Gateway.Session.Secrecy, "Secrecy labels mismatch")
	assert.Equal(t, []string{"trusted", "verified"}, stdinCfg.Gateway.Session.Integrity, "Integrity labels mismatch")
}

// TestStdinConfigWithSessionLabelsEmpty tests empty session labels parsing
func TestStdinConfigWithSessionLabelsEmpty(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"test": {
				"type": "stdio",
				"container": "test/container:latest"
			}
		},
		"gateway": {
			"port": 3000,
			"session": {
				"secrecy": [],
				"integrity": []
			}
		}
	}`

	var stdinCfg StdinConfig
	err := json.Unmarshal([]byte(jsonConfig), &stdinCfg)
	require.NoError(t, err, "JSON unmarshal failed")

	require.NotNil(t, stdinCfg.Gateway.Session, "Session config should not be nil even if empty")
	assert.Empty(t, stdinCfg.Gateway.Session.Secrecy, "Secrecy should be empty")
	assert.Empty(t, stdinCfg.Gateway.Session.Integrity, "Integrity should be empty")
}

// TestStdinConfigNoSessionConfig tests config without session section
func TestStdinConfigNoSessionConfig(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"test": {
				"type": "stdio",
				"container": "test/container:latest"
			}
		},
		"gateway": {
			"port": 3000
		}
	}`

	var stdinCfg StdinConfig
	err := json.Unmarshal([]byte(jsonConfig), &stdinCfg)
	require.NoError(t, err, "JSON unmarshal failed")

	require.NotNil(t, stdinCfg.Gateway, "Gateway should not be nil")
	assert.Nil(t, stdinCfg.Gateway.Session, "Session should be nil when not configured")
}

// TestStdinConfigMultipleGuards tests multiple guard configurations
func TestStdinConfigMultipleGuards(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"server1": {
				"type": "stdio",
				"container": "test/server1:latest",
				"guard": "guard1"
			},
			"server2": {
				"type": "stdio",
				"container": "test/server2:latest",
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
			"port": 3000
		}
	}`

	var stdinCfg StdinConfig
	err := json.Unmarshal([]byte(jsonConfig), &stdinCfg)
	require.NoError(t, err, "JSON unmarshal failed")

	// Verify both guards
	assert.Len(t, stdinCfg.Guards, 2, "Expected 2 guards")

	guard1, ok := stdinCfg.Guards["guard1"]
	require.True(t, ok, "guard1 not found")
	assert.Equal(t, "wasm", guard1.Type)
	assert.Equal(t, "/guards/guard1.wasm", guard1.Path)

	guard2, ok := stdinCfg.Guards["guard2"]
	require.True(t, ok, "guard2 not found")
	assert.Equal(t, "noop", guard2.Type)
	assert.Empty(t, guard2.Path, "noop guard should have no path")

	// Verify server->guard associations
	assert.Equal(t, "guard1", stdinCfg.MCPServers["server1"].Guard)
	assert.Equal(t, "guard2", stdinCfg.MCPServers["server2"].Guard)
}

// TestStdinConfigGuardWithConfig tests guard with custom configuration
func TestStdinConfigGuardWithConfig(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"test": {
				"type": "stdio",
				"container": "test/container:latest",
				"guard": "custom-guard"
			}
		},
		"guards": {
			"custom-guard": {
				"type": "wasm",
				"path": "/guards/custom.wasm",
				"config": {
					"allowedTools": ["read_file", "write_file"],
					"maxFileSize": 1048576,
					"securityLevel": "high"
				}
			}
		},
		"gateway": {
			"port": 3000
		}
	}`

	var stdinCfg StdinConfig
	err := json.Unmarshal([]byte(jsonConfig), &stdinCfg)
	require.NoError(t, err, "JSON unmarshal failed")

	guard, ok := stdinCfg.Guards["custom-guard"]
	require.True(t, ok, "custom-guard not found")
	require.NotNil(t, guard.Config, "Guard config should not be nil")

	// Check config values
	allowedTools, ok := guard.Config["allowedTools"].([]interface{})
	require.True(t, ok, "allowedTools should be an array")
	assert.Len(t, allowedTools, 2)

	maxFileSize, ok := guard.Config["maxFileSize"].(float64) // JSON numbers are float64
	require.True(t, ok, "maxFileSize should be a number")
	assert.Equal(t, float64(1048576), maxFileSize)

	securityLevel, ok := guard.Config["securityLevel"].(string)
	require.True(t, ok, "securityLevel should be a string")
	assert.Equal(t, "high", securityLevel)
}

// TestStdinConfigHTTPServerWithGuard tests HTTP server with guard
func TestStdinConfigHTTPServerWithGuard(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"api-server": {
				"type": "http",
				"url": "http://localhost:8080",
				"guard": "api-guard"
			}
		},
		"guards": {
			"api-guard": {
				"type": "wasm",
				"path": "/guards/api-guard.wasm"
			}
		},
		"gateway": {
			"port": 3000
		}
	}`

	var stdinCfg StdinConfig
	err := json.Unmarshal([]byte(jsonConfig), &stdinCfg)
	require.NoError(t, err, "JSON unmarshal failed")

	server, ok := stdinCfg.MCPServers["api-server"]
	require.True(t, ok, "api-server not found")
	assert.Equal(t, "http", server.Type)
	assert.Equal(t, "api-guard", server.Guard)
}

// TestConvertStdinConfigWithGuards tests the convertStdinConfig function with guards
func TestConvertStdinConfigWithGuards(t *testing.T) {
	stdinCfg := &StdinConfig{
		MCPServers: map[string]*StdinServerConfig{
			"github": {
				Type:      "stdio",
				Container: "ghcr.io/github/github-mcp-server:latest",
				Guard:     "github-guard",
			},
		},
		Guards: map[string]*StdinGuardConfig{
			"github-guard": {
				Type: "wasm",
				Path: "/guard/github-guard.wasm",
			},
		},
		Gateway: &StdinGatewayConfig{
			Port:   intPtrDIFC(3000),
			Domain: "localhost",
			APIKey: "test-key",
			Session: &StdinSessionConfig{
				Secrecy:   []string{"internal"},
				Integrity: []string{"trusted"},
			},
		},
	}

	cfg, err := convertStdinConfig(stdinCfg)
	require.NoError(t, err, "convertStdinConfig failed")

	// Verify guards were converted
	require.NotNil(t, cfg.Guards, "Guards should not be nil")
	guard, ok := cfg.Guards["github-guard"]
	require.True(t, ok, "github-guard not found")
	assert.Equal(t, "wasm", guard.Type)
	assert.Equal(t, "/guard/github-guard.wasm", guard.Path)

	// Verify server guard reference
	server, ok := cfg.Servers["github"]
	require.True(t, ok, "github server not found")
	assert.Equal(t, "github-guard", server.Guard)

	// Verify session config
	require.NotNil(t, cfg.Gateway.Session, "Session should not be nil")
	assert.Equal(t, []string{"internal"}, cfg.Gateway.Session.Secrecy)
	assert.Equal(t, []string{"trusted"}, cfg.Gateway.Session.Integrity)
}

// TestConvertStdinConfigWithoutGuards tests conversion when no guards are defined
func TestConvertStdinConfigWithoutGuards(t *testing.T) {
	stdinCfg := &StdinConfig{
		MCPServers: map[string]*StdinServerConfig{
			"test": {
				Type:      "stdio",
				Container: "test/container:latest",
			},
		},
		Gateway: &StdinGatewayConfig{
			Port: intPtrDIFC(3000),
		},
	}

	cfg, err := convertStdinConfig(stdinCfg)
	require.NoError(t, err, "convertStdinConfig failed")

	// Guards should be nil or empty when not defined
	assert.Empty(t, cfg.Guards, "Guards should be nil or empty")
}

// TestFullDIFCConfigParsing tests complete DIFC configuration parsing
func TestFullDIFCConfigParsing(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"github": {
				"type": "stdio",
				"container": "ghcr.io/github/github-mcp-server:latest",
				"env": {
					"GITHUB_PERSONAL_ACCESS_TOKEN": "test-token"
				},
				"guard": "github-guard"
			},
			"playwright": {
				"type": "stdio",
				"container": "mcp/playwright:latest",
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
			"port": 3001,
			"domain": "localhost",
			"apiKey": "test-api-key",
			"session": {
				"secrecy": ["internal"],
				"integrity": ["agent"]
			}
		}
	}`

	var stdinCfg StdinConfig
	err := json.Unmarshal([]byte(jsonConfig), &stdinCfg)
	require.NoError(t, err, "JSON unmarshal failed")

	// Convert to internal format
	cfg, err := convertStdinConfig(&stdinCfg)
	require.NoError(t, err, "convertStdinConfig failed")

	// Verify complete configuration
	assert.Len(t, cfg.Servers, 2, "Expected 2 servers")
	assert.Len(t, cfg.Guards, 1, "Expected 1 guard")

	// Verify github server configuration
	github := cfg.Servers["github"]
	assert.Equal(t, "github-guard", github.Guard)

	// Verify guard configuration
	guard := cfg.Guards["github-guard"]
	assert.Equal(t, "wasm", guard.Type)
	assert.Equal(t, "/guard/github-guard-rust.wasm", guard.Path)

	// Verify gateway and session configuration
	assert.Equal(t, 3001, cfg.Gateway.Port)
	assert.Equal(t, "localhost", cfg.Gateway.Domain)
	assert.Equal(t, "test-api-key", cfg.Gateway.APIKey)
	assert.Equal(t, []string{"internal"}, cfg.Gateway.Session.Secrecy)
	assert.Equal(t, []string{"agent"}, cfg.Gateway.Session.Integrity)
}

// TestLoadFromStdin_WithConfigExtensionsEnabled tests that LoadFromStdin accepts
// extension fields (guards, guard, session) when MCP_GATEWAY_CONFIG_EXTENSIONS is set
func TestLoadFromStdin_WithConfigExtensionsEnabled(t *testing.T) {
	// Enable config extensions
	os.Setenv("MCP_GATEWAY_CONFIG_EXTENSIONS", "true")
	defer os.Unsetenv("MCP_GATEWAY_CONFIG_EXTENSIONS")

	jsonConfig := `{
		"mcpServers": {
			"github": {
				"type": "stdio",
				"container": "ghcr.io/github/github-mcp-server:latest",
				"env": {
					"GITHUB_PERSONAL_ACCESS_TOKEN": "test-token"
				},
				"guard": "github-guard"
			}
		},
		"guards": {
			"github-guard": {
				"type": "wasm",
				"path": "/guard/github-guard-rust.wasm"
			}
		},
		"gateway": {
			"port": 3001,
			"domain": "localhost",
			"apiKey": "test-api-key",
			"session": {
				"secrecy": ["internal"],
				"integrity": ["trusted"]
			}
		}
	}`

	// Mock stdin
	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	go func() {
		w.Write([]byte(jsonConfig))
		w.Close()
	}()

	cfg, err := LoadFromStdin()
	os.Stdin = oldStdin

	require.NoError(t, err, "LoadFromStdin() should succeed with config extensions enabled")
	require.NotNil(t, cfg, "Config should not be nil")

	// Verify extension fields were parsed
	assert.Len(t, cfg.Servers, 1, "Expected 1 server")
	assert.Equal(t, "github-guard", cfg.Servers["github"].Guard, "Server guard should be set")

	require.NotNil(t, cfg.Guards, "Guards should not be nil")
	assert.Len(t, cfg.Guards, 1, "Expected 1 guard")
	assert.Equal(t, "wasm", cfg.Guards["github-guard"].Type, "Guard type should be wasm")

	require.NotNil(t, cfg.Gateway.Session, "Session should not be nil")
	assert.Equal(t, []string{"internal"}, cfg.Gateway.Session.Secrecy, "Session secrecy should be set")
	assert.Equal(t, []string{"trusted"}, cfg.Gateway.Session.Integrity, "Session integrity should be set")
}

// TestLoadFromStdin_WithConfigExtensionsDisabled tests that LoadFromStdin rejects
// extension fields when MCP_GATEWAY_CONFIG_EXTENSIONS is not set
func TestLoadFromStdin_WithConfigExtensionsDisabled(t *testing.T) {
	// Ensure config extensions are disabled
	os.Unsetenv("MCP_GATEWAY_CONFIG_EXTENSIONS")

	jsonConfig := `{
		"mcpServers": {
			"github": {
				"type": "stdio",
				"container": "ghcr.io/github/github-mcp-server:latest",
				"guard": "github-guard"
			}
		},
		"guards": {
			"github-guard": {
				"type": "wasm",
				"path": "/guard/github-guard-rust.wasm"
			}
		},
		"gateway": {
			"port": 3001,
			"domain": "localhost",
			"apiKey": "test-api-key"
		}
	}`

	// Mock stdin
	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	go func() {
		w.Write([]byte(jsonConfig))
		w.Close()
	}()

	_, err := LoadFromStdin()
	os.Stdin = oldStdin

	// Should fail because schema validation rejects extension fields
	require.Error(t, err, "LoadFromStdin() should fail without config extensions enabled")
	assert.Contains(t, err.Error(), "guards", "Error should mention the unknown 'guards' field")
}

// TestIsConfigExtensionsEnabled tests the isConfigExtensionsEnabled helper function
func TestIsConfigExtensionsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected bool
	}{
		{"not set", "", false},
		{"true", "true", true},
		{"TRUE", "TRUE", true},
		{"True", "True", true},
		{"1", "1", true},
		{"yes", "yes", true},
		{"YES", "YES", true},
		{"on", "on", true},
		{"ON", "ON", true},
		{"false", "false", false},
		{"0", "0", false},
		{"no", "no", false},
		{"off", "off", false},
		{"invalid", "invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == "" {
				os.Unsetenv("MCP_GATEWAY_CONFIG_EXTENSIONS")
			} else {
				os.Setenv("MCP_GATEWAY_CONFIG_EXTENSIONS", tt.envValue)
			}
			defer os.Unsetenv("MCP_GATEWAY_CONFIG_EXTENSIONS")

			result := isConfigExtensionsEnabled()
			assert.Equal(t, tt.expected, result, "isConfigExtensionsEnabled() for %q", tt.envValue)
		})
	}
}

func TestGuardPolicy_StdinParsingAndConversion(t *testing.T) {
	jsonConfig := `{
		"mcpServers": {
			"github": {
				"type": "stdio",
				"container": "ghcr.io/github/github-mcp-server:latest",
				"guard": "github-guard"
			}
		},
		"guards": {
			"github-guard": {
				"type": "wasm",
				"path": "/guard/github-guard-rust.wasm",
				"policy": {
					"allowonly": {
						"repos": ["lpcox/github-guard"],
						"integrity": "reader"
					}
				}
			}
		}
	}`

	var stdinCfg StdinConfig
	err := json.Unmarshal([]byte(jsonConfig), &stdinCfg)
	require.NoError(t, err)

	cfg, err := convertStdinConfig(&stdinCfg)
	require.NoError(t, err)
	require.NotNil(t, cfg.Guards["github-guard"].Policy)

	normalized, err := NormalizeGuardPolicy(cfg.Guards["github-guard"].Policy)
	require.NoError(t, err)
	assert.Equal(t, "scoped", normalized.ScopeKind)
	assert.Equal(t, []string{"lpcox/github-guard"}, normalized.ScopeValues)
	assert.Equal(t, IntegrityReaderContrib, normalized.Integrity)
}

func TestGuardPolicy_InvalidRejected(t *testing.T) {
	stdinCfg := &StdinConfig{
		MCPServers: map[string]*StdinServerConfig{
			"github": {
				Type:      "stdio",
				Container: "ghcr.io/github/github-mcp-server:latest",
				Guard:     "github-guard",
			},
		},
		Guards: map[string]*StdinGuardConfig{
			"github-guard": {
				Type: "wasm",
				Path: "/guard/github-guard.wasm",
				Policy: &GuardPolicy{
					AllowOnly: &AllowOnlyPolicy{
						Repos:     []interface{}{"Invalid/Repo"},
						Integrity: "invalid-integrity",
					},
				},
			},
		},
	}

	_, err := convertStdinConfig(stdinCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid policy")
}

func TestParseGuardPolicyJSON(t *testing.T) {
	policy, err := ParseGuardPolicyJSON(`{"allowonly":{"repos":"public","integrity":"none"}}`)
	require.NoError(t, err)
	require.NotNil(t, policy)

	normalized, err := NormalizeGuardPolicy(policy)
	require.NoError(t, err)
	assert.Equal(t, "public", normalized.ScopeKind)
	assert.Equal(t, IntegrityNone, normalized.Integrity)
}

func TestParseGuardPolicyJSON_UpdatedRepoRegex(t *testing.T) {
	t.Run("accepts underscore scopes", func(t *testing.T) {
		policy, err := ParseGuardPolicyJSON(`{"allowonly":{"repos":["owner_name/repo_name","owner-name/*","owner_name/repo_prefix*"],"integrity":"reader"}}`)
		require.NoError(t, err)
		require.NotNil(t, policy)

		normalized, err := NormalizeGuardPolicy(policy)
		require.NoError(t, err)
		assert.Equal(t, "scoped", normalized.ScopeKind)
		assert.Equal(t, []string{"owner-name/*", "owner_name/repo_name", "owner_name/repo_prefix*"}, normalized.ScopeValues)
		assert.Equal(t, IntegrityReaderContrib, normalized.Integrity)
	})

	t.Run("rejects dot in repo scope", func(t *testing.T) {
		_, err := ParseGuardPolicyJSON(`{"allowonly":{"repos":["owner/repo.name"],"integrity":"reader"}}`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid")
	})
}
// Helper function for creating int pointers in tests
func intPtrDIFC(i int) *int {
	return &i
}
