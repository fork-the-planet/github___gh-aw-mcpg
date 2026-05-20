// Package config provides configuration loading and parsing.
// This file defines the tracing configuration for OpenTelemetry OTLP export.
package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/config/rules"
)

// DefaultTracingSampleRate is the default sample rate for tracing (100% sampling).
const DefaultTracingSampleRate = 1.0

// DefaultTracingServiceName is the default service name for tracing.
const DefaultTracingServiceName = "mcp-gateway"

// W3C trace context patterns (spec §4.1.3.6)
var (
	traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	spanIDPattern  = regexp.MustCompile(`^[0-9a-f]{16}$`)
	// W3C Trace Context forbids all-zero trace/span IDs.
	allZeroTraceID = regexp.MustCompile(`^0{32}$`)
	allZeroSpanID  = regexp.MustCompile(`^0{16}$`)
)

// TracingConfig holds OpenTelemetry tracing configuration.
// Tracing is disabled when Endpoint is empty.
//
// Configuration can also be provided via standard OTEL environment variables:
//   - OTEL_EXPORTER_OTLP_ENDPOINT — overrides Endpoint
//   - OTEL_SERVICE_NAME — overrides ServiceName
//
// Example TOML (spec §4.1.3.6, using the opentelemetry section):
//
//	[gateway.opentelemetry]
//	endpoint = "https://otel-collector.example.com"
//	service_name = "mcp-gateway"
//	trace_id = "4bf92f3577b34da6a3ce929d0e0e4736"
//	span_id = "00f067aa0ba902b7"
//	headers = "Authorization=Bearer ${OTEL_TOKEN}"
type TracingConfig struct {
	// Endpoint is the OTLP HTTP endpoint to export traces to.
	// When using the opentelemetry section (spec §4.1.3.6), this MUST be an HTTPS URL.
	// If empty, tracing is disabled and a noop tracer is used.
	Endpoint string `toml:"endpoint" json:"endpoint,omitempty"`

	// Headers is a comma-separated list of key=value HTTP headers sent with every OTLP
	// export request (e.g. "Authorization=Bearer ${OTEL_TOKEN},X-Custom=value").
	// Supports ${VAR} variable expansion (expanded at config load time).
	Headers string `toml:"headers" json:"headers,omitempty"`

	// TraceID is an optional W3C trace ID (32-char lowercase hex) used to construct the
	// parent traceparent header, linking gateway spans into a pre-existing trace.
	// Supports ${VAR} variable expansion (expanded at config load time).
	// Must be 32 lowercase hex characters and must not be all zeros.
	TraceID string `toml:"trace_id" json:"traceId,omitempty"`

	// SpanID is an optional W3C span ID (16-char lowercase hex) paired with TraceID
	// to construct the parent traceparent header. Ignored when TraceID is absent.
	// Supports ${VAR} variable expansion (expanded at config load time).
	// Must be 16 lowercase hex characters and must not be all zeros.
	SpanID string `toml:"span_id" json:"spanId,omitempty"`

	// ServiceName is the service name reported in traces.
	// Defaults to "mcp-gateway".
	ServiceName string `toml:"service_name" json:"serviceName,omitempty"`

	// SampleRate controls the fraction of traces that are sampled and exported.
	// Valid range: 0.0 (no sampling) to 1.0 (sample everything).
	// Defaults to 1.0 (100% sampling).
	// Uses a pointer so that 0.0 can be distinguished from "unset".
	// Note: SampleRate is a gateway extension field not present in spec §4.1.3.6.
	SampleRate *float64 `toml:"sample_rate" json:"sampleRate,omitempty"`

	// SignalPath is the OTLP signal path appended to the base endpoint URL.
	// Defaults to "/v1/traces" per the OpenTelemetry specification.
	// Override this only if your collector uses a non-standard ingest path.
	SignalPath string `toml:"signal_path" json:"signalPath,omitempty"`
}

// GetSampleRate returns the configured sample rate, defaulting to 1.0 if unset.
func (c *TracingConfig) GetSampleRate() float64 {
	if c == nil || c.SampleRate == nil {
		return DefaultTracingSampleRate
	}
	return *c.SampleRate
}

// validateOpenTelemetryConfig validates OpenTelemetry configuration per spec §4.1.3.6.
// When enforceHTTPS is true (i.e. the config came from the opentelemetry section),
// the endpoint is required and MUST use HTTPS.
// Non-empty traceId and spanId values are validated as W3C hex strings; variables
// must be expanded before validation, and unexpanded ${VAR} expressions are rejected.
func validateOpenTelemetryConfig(cfg *TracingConfig, enforceHTTPS bool) error {
	if cfg == nil {
		return nil
	}

	logValidation.Print("Validating OpenTelemetry configuration (spec §4.1.3.6)")

	// endpoint is required when opentelemetry section is present
	if enforceHTTPS && cfg.Endpoint == "" {
		return rules.MissingRequired("endpoint", "opentelemetry", "gateway.opentelemetry.endpoint",
			"Provide an HTTPS OTLP endpoint (e.g., \"https://otel-collector.example.com\")")
	}

	// endpoint MUST be HTTPS (spec §4.1.3.6)
	if enforceHTTPS && cfg.Endpoint != "" {
		if !strings.HasPrefix(cfg.Endpoint, "https://") {
			logValidation.Printf("Non-HTTPS endpoint in opentelemetry config: %s", cfg.Endpoint)
			return rules.InvalidValue("endpoint",
				fmt.Sprintf("opentelemetry endpoint must use HTTPS, got '%s'", cfg.Endpoint),
				"gateway.opentelemetry.endpoint",
				"Use an HTTPS URL (e.g., \"https://otel-collector.example.com\")")
		}
	}

	// Validate traceId: must be a 32-char lowercase hex string, not all-zero
	if cfg.TraceID != "" {
		if !traceIDPattern.MatchString(cfg.TraceID) {
			logValidation.Printf("Invalid traceId format: %s", cfg.TraceID)
			return rules.InvalidValue("traceId",
				fmt.Sprintf("traceId must be a 32-character lowercase hexadecimal string, got '%s'", cfg.TraceID),
				"gateway.opentelemetry.traceId",
				"Provide a valid W3C trace ID (32 lowercase hex chars, e.g., \"4bf92f3577b34da6a3ce929d0e0e4736\")")
		}
		if allZeroTraceID.MatchString(cfg.TraceID) {
			logValidation.Printf("All-zero traceId rejected per W3C Trace Context: %s", cfg.TraceID)
			return rules.InvalidValue("traceId",
				"traceId must not be all zeros (W3C Trace Context forbids an all-zero trace-id)",
				"gateway.opentelemetry.traceId",
				"Provide a non-zero W3C trace ID (e.g., \"4bf92f3577b34da6a3ce929d0e0e4736\")")
		}
	}

	// Validate spanId: must be a 16-char lowercase hex string, not all-zero
	if cfg.SpanID != "" {
		if !spanIDPattern.MatchString(cfg.SpanID) {
			logValidation.Printf("Invalid spanId format: %s", cfg.SpanID)
			return rules.InvalidValue("spanId",
				fmt.Sprintf("spanId must be a 16-character lowercase hexadecimal string, got '%s'", cfg.SpanID),
				"gateway.opentelemetry.spanId",
				"Provide a valid W3C span ID (16 lowercase hex chars, e.g., \"00f067aa0ba902b7\")")
		}
		if allZeroSpanID.MatchString(cfg.SpanID) {
			logValidation.Printf("All-zero spanId rejected per W3C Trace Context: %s", cfg.SpanID)
			return rules.InvalidValue("spanId",
				"spanId must not be all zeros (W3C Trace Context forbids an all-zero span-id)",
				"gateway.opentelemetry.spanId",
				"Provide a non-zero W3C span ID (e.g., \"00f067aa0ba902b7\")")
		}
	}

	// spanId without traceId is meaningless — log a warning but do not fail
	if cfg.SpanID != "" && cfg.TraceID == "" {
		logValidation.Print("Warning: opentelemetry spanId is set without traceId; spanId will be ignored")
	}

	logValidation.Print("OpenTelemetry config validation passed")
	return nil
}

func init() {
	// Register default setter for Tracing config
	RegisterDefaults(func(cfg *Config) {
		if cfg.Gateway != nil && cfg.Gateway.Tracing != nil {
			if cfg.Gateway.Tracing.ServiceName == "" {
				cfg.Gateway.Tracing.ServiceName = DefaultTracingServiceName
			}
		}
	})

	// Register stdin converter for the opentelemetry gateway config field (spec §4.1.3.6).
	RegisterStdinConverter(func(cfg *Config, stdinCfg *StdinConfig) {
		if stdinCfg.Gateway == nil || stdinCfg.Gateway.OpenTelemetry == nil {
			return
		}
		otel := stdinCfg.Gateway.OpenTelemetry
		if cfg.Gateway == nil {
			cfg.Gateway = &GatewayConfig{}
		}
		cfg.Gateway.Tracing = &TracingConfig{
			Endpoint:    otel.Endpoint,
			Headers:     otel.Headers,
			TraceID:     otel.TraceID,
			SpanID:      otel.SpanID,
			ServiceName: otel.ServiceName,
		}
		if cfg.Gateway.Tracing.ServiceName == "" {
			cfg.Gateway.Tracing.ServiceName = DefaultTracingServiceName
		}
	})
}

// expandTracingVariables expands ${VAR} expressions in TracingConfig fields.
// This is called for TOML-loaded configs before validation, mirroring the
// stdin JSON path where ExpandRawJSONVariables handles expansion.
func expandTracingVariables(cfg *TracingConfig) error {
	if cfg == nil {
		return nil
	}

	logValidation.Printf("Expanding tracing config variables: hasEndpoint=%v, hasTraceID=%v, hasSpanID=%v, hasHeaders=%v",
		cfg.Endpoint != "", cfg.TraceID != "", cfg.SpanID != "", cfg.Headers != "")

	fields := []struct {
		name     string
		jsonPath string
		value    *string
	}{
		{name: "endpoint", jsonPath: "gateway.opentelemetry.endpoint", value: &cfg.Endpoint},
		{name: "traceId", jsonPath: "gateway.opentelemetry.traceId", value: &cfg.TraceID},
		{name: "spanId", jsonPath: "gateway.opentelemetry.spanId", value: &cfg.SpanID},
		{name: "headers", jsonPath: "gateway.opentelemetry.headers", value: &cfg.Headers},
	}

	for _, field := range fields {
		if *field.value == "" {
			continue
		}

		expanded, err := expandVariables(*field.value, field.jsonPath)
		if err != nil {
			return err
		}

		logValidation.Printf("Expanded tracing %s variable", field.name)
		*field.value = expanded
	}

	logValidation.Print("Tracing config variable expansion completed")
	return nil
}
