package tracing_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
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

// TestInitProvider_GlobalPropagatorRegistration verifies that InitProvider registers the
// W3C TraceContext propagator globally, so that incoming traceparent headers are
// respected by downstream HTTP middleware.
func TestInitProvider_GlobalPropagatorRegistration(t *testing.T) {
	ctx := context.Background()

	// Use a nil config (noop path) — propagator should still be registered.
	provider, err := tracing.InitProvider(ctx, nil)
	require.NoError(t, err)
	defer provider.Shutdown(ctx)

	prop := otel.GetTextMapPropagator()
	require.NotNil(t, prop)

	// Round-trip: inject a known span context, then extract it.
	exporter := tracetest.NewInMemoryExporter()
	sp := sdktrace.NewSimpleSpanProcessor(exporter)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	tr := tp.Tracer("test")

	// Create a parent span and inject its context into HTTP headers.
	_, parentSpan := tr.Start(ctx, "parent")
	parentSpanCtx := parentSpan.SpanContext()
	parentSpan.End()

	carrier := propagation.MapCarrier{}
	prop.Inject(trace.ContextWithSpanContext(ctx, parentSpanCtx), carrier)

	// Simulate an incoming HTTP request carrying the traceparent header.
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	for k, v := range carrier {
		req.Header.Set(k, v)
	}

	// Extract should recover the parent span context from the request headers.
	extractedCtx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.HeaderCarrier(req.Header))
	extractedSpanCtx := trace.SpanFromContext(extractedCtx).SpanContext()

	assert.Equal(t, parentSpanCtx.TraceID(), extractedSpanCtx.TraceID(),
		"extracted trace ID must match the injected parent trace ID")
}

// TestWrapHTTPHandler_ContinuesRemoteTrace verifies that WrapHTTPHandler extracts an
// incoming traceparent header and makes the span a child of the remote parent.
func TestWrapHTTPHandler_ContinuesRemoteTrace(t *testing.T) {
	ctx := context.Background()

	// Initialise provider so the W3C propagator is registered globally.
	provider, err := tracing.InitProvider(ctx, nil)
	require.NoError(t, err)
	defer provider.Shutdown(ctx)

	// Build an in-memory SDK provider so we can capture spans.
	exporter := tracetest.NewInMemoryExporter()
	sp := sdktrace.NewSimpleSpanProcessor(exporter)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(noop.NewTracerProvider())

	// Create a parent span and build a request with its traceparent header.
	_, parentSpan := tp.Tracer("test").Start(ctx, "agent-span")
	parentTraceID := parentSpan.SpanContext().TraceID()
	parentSpan.End()

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(
		trace.ContextWithSpanContext(ctx, parentSpan.SpanContext()),
		carrier,
	)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	for k, v := range carrier {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()

	// Capture the span context seen inside the handler.
	var capturedSpanCtx trace.SpanContext
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSpanCtx = trace.SpanFromContext(r.Context()).SpanContext()
	})

	handler := tracing.WrapHTTPHandler(inner, "test.request")
	handler.ServeHTTP(rr, req)

	assert.Equal(t, parentTraceID, capturedSpanCtx.TraceID(),
		"handler span should share the parent's trace ID when traceparent is present")
}

// TestWrapHTTPHandler_GeneratesRootSpan verifies that when no
// traceparent header is present a fresh root span (new trace ID) is generated.
func TestWrapHTTPHandler_GeneratesRootSpan(t *testing.T) {
	ctx := context.Background()

	provider, err := tracing.InitProvider(ctx, nil)
	require.NoError(t, err)
	defer provider.Shutdown(ctx)

	exporter := tracetest.NewInMemoryExporter()
	sp := sdktrace.NewSimpleSpanProcessor(exporter)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(noop.NewTracerProvider())

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil) // no traceparent
	rr := httptest.NewRecorder()

	var capturedSpanCtx trace.SpanContext
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSpanCtx = trace.SpanFromContext(r.Context()).SpanContext()
	})

	handler := tracing.WrapHTTPHandler(inner, "test.request")
	handler.ServeHTTP(rr, req)

	assert.True(t, capturedSpanCtx.IsValid(), "should have a valid span context even without traceparent")
	assert.False(t, capturedSpanCtx.IsRemote(), "span should not be marked remote — it is a local root span")
}

// TestInitProvider_WithHeaders verifies that OTLP export headers are forwarded
// to the collector. A channel synchronises with the test HTTP server so the
// assertion is deterministic rather than timing-dependent.
func TestInitProvider_WithHeaders(t *testing.T) {
	ctx := context.Background()

	// Channel signals when the test server receives an export request.
	received := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case received <- r.Header.Get("Authorization"):
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := &config.TracingConfig{
		Endpoint: ts.URL,
		Headers:  "Authorization=Bearer test-token",
	}

	provider, err := tracing.InitProvider(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)

	// Create and end a span to trigger an export attempt.
	tr := provider.Tracer()
	_, span := tr.Start(ctx, "header-test-span")
	span.End()

	// Shutdown flushes the batch processor, ensuring the export is sent.
	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = provider.Shutdown(shutdownCtx)

	// Wait for the export request with a timeout.
	select {
	case auth := <-received:
		assert.Equal(t, "Bearer test-token", auth,
			"Authorization header must be forwarded to the OTLP collector")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for OTLP export request — headers test is non-deterministic")
	}
}

// TestParentContext_WithValidTraceIDAndSpanID verifies that ParentContext builds a valid
// remote span context when both traceId and spanId are provided.
func TestParentContext_WithValidTraceIDAndSpanID(t *testing.T) {
	ctx := context.Background()

	// Initialize noop provider to set up the W3C propagator globally
	provider, err := tracing.InitProvider(ctx, nil)
	require.NoError(t, err)
	defer provider.Shutdown(ctx)

	cfg := &config.TracingConfig{
		Endpoint: "https://example.com",
		TraceID:  "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:   "00f067aa0ba902b7",
	}

	parentCtx := tracing.ParentContext(ctx, cfg)

	// The context must be enriched (different from background context)
	assert.NotEqual(t, ctx, parentCtx, "ParentContext must return an enriched context")

	// Verify the remote span context contains the correct traceId and spanId
	// by extracting it from the context and checking via propagation round-trip.
	prop := otel.GetTextMapPropagator()
	carrier := propagation.MapCarrier{}
	prop.Inject(parentCtx, carrier)
	traceparent := carrier["traceparent"]
	require.NotEmpty(t, traceparent, "W3C traceparent must be present after injection")

	// traceparent format: 00-{traceId}-{spanId}-{flags}
	assert.Contains(t, traceparent, "4bf92f3577b34da6a3ce929d0e0e4736",
		"traceparent must contain the configured traceId")
	assert.Contains(t, traceparent, "00f067aa0ba902b7",
		"traceparent must contain the configured spanId")
}

// TestParentContext_WithTraceIDOnly verifies that ParentContext works when only traceId is provided.
func TestParentContext_WithTraceIDOnly(t *testing.T) {
	ctx := context.Background()

	cfg := &config.TracingConfig{
		TraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
		// SpanID intentionally absent
	}

	parentCtx := tracing.ParentContext(ctx, cfg)
	// Should return an enriched context
	assert.NotEqual(t, ctx, parentCtx, "ParentContext with traceId only must return an enriched context")
}

// TestParentContext_NoConfig verifies that ParentContext is a no-op when config is nil.
func TestParentContext_NoConfig(t *testing.T) {
	ctx := context.Background()
	parentCtx := tracing.ParentContext(ctx, nil)
	assert.Equal(t, ctx, parentCtx, "ParentContext with nil config must return the original context unchanged")
}

// TestParentContext_EmptyTraceID verifies that ParentContext is a no-op when traceId is empty.
func TestParentContext_EmptyTraceID(t *testing.T) {
	ctx := context.Background()
	cfg := &config.TracingConfig{
		SpanID: "00f067aa0ba902b7", // spanId without traceId
	}
	parentCtx := tracing.ParentContext(ctx, cfg)
	assert.Equal(t, ctx, parentCtx, "ParentContext without traceId must return the original context unchanged")
}

// TestParentContext_InvalidTraceID verifies that ParentContext handles malformed traceIds gracefully.
func TestParentContext_InvalidTraceID(t *testing.T) {
	ctx := context.Background()
	cfg := &config.TracingConfig{
		TraceID: "not-valid-hex",
	}
	parentCtx := tracing.ParentContext(ctx, cfg)
	assert.Equal(t, ctx, parentCtx, "ParentContext with invalid traceId must return original context")
}

// TestInitProvider_WithTraceIDAndSpanID verifies that InitProvider succeeds with traceId/spanId config.
func TestInitProvider_WithTraceIDAndSpanID(t *testing.T) {
	ctx := context.Background()

	cfg := &config.TracingConfig{
		Endpoint: fmt.Sprintf("http://localhost:%d", 14320),
		TraceID:  "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:   "00f067aa0ba902b7",
	}

	provider, err := tracing.InitProvider(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)

	shutdownCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_ = provider.Shutdown(shutdownCtx)
}
