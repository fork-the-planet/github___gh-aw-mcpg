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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/logger"
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

// Shutdown flushes and shuts down the tracer provider.
// For noop providers this is a no-op. Must be called on application exit.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.sdk != nil {
		return p.sdk.Shutdown(ctx)
	}
	return nil
}

// resolveEndpoint returns the OTLP endpoint from config.
// CLI flags set the config value using env vars as defaults, so config already
// reflects the correct precedence: CLI flag > env var > config file.
func resolveEndpoint(cfg *config.TracingConfig) string {
	if cfg != nil {
		return cfg.Endpoint
	}
	return ""
}

// resolveServiceName returns the service name from config.
func resolveServiceName(cfg *config.TracingConfig) string {
	if cfg != nil && cfg.ServiceName != "" {
		return cfg.ServiceName
	}
	return config.DefaultTracingServiceName
}

// resolveSampleRate returns the sample rate from config (defaults to 1.0).
// Valid configured values are in the range [0.0, 1.0], where 0.0 disables sampling.
func resolveSampleRate(cfg *config.TracingConfig) float64 {
	rate := cfg.GetSampleRate()

	if rate >= 0.0 && rate <= 1.0 {
		return rate
	}

	logTracing.Printf("Warning: invalid tracing sample rate %.4f; using default %.2f", rate, config.DefaultTracingSampleRate)
	return config.DefaultTracingSampleRate
}

// parseOTLPHeaders parses a comma-separated "key=value" string into a map.
// Empty pairs, pairs without "=", and pairs with an empty key are logged as
// warnings and skipped to avoid invalid HTTP header field names.
// Leading/trailing whitespace around keys and values is trimmed.
func parseOTLPHeaders(raw string) map[string]string {
	headers := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(pair)
		if trimmed == "" {
			continue
		}
		k, v, ok := strings.Cut(trimmed, "=")
		if !ok {
			logTracing.Printf("Warning: skipping malformed OTLP header pair (missing '=')")
			continue
		}
		key := strings.TrimSpace(k)
		if key == "" {
			logTracing.Printf("Warning: skipping OTLP header pair with empty key")
			continue
		}
		headers[key] = strings.TrimSpace(v)
	}
	return headers
}

// resolveHeaders parses the configured OTLP export headers string (or returns nil).
func resolveHeaders(cfg *config.TracingConfig) map[string]string {
	if cfg == nil || cfg.Headers == "" {
		return nil
	}
	return parseOTLPHeaders(cfg.Headers)
}

// resolveParentContext builds a context carrying the W3C remote parent span context
// from the configured traceId and spanId (spec §4.1.3.6).
// If traceId is absent, or either ID is malformed, the original context is returned unchanged.
// A missing spanId is replaced with a random span ID so the traceparent is still valid.
func resolveParentContext(ctx context.Context, cfg *config.TracingConfig) context.Context {
	if cfg == nil || cfg.TraceID == "" {
		return ctx
	}

	traceIDBytes, err := hex.DecodeString(cfg.TraceID)
	if err != nil || len(traceIDBytes) != 16 {
		logTracing.Printf("Warning: invalid traceId '%s'; skipping W3C parent context", cfg.TraceID)
		return ctx
	}
	var traceID trace.TraceID
	copy(traceID[:], traceIDBytes)

	var spanID trace.SpanID
	if cfg.SpanID != "" {
		spanIDBytes, err := hex.DecodeString(cfg.SpanID)
		if err != nil || len(spanIDBytes) != 8 {
			logTracing.Printf("Warning: invalid spanId '%s'; generating a random span ID", cfg.SpanID)
			// Fall through to generate a random span ID below
		} else {
			copy(spanID[:], spanIDBytes)
		}
	}

	// When spanId is all-zeros (absent or invalid), generate a random span ID.
	// A valid SpanContext requires a non-zero SpanID (W3C Trace Context spec).
	// T-OTEL-008: when only traceId is provided, a random spanId is generated.
	if spanID == (trace.SpanID{}) {
		generatedID, genErr := generateRandomSpanID()
		if genErr != nil {
			logTracing.Printf("Warning: failed to generate random span ID: %v; skipping W3C parent context", genErr)
			return ctx
		}
		spanID = generatedID
		logTracing.Printf("Generated random spanId for W3C parent context")
	}

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	if !sc.IsValid() {
		logTracing.Printf("Warning: constructed parent SpanContext is not valid; skipping W3C parent context")
		return ctx
	}
	logTracing.Printf("W3C parent context resolved: traceId=%s, spanId=%s", traceID, spanID)
	return trace.ContextWithRemoteSpanContext(ctx, sc)
}

// generateRandomSpanID creates a cryptographically random 8-byte span ID.
func generateRandomSpanID() (trace.SpanID, error) {
	var id trace.SpanID
	if _, err := rand.Read(id[:]); err != nil {
		return id, fmt.Errorf("failed to generate random span ID: %w", err)
	}
	return id, nil
}

// registerPropagator installs the global W3C TraceContext + Baggage propagator.
// This enables incoming traceparent/tracestate headers to be extracted so that
// agent-initiated traces are continued rather than fragmented.
func registerPropagator() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

// InitProvider initializes the global OpenTelemetry tracer provider.
// When endpoint is empty, a noop provider is installed (zero overhead).
// When endpoint is configured, an OTLP/HTTP exporter is created and the SDK
// tracer provider is registered as the global provider.
//
// In both cases a W3C TraceContext propagator is registered globally so that
// incoming traceparent/tracestate headers are honoured by all HTTP middleware.
//
// The returned Provider must be shut down on application exit to flush buffered spans.
func InitProvider(ctx context.Context, cfg *config.TracingConfig) (*Provider, error) {
	endpoint := resolveEndpoint(cfg)
	serviceName := resolveServiceName(cfg)
	sampleRate := resolveSampleRate(cfg)

	// Always register the W3C propagator so that incoming traceparent headers
	// are extracted, even when tracing is disabled (noop spans are still
	// parented correctly if propagation is later enabled upstream).
	registerPropagator()

	if endpoint == "" {
		logTracing.Printf("Tracing disabled: no OTLP endpoint configured")
		noopTP := noop.NewTracerProvider()
		otel.SetTracerProvider(noopTP)
		return &Provider{
			tp:     noopTP,
			tracer: noopTP.Tracer(instrumentationName),
		}, nil
	}

	logTracing.Printf("Initializing OTLP tracing: endpoint=%s, service=%s, sampleRate=%.2f", endpoint, serviceName, sampleRate)

	// Build OTLP HTTP exporter options
	exporterOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(endpoint),
		otlptracehttp.WithTimeout(10 * time.Second),
	}

	// Apply configured headers (spec §4.1.3.6: headers sent with every OTLP export request)
	if headers := resolveHeaders(cfg); headers != nil {
		logTracing.Printf("Applying %d OTLP export header(s)", len(headers))
		exporterOpts = append(exporterOpts, otlptracehttp.WithHeaders(headers))
	}

	// Build OTLP HTTP exporter with 10s timeout
	exporter, err := otlptracehttp.New(ctx, exporterOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	// Build resource with service name, version, and SDK metadata
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithSchemaURL(semconv.SchemaURL),
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
	case sampleRate <= 0.0:
		sampler = sdktrace.NeverSample()
	default:
		sampler = sdktrace.TraceIDRatioBased(sampleRate)
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
