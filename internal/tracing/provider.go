// Package tracing provides OpenTelemetry OTLP trace export for the MCP Gateway.
//
// When an OTLP endpoint is configured (via config or OTEL_EXPORTER_OTLP_ENDPOINT),
// this package initializes a real tracer provider that exports spans over HTTP.
// When no endpoint is configured, a noop tracer provider is used, adding zero overhead.
//
// Usage:
//
//	tp, err := tracing.InitProvider(ctx, cfg.Gateway.Tracing)
//	if err != nil {
//	    return err
//	}
//	defer tp.Shutdown(ctx)
//
// Once initialized, obtain a tracer with:
//
//	tracer := otel.Tracer("github.com/github/gh-aw-mcpg")
package tracing

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/strutil"
	"github.com/github/gh-aw-mcpg/internal/version"
)

const instrumentationName = "github.com/github/gh-aw-mcpg"

var logTracing = logger.New("tracing:provider")

// Provider wraps an OpenTelemetry TracerProvider and provides a Shutdown method.
type Provider struct {
	tp     trace.TracerProvider
	sdk    *sdktrace.TracerProvider // non-nil only when OTLP is configured
	tracer trace.Tracer
}

// Tracer returns the tracer for the MCP gateway instrumentation scope.
func (p *Provider) Tracer() trace.Tracer {
	return p.tracer
}

// IsEnabled reports whether the provider has a real SDK (non-noop) exporter active.
func (p *Provider) IsEnabled() bool {
	return p.sdk != nil
}

// Shutdown flushes and shuts down the tracer provider.
// For noop providers this is a no-op. Must be called on application exit.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.sdk != nil {
		logTracing.Print("Flushing and shutting down OTLP tracer provider")
		return p.sdk.Shutdown(ctx)
	}
	return nil
}

// generateRandomSpanID creates a cryptographically random 8-byte span ID.
func generateRandomSpanID() (trace.SpanID, error) {
	var id trace.SpanID
	b, err := strutil.RandomBytes(len(id))
	if err != nil {
		return id, fmt.Errorf("failed to generate random span ID: %w", err)
	}
	copy(id[:], b)
	return id, nil
}

// registerPropagator installs the global W3C TraceContext + Baggage propagator.
// This enables incoming traceparent/tracestate headers to be extracted so that
// agent-initiated traces are continued rather than fragmented.
func registerPropagator() {
	logTracing.Print("Registering global W3C TraceContext+Baggage text map propagator")
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

// InitProvider initializes the global OpenTelemetry tracer provider.
// When no endpoint is configured, a noop provider is installed (zero overhead).
// When one or more endpoints are configured, an OTLP/HTTP exporter is created
// for each endpoint and the SDK tracer provider is registered as the global
// provider. Multiple endpoints (from GH_AW_OTLP_ENDPOINTS) are supported via a
// fan-out exporter so that every backend receives complete gateway traces.
//
// In both cases a W3C TraceContext propagator is registered globally so that
// incoming traceparent/tracestate headers are honoured by all HTTP middleware.
//
// The returned Provider must be shut down on application exit to flush buffered spans.
func InitProvider(ctx context.Context, cfg *config.TracingConfig) (*Provider, error) {
	endpoint := resolveEndpoint(cfg)
	extraEndpoints := resolveExtraEndpoints(cfg)
	serviceName := resolveServiceName(cfg)
	sampleRate := resolveSampleRate(cfg)

	// Always register the W3C propagator so that incoming traceparent headers
	// are extracted, even when tracing is disabled (noop spans are still
	// parented correctly if propagation is later enabled upstream).
	registerPropagator()

	// Determine the active set of endpoints:
	//   - GH_AW_OTLP_ENDPOINTS takes precedence (fan-out to all listed endpoints).
	//   - Falls back to the single endpoint from config/OTEL_EXPORTER_OTLP_ENDPOINT.
	var activeEndpoints []string
	if len(extraEndpoints) > 0 {
		activeEndpoints = extraEndpoints
	} else if endpoint != "" {
		activeEndpoints = []string{endpoint}
	}

	if len(activeEndpoints) == 0 {
		logTracing.Printf("Tracing disabled: no OTLP endpoint configured")
		noopTP := noop.NewTracerProvider()
		otel.SetTracerProvider(noopTP)
		return &Provider{
			tp:     noopTP,
			tracer: noopTP.Tracer(instrumentationName),
		}, nil
	}

	if len(activeEndpoints) > 1 {
		logTracing.Printf("Initializing OTLP fan-out tracing: %d endpoints, service=%s, sampleRate=%.2f",
			len(activeEndpoints), serviceName, sampleRate)
	} else {
		logTracing.Printf("Initializing OTLP tracing: endpoint=%s, service=%s, sampleRate=%.2f",
			activeEndpoints[0], serviceName, sampleRate)
	}

	// Resolve shared headers applied to all exporters.
	headers := resolveHeaders(cfg)
	if headers != nil {
		logTracing.Printf("Applying %d OTLP export header(s)", len(headers))
	}

	// Build one OTLP HTTP exporter per active endpoint.
	exporters := make([]sdktrace.SpanExporter, 0, len(activeEndpoints))
	for _, ep := range activeEndpoints {
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpointURL(ep),
			otlptracehttp.WithTimeout(10 * time.Second),
		}
		if headers != nil {
			opts = append(opts, otlptracehttp.WithHeaders(headers))
		}
		exp, err := otlptracehttp.New(ctx, opts...)
		if err != nil {
			logTracing.Printf("Warning: failed to create OTLP exporter for endpoint %s: %v; skipping", ep, err)
			continue
		}
		exporters = append(exporters, exp)
	}
	if len(exporters) == 0 {
		return nil, fmt.Errorf("failed to create any OTLP trace exporters")
	}
	logTracing.Printf("OTLP HTTP trace exporter(s) created: %d", len(exporters))

	// Wrap in a fan-out exporter (returns the single exporter directly when only one).
	exporter := newFanoutExporter(exporters)

	// Build resource with service name, version, and SDK metadata
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithContainer(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version.Get()),
		),
		resource.WithProcessPID(),
		resource.WithHost(),
	)
	if err != nil {
		// Non-fatal: proceed with empty resource
		logTracing.Printf("Warning: failed to create OTEL resource: %v", err)
		res = resource.Empty()
	}

	// Select sampler based on configured rate
	var sampler sdktrace.Sampler
	switch {
	case sampleRate >= 1.0:
		sampler = sdktrace.AlwaysSample()
		logTracing.Print("Using AlwaysSample tracer sampler")
	case sampleRate <= 0.0:
		sampler = sdktrace.NeverSample()
		logTracing.Print("Using NeverSample tracer sampler")
	default:
		sampler = sdktrace.TraceIDRatioBased(sampleRate)
		logTracing.Printf("Using TraceIDRatioBased tracer sampler: rate=%.4f", sampleRate)
	}

	sdkTP := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Register as the global provider so instrumented libraries pick it up
	otel.SetTracerProvider(sdkTP)

	tracer := sdkTP.Tracer(instrumentationName)
	logTracing.Printf("OTLP tracing initialized successfully")

	return &Provider{
		tp:     sdkTP,
		sdk:    sdkTP,
		tracer: tracer,
	}, nil
}

// Tracer returns the global MCP gateway tracer.
// This is a convenience wrapper around otel.Tracer for packages that don't
// hold a reference to the Provider.
func Tracer() trace.Tracer {
	return otel.Tracer(instrumentationName)
}

// GetCachedOrGlobal returns cached if non-nil, otherwise falls back to the global tracer.
func GetCachedOrGlobal(cached trace.Tracer) trace.Tracer {
	if cached != nil {
		return cached
	}
	return Tracer()
}

// ParentContext returns a context carrying the W3C remote parent span context
// from the configured traceId and spanId (spec §4.1.3.6).
// Exported for use at startup to build the root span's parent context.
func ParentContext(ctx context.Context, cfg *config.TracingConfig) context.Context {
	return resolveParentContext(ctx, cfg)
}
