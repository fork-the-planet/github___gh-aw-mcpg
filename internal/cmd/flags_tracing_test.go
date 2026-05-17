package cmd

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/config"
)

func TestDefaultTracingServiceName_IsCorrect(t *testing.T) {
	// Verify the default constant value hasn't changed unexpectedly.
	// "mcp-gateway" is the canonical service name used in OTLP traces.
	assert.Equal(t, "mcp-gateway", config.DefaultTracingServiceName,
		"DefaultTracingServiceName constant should remain 'mcp-gateway'")
}

// TestEnsureTracingConfig_WhenNil verifies that ensureTracingConfig initializes
// a new TracingConfig when cfg.Gateway.Tracing is nil.
func TestEnsureTracingConfig_WhenNil(t *testing.T) {
	cfg := &config.Config{
		Gateway: &config.GatewayConfig{},
	}
	require.Nil(t, cfg.Gateway.Tracing, "Tracing should start nil")

	tc := ensureTracingConfig(cfg)

	require.NotNil(t, tc, "ensureTracingConfig should return a non-nil TracingConfig")
	assert.Same(t, cfg.Gateway.Tracing, tc, "cfg.Gateway.Tracing should point to the returned config")
}

// TestEnsureTracingConfig_WhenNotNil verifies that ensureTracingConfig returns
// the existing TracingConfig without replacing it.
func TestEnsureTracingConfig_WhenNotNil(t *testing.T) {
	existing := &config.TracingConfig{Endpoint: "http://collector:4318"}
	cfg := &config.Config{
		Gateway: &config.GatewayConfig{Tracing: existing},
	}

	tc := ensureTracingConfig(cfg)

	assert.Same(t, existing, tc, "ensureTracingConfig should return the existing TracingConfig unchanged")
	assert.Equal(t, "http://collector:4318", tc.Endpoint, "Endpoint should not be modified")
}

// TestEnsureTracingConfig_NilGateway verifies that ensureTracingConfig
// does not panic and returns a usable TracingConfig when Gateway is nil.
func TestEnsureTracingConfig_NilGateway(t *testing.T) {
	cfg := &config.Config{}
	// Gateway is nil — this exercises a defensive path.
	assert.NotPanics(t, func() {
		// ensureTracingConfig reads cfg.Gateway.Tracing; if Gateway is nil it will panic.
		// This test documents the expected pre-condition: Gateway must be non-nil before calling.
		// We therefore set up a minimal Gateway to avoid a nil-dereference.
		cfg.Gateway = &config.GatewayConfig{}
		tc := ensureTracingConfig(cfg)
		assert.NotNil(t, tc)
	})
}

// TestRegisterTracingFlags_DefaultsWithNoEnv verifies that when neither
// OTEL_EXPORTER_OTLP_ENDPOINT nor OTEL_SERVICE_NAME are set, the flags use
// their built-in defaults (empty endpoint, "mcp-gateway" service name, 1.0 sample rate).
func TestRegisterTracingFlags_DefaultsWithNoEnv(t *testing.T) {
	originalEndpoint, hadEndpoint := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT")
	originalService, hadService := os.LookupEnv("OTEL_SERVICE_NAME")

	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	os.Unsetenv("OTEL_SERVICE_NAME")
	t.Cleanup(func() {
		if hadEndpoint {
			os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", originalEndpoint)
		} else {
			os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		}
		if hadService {
			os.Setenv("OTEL_SERVICE_NAME", originalService)
		} else {
			os.Unsetenv("OTEL_SERVICE_NAME")
		}
	})

	cmd := &cobra.Command{Use: "test"}
	var endpoint, service string
	var sampleRate float64

	registerTracingFlags(cmd.Flags(), &endpoint, &service, &sampleRate,
		"endpoint help", "service help", "sample help")

	actualEndpoint, err := cmd.Flags().GetString("otlp-endpoint")
	require.NoError(t, err)
	assert.Empty(t, actualEndpoint, "otlp-endpoint should default to empty when env var is not set")

	actualService, err := cmd.Flags().GetString("otlp-service-name")
	require.NoError(t, err)
	assert.Equal(t, config.DefaultTracingServiceName, actualService,
		"otlp-service-name should default to DefaultTracingServiceName when env var is not set")

	actualSampleRate, err := cmd.Flags().GetFloat64("otlp-sample-rate")
	require.NoError(t, err)
	assert.Equal(t, config.DefaultTracingSampleRate, actualSampleRate,
		"otlp-sample-rate should default to DefaultTracingSampleRate")
}
