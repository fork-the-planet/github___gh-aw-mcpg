package config

import (
	"fmt"
	"regexp"
	"strings"
)

// W3C trace context patterns (spec §4.1.3.6)
var (
	traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	spanIDPattern  = regexp.MustCompile(`^[0-9a-f]{16}$`)
	// W3C Trace Context forbids all-zero trace/span IDs.
	allZeroTraceID = regexp.MustCompile(`^0{32}$`)
	allZeroSpanID  = regexp.MustCompile(`^0{16}$`)
)

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
		return MissingRequired("endpoint", "opentelemetry", "gateway.opentelemetry.endpoint",
			"Provide an HTTPS OTLP endpoint (e.g., \"https://otel-collector.example.com\")")
	}

	// endpoint MUST be HTTPS (spec §4.1.3.6)
	if enforceHTTPS && cfg.Endpoint != "" {
		if !strings.HasPrefix(cfg.Endpoint, "https://") {
			logValidation.Printf("Non-HTTPS endpoint in opentelemetry config: %s", cfg.Endpoint)
			return InvalidValue("endpoint",
				fmt.Sprintf("opentelemetry endpoint must use HTTPS, got '%s'", cfg.Endpoint),
				"gateway.opentelemetry.endpoint",
				"Use an HTTPS URL (e.g., \"https://otel-collector.example.com\")")
		}
	}

	// Validate traceId: must be a 32-char lowercase hex string, not all-zero
	if cfg.TraceID != "" {
		if !traceIDPattern.MatchString(cfg.TraceID) {
			logValidation.Printf("Invalid traceId format: %s", cfg.TraceID)
			return InvalidValue("traceId",
				fmt.Sprintf("traceId must be a 32-character lowercase hexadecimal string, got '%s'", cfg.TraceID),
				"gateway.opentelemetry.traceId",
				"Provide a valid W3C trace ID (32 lowercase hex chars, e.g., \"4bf92f3577b34da6a3ce929d0e0e4736\")")
		}
		if allZeroTraceID.MatchString(cfg.TraceID) {
			logValidation.Printf("All-zero traceId rejected per W3C Trace Context: %s", cfg.TraceID)
			return InvalidValue("traceId",
				"traceId must not be all zeros (W3C Trace Context forbids an all-zero trace-id)",
				"gateway.opentelemetry.traceId",
				"Provide a non-zero W3C trace ID (e.g., \"4bf92f3577b34da6a3ce929d0e0e4736\")")
		}
	}

	// Validate spanId: must be a 16-char lowercase hex string, not all-zero
	if cfg.SpanID != "" {
		if !spanIDPattern.MatchString(cfg.SpanID) {
			logValidation.Printf("Invalid spanId format: %s", cfg.SpanID)
			return InvalidValue("spanId",
				fmt.Sprintf("spanId must be a 16-character lowercase hexadecimal string, got '%s'", cfg.SpanID),
				"gateway.opentelemetry.spanId",
				"Provide a valid W3C span ID (16 lowercase hex chars, e.g., \"00f067aa0ba902b7\")")
		}
		if allZeroSpanID.MatchString(cfg.SpanID) {
			logValidation.Printf("All-zero spanId rejected per W3C Trace Context: %s", cfg.SpanID)
			return InvalidValue("spanId",
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
