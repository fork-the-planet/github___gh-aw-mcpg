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
	if err := validateW3CHexID(cfg.TraceID, "traceId", "gateway.opentelemetry.traceId",
		traceIDPattern, allZeroTraceID, 32, "4bf92f3577b34da6a3ce929d0e0e4736"); err != nil {
		return err
	}

	// Validate spanId: must be a 16-char lowercase hex string, not all-zero
	if err := validateW3CHexID(cfg.SpanID, "spanId", "gateway.opentelemetry.spanId",
		spanIDPattern, allZeroSpanID, 16, "00f067aa0ba902b7"); err != nil {
		return err
	}

	// spanId without traceId is meaningless — log a warning but do not fail
	if cfg.SpanID != "" && cfg.TraceID == "" {
		logValidation.Print("Warning: opentelemetry spanId is set without traceId; spanId will be ignored")
	}

	logValidation.Print("OpenTelemetry config validation passed")
	return nil
}

// validateW3CHexID validates a W3C Trace Context hex ID field.
// fieldName is the JSON field (e.g. "traceId"), jsonPath is the dotted config path,
// hexLen is the expected character count (32 for trace-id, 16 for span-id),
// and example is a sample valid value used in suggestion text.
func validateW3CHexID(
	value, fieldName, jsonPath string,
	formatPattern, allZeroPattern *regexp.Regexp,
	hexLen int,
	example string,
) error {
	if value == "" {
		return nil
	}
	w3cName := strings.Replace(fieldName, "Id", " ID", 1)
	w3cHyphenName := strings.Replace(fieldName, "Id", "-id", 1)

	if !formatPattern.MatchString(value) {
		logValidation.Printf("Invalid %s format: %s", fieldName, value)
		return InvalidValue(fieldName,
			fmt.Sprintf("%s must be a %d-character lowercase hexadecimal string, got '%s'", fieldName, hexLen, value),
			jsonPath,
			fmt.Sprintf("Provide a valid W3C %s (%d lowercase hex chars, e.g., %q)", w3cName, hexLen, example))
	}
	if allZeroPattern.MatchString(value) {
		logValidation.Printf("All-zero %s rejected per W3C Trace Context: %s", fieldName, value)
		return InvalidValue(fieldName,
			fmt.Sprintf("%s must not be all zeros (W3C Trace Context forbids an all-zero %s)", fieldName, w3cHyphenName),
			jsonPath,
			fmt.Sprintf("Provide a non-zero W3C %s (e.g., %q)", w3cName, example))
	}
	return nil
}
