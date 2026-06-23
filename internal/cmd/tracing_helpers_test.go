package cmd

import (
	"bytes"
	"context"
	"log"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/tracing"
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

	t.Run("configured endpoint creates SDK provider without warning", func(t *testing.T) {
		// HTTP OTLP exporters are lazily connected: otlptracehttp.New succeeds even
		// for unreachable endpoints, so InitProvider does not return an error and the
		// warn callback is never invoked. The provider is a real SDK (non-noop) instance.
		// Reset the global OTel provider to noop after the subtest to avoid leaking
		// background batcher goroutines and making other tests order-dependent.
		t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })
		var warnCalled bool
		cfg := &config.TracingConfig{
			Endpoint: "https://127.0.0.1:1/does-not-exist",
		}
		provider := initTracingProviderWithFallback(
			context.Background(),
			cfg,
			"tracing init failed: %v",
			func(format string, args ...any) { warnCalled = true },
		)
		require.NotNil(t, provider, "Provider must not be nil")
		assert.False(t, warnCalled, "OTLP exporter construction is lazy; no warning expected")
		assert.True(t, provider.IsEnabled(), "Configured endpoint should produce an SDK (non-noop) provider")
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

	t.Run("sdk provider shuts down cleanly", func(t *testing.T) {
		// HTTP OTLP exporters are lazy; construction succeeds even for unreachable endpoints.
		// Reset the global OTel provider to noop after the subtest so that a shut-down
		// provider is not left as the global, which would make later tests order-dependent.
		t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })
		provider, err := tracing.InitProvider(context.Background(), &config.TracingConfig{
			Endpoint: "http://127.0.0.1:14318",
		})
		require.NoError(t, err)
		require.True(t, provider.IsEnabled(), "Expected a real SDK provider with a configured endpoint")

		var warnCalled bool
		shutdownTracingProviderWithTimeout(provider, func(format string, args ...any) {
			warnCalled = true
		})
		assert.False(t, warnCalled, "SDK provider with no pending spans should shut down without error")
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

// TestLogTracingWarnf verifies that logTracingWarnf prefixes the message with
// "Warning: " and writes the formatted string to the default log output.
func TestLogTracingWarnf(t *testing.T) {
	var buf bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(oldOutput) })

	logTracingWarnf("disk cache failed: %s", "permission denied")

	assert.Contains(t, buf.String(), "Warning: disk cache failed: permission denied")
}
