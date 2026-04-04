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
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/logger"
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

// InitProvider initializes the global OpenTelemetry tracer provider.
// When endpoint is empty, a noop provider is installed (zero overhead).
// When endpoint is configured, an OTLP/HTTP exporter is created and the SDK
// tracer provider is registered as the global provider.
//
// The returned Provider must be shut down on application exit to flush buffered spans.
func InitProvider(ctx context.Context, cfg *config.TracingConfig) (*Provider, error) {
	endpoint := resolveEndpoint(cfg)
	serviceName := resolveServiceName(cfg)
	sampleRate := resolveSampleRate(cfg)

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

	// Build OTLP HTTP exporter with 10s timeout
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint),
		otlptracehttp.WithTimeout(10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	// Build resource with service name and version
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
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
