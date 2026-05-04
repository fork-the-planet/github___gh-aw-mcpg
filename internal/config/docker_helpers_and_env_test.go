package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetGatewayPortFromEnv tests the env-based gateway port parsing.
func TestGetGatewayPortFromEnv_Comprehensive(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		envSet   bool
		wantPort int
		wantErr  bool
		errMsg   string
	}{
		{
			name:    "env var not set",
			envSet:  false,
			wantErr: true,
			errMsg:  "MCP_GATEWAY_PORT environment variable not set",
		},
		{
			name:     "env var set to empty string",
			envValue: "",
			envSet:   true,
			wantErr:  true,
			errMsg:   "MCP_GATEWAY_PORT environment variable not set",
		},
		{
			name:     "invalid integer value",
			envValue: "not-a-number",
			envSet:   true,
			wantErr:  true,
			errMsg:   "invalid MCP_GATEWAY_PORT value",
		},
		{
			name:     "port zero (out of range)",
			envValue: "0",
			envSet:   true,
			wantErr:  true,
		},
		{
			name:     "port 65536 (out of range)",
			envValue: "65536",
			envSet:   true,
			wantErr:  true,
		},
		{
			name:     "negative port",
			envValue: "-1",
			envSet:   true,
			wantErr:  true,
		},
		{
			name:     "valid port 3000",
			envValue: "3000",
			envSet:   true,
			wantPort: 3000,
			wantErr:  false,
		},
		{
			name:     "valid port 1",
			envValue: "1",
			envSet:   true,
			wantPort: 1,
			wantErr:  false,
		},
		{
			name:     "valid port 65535 (max)",
			envValue: "65535",
			envSet:   true,
			wantPort: 65535,
			wantErr:  false,
		},
		{
			name:     "valid port 8080",
			envValue: "8080",
			envSet:   true,
			wantPort: 8080,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envSet {
				t.Setenv("MCP_GATEWAY_PORT", tt.envValue)
			} else {
				os.Unsetenv("MCP_GATEWAY_PORT")
			}

			port, err := GetGatewayPortFromEnv()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.ErrorContains(t, err, tt.errMsg)
				}
				assert.Equal(t, 0, port)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPort, port)
			}
		})
	}
}

// TestGetGatewayToolTimeoutFromEnv tests the env-based tool timeout parsing.
func TestGetGatewayToolTimeoutFromEnv(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		envSet      bool
		wantTimeout int
		wantOK      bool
		wantErr     bool
		errMsg      string
	}{
		{
			name:    "env var not set",
			envSet:  false,
			wantOK:  false,
			wantErr: false,
		},
		{
			name:     "env var set to empty string",
			envValue: "",
			envSet:   true,
			wantOK:   false,
			wantErr:  false,
		},
		{
			name:     "invalid integer value",
			envValue: "not-a-number",
			envSet:   true,
			wantOK:   false,
			wantErr:  true,
			errMsg:   "invalid MCP_GATEWAY_TOOL_TIMEOUT value",
		},
		{
			name:     "below minimum (9)",
			envValue: "9",
			envSet:   true,
			wantOK:   false,
			wantErr:  true,
			errMsg:   "must be at least 10",
		},
		{
			name:     "zero (out of range)",
			envValue: "0",
			envSet:   true,
			wantOK:   false,
			wantErr:  true,
			errMsg:   "must be at least 10",
		},
		{
			name:     "negative value",
			envValue: "-1",
			envSet:   true,
			wantOK:   false,
			wantErr:  true,
			errMsg:   "must be at least 10",
		},
		{
			name:        "valid minimum boundary (10)",
			envValue:    "10",
			envSet:      true,
			wantTimeout: 10,
			wantOK:      true,
			wantErr:     false,
		},
		{
			name:        "valid default value (60)",
			envValue:    "60",
			envSet:      true,
			wantTimeout: 60,
			wantOK:      true,
			wantErr:     false,
		},
		{
			name:        "valid high value (120)",
			envValue:    "120",
			envSet:      true,
			wantTimeout: 120,
			wantOK:      true,
			wantErr:     false,
		},
		{
			name:        "valid large value (3600 = 1 hour)",
			envValue:    "3600",
			envSet:      true,
			wantTimeout: 3600,
			wantOK:      true,
			wantErr:     false,
		},
		{
			name:        "valid very large value (86400 = 24 hours)",
			envValue:    "86400",
			envSet:      true,
			wantTimeout: 86400,
			wantOK:      true,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envSet {
				t.Setenv("MCP_GATEWAY_TOOL_TIMEOUT", tt.envValue)
			} else {
				os.Unsetenv("MCP_GATEWAY_TOOL_TIMEOUT")
			}

			timeout, ok, err := GetGatewayToolTimeoutFromEnv()
			if tt.wantErr {
				require.Error(t, err)
				assert.False(t, ok)
				if tt.errMsg != "" {
					assert.ErrorContains(t, err, tt.errMsg)
				}
				assert.Equal(t, 0, timeout)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantOK, ok)
				if tt.wantOK {
					assert.Equal(t, tt.wantTimeout, timeout)
				} else {
					assert.Equal(t, 0, timeout)
				}
			}
		})
	}
}

// TestToolTimeoutEnvOrDefault tests that toolTimeoutEnvOrDefault uses the env var when set.
func TestToolTimeoutEnvOrDefault(t *testing.T) {
	t.Run("returns DefaultToolTimeout when env var is not set", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_TOOL_TIMEOUT", "")
		got := toolTimeoutEnvOrDefault()
		assert.Equal(t, DefaultToolTimeout, got, "should return DefaultToolTimeout when env var is empty")
	})

	t.Run("returns env var value when set to valid value", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_TOOL_TIMEOUT", "120")
		got := toolTimeoutEnvOrDefault()
		assert.Equal(t, 120, got, "should return 120 from env var")
	})

	t.Run("returns DefaultToolTimeout when env var has invalid value", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_TOOL_TIMEOUT", "not-a-number")
		got := toolTimeoutEnvOrDefault()
		assert.Equal(t, DefaultToolTimeout, got, "should fall back to default on invalid env var")
	})

	t.Run("returns env var value at minimum boundary (10)", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_TOOL_TIMEOUT", "10")
		got := toolTimeoutEnvOrDefault()
		assert.Equal(t, 10, got, "should return 10 at minimum boundary")
	})

	t.Run("returns env var large value (3600 = 1 hour)", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_TOOL_TIMEOUT", "3600")
		got := toolTimeoutEnvOrDefault()
		assert.Equal(t, 3600, got, "should return 3600 (1 hour)")
	})
}

// TestConvertStdinConfig_ToolTimeoutEnvFallback verifies the stdin config priority:
// stdin config value > MCP_GATEWAY_TOOL_TIMEOUT env var > built-in default.
func TestConvertStdinConfig_ToolTimeoutEnvFallback(t *testing.T) {
	t.Run("stdin value takes priority over env var", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_TOOL_TIMEOUT", "120")
		stdinCfg := &StdinConfig{
			MCPServers: map[string]*StdinServerConfig{
				"test": {Type: "stdio", Container: "test/server:latest"},
			},
			Gateway: &StdinGatewayConfig{
				ToolTimeout: intPtr(300),
			},
		}
		cfg, err := convertStdinConfig(stdinCfg)
		require.NoError(t, err)
		assert.Equal(t, 300, cfg.Gateway.ToolTimeout, "stdin value (300) should override env var (120)")
	})

	t.Run("env var used as fallback when stdin omits toolTimeout", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_TOOL_TIMEOUT", "120")
		stdinCfg := &StdinConfig{
			MCPServers: map[string]*StdinServerConfig{
				"test": {Type: "stdio", Container: "test/server:latest"},
			},
			Gateway: &StdinGatewayConfig{}, // toolTimeout not set
		}
		cfg, err := convertStdinConfig(stdinCfg)
		require.NoError(t, err)
		assert.Equal(t, 120, cfg.Gateway.ToolTimeout, "env var (120) should be used when stdin omits toolTimeout")
	})

	t.Run("built-in default used when both stdin and env var are absent", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_TOOL_TIMEOUT", "")
		stdinCfg := &StdinConfig{
			MCPServers: map[string]*StdinServerConfig{
				"test": {Type: "stdio", Container: "test/server:latest"},
			},
			Gateway: &StdinGatewayConfig{}, // toolTimeout not set
		}
		cfg, err := convertStdinConfig(stdinCfg)
		require.NoError(t, err)
		assert.Equal(t, DefaultToolTimeout, cfg.Gateway.ToolTimeout, "built-in default should be used when both stdin and env var are absent")
	})

	t.Run("env var used when no gateway section in stdin", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_TOOL_TIMEOUT", "180")
		stdinCfg := &StdinConfig{
			MCPServers: map[string]*StdinServerConfig{
				"test": {Type: "stdio", Container: "test/server:latest"},
			},
			// no Gateway section
		}
		cfg, err := convertStdinConfig(stdinCfg)
		require.NoError(t, err)
		assert.Equal(t, 180, cfg.Gateway.ToolTimeout, "env var (180) should be used when no gateway section present")
	})

	t.Run("built-in default used when no gateway section and no env var", func(t *testing.T) {
		t.Setenv("MCP_GATEWAY_TOOL_TIMEOUT", "")
		stdinCfg := &StdinConfig{
			MCPServers: map[string]*StdinServerConfig{
				"test": {Type: "stdio", Container: "test/server:latest"},
			},
			// no Gateway section
		}
		cfg, err := convertStdinConfig(stdinCfg)
		require.NoError(t, err)
		assert.Equal(t, DefaultToolTimeout, cfg.Gateway.ToolTimeout, "built-in default should be used when no gateway section and no env var")
	})
}
