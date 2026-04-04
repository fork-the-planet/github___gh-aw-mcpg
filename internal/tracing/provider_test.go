package tracing_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/tracing"
)

func ptrFloat64(v float64) *float64 { return &v }

func TestInitProvider_NoEndpoint_ReturnsNoopProvider(t *testing.T) {
	ctx := context.Background()

	// With nil config (no endpoint), should return a noop provider
	provider, err := tracing.InitProvider(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, provider)

	// Noop provider must shut down cleanly
	assert.NoError(t, provider.Shutdown(ctx))

	// The global provider should be a noop provider
	tp := otel.GetTracerProvider()
	assert.IsType(t, noop.NewTracerProvider(), tp)
}

func TestInitProvider_EmptyEndpoint_ReturnsNoopProvider(t *testing.T) {
	ctx := context.Background()

	cfg := &config.TracingConfig{
		Endpoint:    "", // explicitly empty
		ServiceName: "test-service",
		SampleRate:  ptrFloat64(1.0),
	}

	provider, err := tracing.InitProvider(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)

	assert.NoError(t, provider.Shutdown(ctx))
}

func TestInitProvider_WithEndpoint_ReturnsSdkProvider(t *testing.T) {
	ctx := context.Background()

	// Point at a non-existent endpoint; exporter creation should still succeed
	// (connection is lazy) and the provider should be initialized.
	cfg := &config.TracingConfig{
		Endpoint:    "http://localhost:14318", // non-existent, but valid URL
		ServiceName: "test-service",
		SampleRate:  ptrFloat64(1.0),
	}

	provider, err := tracing.InitProvider(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)

	// Tracer should be non-nil
	assert.NotNil(t, provider.Tracer())

	// Shutdown with a short context so test doesn't hang waiting to flush
	shutdownCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	// Shutdown may fail if it tries to flush to the non-existent endpoint,
	// but the provider itself should handle it gracefully (no panic)
	_ = provider.Shutdown(shutdownCtx)
}

func TestTracer_ReturnsNonNil(t *testing.T) {
	// Reset to noop global provider
	ctx := context.Background()
	provider, err := tracing.InitProvider(ctx, nil)
	require.NoError(t, err)
	defer provider.Shutdown(ctx)

	tr := tracing.Tracer()
	assert.NotNil(t, tr)
}

func TestInitProvider_SampleRateZero_UsesNeverSampler(t *testing.T) {
	ctx := context.Background()

	cfg := &config.TracingConfig{
		Endpoint:    "http://localhost:14318",
		ServiceName: "test-service",
		SampleRate:  ptrFloat64(0.0), // never sample
	}

	provider, err := tracing.InitProvider(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)

	// Verify NeverSample behavior: spans should not be sampled
	tr := provider.Tracer()
	_, span := tr.Start(ctx, "test-span")
	assert.False(t, span.SpanContext().IsSampled(), "span should NOT be sampled with rate 0.0")
	assert.False(t, span.IsRecording(), "span should NOT be recording with rate 0.0")
	span.End()

	shutdownCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_ = provider.Shutdown(shutdownCtx)
}

func TestInitProvider_SampleRatePartial_UsesRatioSampler(t *testing.T) {
	ctx := context.Background()

	cfg := &config.TracingConfig{
		Endpoint:    "http://localhost:14318",
		ServiceName: "test-service",
		SampleRate:  ptrFloat64(0.5), // 50% sampling
	}

	provider, err := tracing.InitProvider(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)

	shutdownCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_ = provider.Shutdown(shutdownCtx)
}

func TestInitProvider_SampleRateOne_UsesAlwaysSampler(t *testing.T) {
	ctx := context.Background()

	cfg := &config.TracingConfig{
		Endpoint:    "http://localhost:14318",
		ServiceName: "test-service",
		SampleRate:  ptrFloat64(1.0), // always sample
	}

	provider, err := tracing.InitProvider(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)

	// Verify AlwaysSample behavior: spans should be sampled
	tr := provider.Tracer()
	_, span := tr.Start(ctx, "test-span")
	assert.True(t, span.SpanContext().IsSampled(), "span should be sampled with rate 1.0")
	assert.True(t, span.IsRecording(), "span should be recording with rate 1.0")
	span.End()

	shutdownCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_ = provider.Shutdown(shutdownCtx)
}

func TestInitProvider_SampleRateNil_DefaultsToAlwaysSample(t *testing.T) {
	ctx := context.Background()

	cfg := &config.TracingConfig{
		Endpoint:    "http://localhost:14318",
		ServiceName: "test-service",
		// SampleRate is nil (unset) — should default to 1.0
	}

	provider, err := tracing.InitProvider(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)

	// Verify default AlwaysSample behavior
	tr := provider.Tracer()
	_, span := tr.Start(ctx, "test-span")
	assert.True(t, span.SpanContext().IsSampled(), "span should be sampled with default rate")
	assert.True(t, span.IsRecording(), "span should be recording with default rate")
	span.End()

	shutdownCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_ = provider.Shutdown(shutdownCtx)
}
