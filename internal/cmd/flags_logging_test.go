package cmd

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestGetDefaultLogDir(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		expected string
	}{
		{
			name:     "no env var - returns default",
			setEnv:   false,
			expected: config.DefaultLogDir,
		},
		{
			name:     "env var set - returns custom path",
			envValue: "/custom/log/dir",
			setEnv:   true,
			expected: "/custom/log/dir",
		},
		{
			name:     "empty env var - returns default",
			envValue: "",
			setEnv:   true,
			expected: config.DefaultLogDir,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("MCP_GATEWAY_LOG_DIR", tt.envValue)
			} else {
				t.Setenv("MCP_GATEWAY_LOG_DIR", "")
			}

			result := getDefaultLogDir()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDefaultPayloadDir(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		expected string
	}{
		{
			name:     "no env var - returns default",
			setEnv:   false,
			expected: config.DefaultPayloadDir,
		},
		{
			name:     "env var set - returns custom path",
			envValue: "/custom/payload/dir",
			setEnv:   true,
			expected: "/custom/payload/dir",
		},
		{
			name:     "empty env var - returns default",
			envValue: "",
			setEnv:   true,
			expected: config.DefaultPayloadDir,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("MCP_GATEWAY_PAYLOAD_DIR", tt.envValue)
			} else {
				t.Setenv("MCP_GATEWAY_PAYLOAD_DIR", "")
			}

			result := getDefaultPayloadDir()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDefaultPayloadSizeThreshold(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		expected int
	}{
		{
			name:     "no env var - returns default",
			setEnv:   false,
			expected: config.DefaultPayloadSizeThreshold,
		},
		{
			name:     "valid env var",
			envValue: "2048",
			setEnv:   true,
			expected: 2048,
		},
		{
			name:     "very large threshold",
			envValue: "10240",
			setEnv:   true,
			expected: 10240,
		},
		{
			name:     "small threshold",
			envValue: "512",
			setEnv:   true,
			expected: 512,
		},
		{
			name:     "invalid value - non-numeric",
			envValue: "invalid",
			setEnv:   true,
			expected: config.DefaultPayloadSizeThreshold,
		},
		{
			name:     "invalid value - negative",
			envValue: "-100",
			setEnv:   true,
			expected: config.DefaultPayloadSizeThreshold,
		},
		{
			name:     "invalid value - zero",
			envValue: "0",
			setEnv:   true,
			expected: config.DefaultPayloadSizeThreshold,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD", tt.envValue)
			} else {
				t.Setenv("MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD", "")
			}

			result := getDefaultPayloadSizeThreshold()
			assert.Equal(t, tt.expected, result, "Threshold should match expected value")
		})
	}
}

func TestPayloadSizeThresholdFlagDefault(t *testing.T) {
	t.Setenv("MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD", "")

	result := getDefaultPayloadSizeThreshold()
	assert.Equal(t, 524288, result, "Default should be 524288 bytes (512KB)")
}

func TestPayloadSizeThresholdEnvVar(t *testing.T) {
	t.Setenv("MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD", "4096")

	result := getDefaultPayloadSizeThreshold()
	assert.Equal(t, 4096, result, "Environment variable should override default")
}

func TestGetDefaultPayloadPathPrefix(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		expected string
	}{
		{
			name:     "no env var - returns default",
			setEnv:   false,
			expected: defaultPayloadPathPrefix,
		},
		{
			name:     "env var set - returns custom path",
			envValue: "/workspace/payloads",
			setEnv:   true,
			expected: "/workspace/payloads",
		},
		{
			name:     "empty env var - returns default",
			envValue: "",
			setEnv:   true,
			expected: defaultPayloadPathPrefix,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("MCP_GATEWAY_PAYLOAD_PATH_PREFIX", tt.envValue)
			} else {
				t.Setenv("MCP_GATEWAY_PAYLOAD_PATH_PREFIX", "")
			}

			result := getDefaultPayloadPathPrefix()
			assert.Equal(t, tt.expected, result)
		})
	}
}
