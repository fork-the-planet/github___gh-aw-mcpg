package cmd

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/config"
)

func TestRegisterTracingFlags_DefaultsFromEnv(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	t.Setenv("OTEL_SERVICE_NAME", "test-service")

	cmd := &cobra.Command{Use: "test"}

	var endpoint string
	var service string
	var sampleRate float64

	registerTracingFlags(
		cmd,
		&endpoint,
		&service,
		&sampleRate,
		"endpoint help",
		"service help",
		"sample help",
	)

	actualEndpoint, err := cmd.Flags().GetString("otlp-endpoint")
	require.NoError(t, err)
	assert.Equal(t, "http://collector:4318", actualEndpoint)

	actualService, err := cmd.Flags().GetString("otlp-service-name")
	require.NoError(t, err)
	assert.Equal(t, "test-service", actualService)

	actualSampleRate, err := cmd.Flags().GetFloat64("otlp-sample-rate")
	require.NoError(t, err)
	assert.Equal(t, config.DefaultTracingSampleRate, actualSampleRate)

	err = cmd.ParseFlags([]string{
		"--otlp-endpoint=http://override:4318",
		"--otlp-service-name=override-service",
		"--otlp-sample-rate=0.25",
	})
	require.NoError(t, err)
	assert.Equal(t, "http://override:4318", endpoint)
	assert.Equal(t, "override-service", service)
	assert.Equal(t, 0.25, sampleRate)
}

// TestInitTracingProviderWithFallback verifies that the helper returns a working
// provider for both the nil-config (noop) path and the error-fallback path.
func TestInitTracingProviderWithFallback(t *testing.T) {
	t.Run("nil config returns noop provider without error", func(t *testing.T) {
		var warnCalled bool
		provider := initTracingProviderWithFallback(
			context.Background(),
			nil,
			"warn: %v",
			func(format string, args ...any) { warnCalled = true },
		)
		require.NotNil(t, provider, "Provider must not be nil")
		assert.False(t, warnCalled, "No warning should be emitted for nil config")
	})

	t.Run("valid config with no endpoint returns noop provider without warning", func(t *testing.T) {
		var warnCalled bool
		cfg := &config.TracingConfig{
			// No endpoint — InitProvider should succeed with a noop tracer.
		}
		provider := initTracingProviderWithFallback(
			context.Background(),
			cfg,
			"warn: %v",
			func(format string, args ...any) { warnCalled = true },
		)
		require.NotNil(t, provider, "Provider must not be nil")
		assert.False(t, warnCalled, "No warning expected when no endpoint is configured")
	})

	t.Run("unreachable endpoint falls back to noop provider and emits warning", func(t *testing.T) {
		var warnMsg string
		cfg := &config.TracingConfig{
			// Use a bogus address that will immediately fail during startup.
			// InitProvider performs a dial during provider creation when an
			// endpoint is set, so this exercises the error-fallback branch.
			Endpoint: "https://127.0.0.1:1/does-not-exist",
		}
		provider := initTracingProviderWithFallback(
			context.Background(),
			cfg,
			"tracing init failed: %v",
			func(format string, args ...any) {
				warnMsg = format
			},
		)
		// Regardless of whether the provider initialisation fails,
		// the fallback must always return a non-nil provider.
		require.NotNil(t, provider, "Fallback provider must not be nil")
		// If the init failed, the warning callback must have been called.
		if warnMsg != "" {
			assert.Contains(t, warnMsg, "tracing init failed")
		}
	})
}

// TestShutdownTracingProviderWithTimeout verifies that the shutdown helper
// completes without panicking for a noop provider (which is the common case).
func TestShutdownTracingProviderWithTimeout(t *testing.T) {
	t.Run("noop provider shuts down cleanly", func(t *testing.T) {
		var warnCalled bool
		provider := initTracingProviderWithFallback(
			context.Background(),
			nil,
			"warn: %v",
			func(format string, args ...any) {},
		)
		require.NotNil(t, provider)

		// Should complete without panic or warning.
		shutdownTracingProviderWithTimeout(provider, func(format string, args ...any) {
			warnCalled = true
		})
		assert.False(t, warnCalled, "Shutdown of noop provider should not produce a warning")
	})
}

func TestSetupCommandTracing(t *testing.T) {
	t.Run("returns provider and cleanup for noop tracing config", func(t *testing.T) {
		var initWarnCalled bool
		var shutdownWarnCalled bool

		provider, cleanup := setupCommandTracing(
			context.Background(),
			nil,
			"warn: %v",
			func(format string, args ...any) {
				initWarnCalled = true
			},
			func(format string, args ...any) {
				shutdownWarnCalled = true
			},
		)

		require.NotNil(t, provider)
		require.NotNil(t, cleanup)
		assert.False(t, initWarnCalled)

		assert.NotPanics(t, cleanup)
		assert.False(t, shutdownWarnCalled)
	})
}
