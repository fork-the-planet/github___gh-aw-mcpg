package cmd

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestGetDefaultOTLPEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		expected string
	}{
		{
			name:     "no env var - returns empty string (tracing disabled)",
			setEnv:   false,
			expected: "",
		},
		{
			name:     "env var set to HTTP endpoint",
			envValue: "http://localhost:4318",
			setEnv:   true,
			expected: "http://localhost:4318",
		},
		{
			name:     "env var set to HTTPS endpoint",
			envValue: "https://otel.example.com:4318",
			setEnv:   true,
			expected: "https://otel.example.com:4318",
		},
		{
			name:     "empty env var - returns empty string",
			envValue: "",
			setEnv:   true,
			expected: "",
		},
		{
			name:     "env var with trailing slash",
			envValue: "http://localhost:4318/",
			setEnv:   true,
			expected: "http://localhost:4318/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tt.envValue)
			} else {
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
			}

			result := getDefaultOTLPEndpoint()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDefaultOTLPServiceName(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		expected string
	}{
		{
			name:     "no env var - returns default service name",
			setEnv:   false,
			expected: config.DefaultTracingServiceName,
		},
		{
			name:     "env var set to custom service name",
			envValue: "my-custom-service",
			setEnv:   true,
			expected: "my-custom-service",
		},
		{
			name:     "env var set to explicit mcp-gateway",
			envValue: "mcp-gateway",
			setEnv:   true,
			expected: "mcp-gateway",
		},
		{
			name:     "empty env var - returns default",
			envValue: "",
			setEnv:   true,
			expected: config.DefaultTracingServiceName,
		},
		{
			name:     "env var with spaces in service name",
			envValue: "my service",
			setEnv:   true,
			expected: "my service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("OTEL_SERVICE_NAME", tt.envValue)
			} else {
				t.Setenv("OTEL_SERVICE_NAME", "")
			}

			result := getDefaultOTLPServiceName()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDefaultOTLPServiceName_DefaultIsCorrect(t *testing.T) {
	// Verify the default constant value hasn't changed unexpectedly.
	// "mcp-gateway" is the canonical service name used in OTLP traces.
	assert.Equal(t, "mcp-gateway", config.DefaultTracingServiceName,
		"DefaultTracingServiceName constant should remain 'mcp-gateway'")
}
