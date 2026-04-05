// Package config provides configuration loading and parsing.
// This file contains compliance tests for OpenTelemetry configuration per spec §4.1.3.6
// (MCP Gateway Specification v1.11.0).
//
// Test IDs correspond to the compliance matrix in the issue:
//   - T-OTEL-001: Gateway starts when opentelemetry is omitted
//   - T-OTEL-002: Gateway starts with valid HTTPS endpoint
//   - T-OTEL-003: Reject missing endpoint when opentelemetry is present
//   - T-OTEL-004: Reject non-HTTPS endpoint
//   - T-OTEL-005: TracingConfig carries required fields (headers, traceId, spanId)
//   - T-OTEL-006: Headers are preserved in TracingConfig
//   - T-OTEL-007: Valid traceId + spanId pass validation
//   - T-OTEL-008: traceId-only is valid (spanId optional)
//   - T-OTEL-009: spanId without traceId logs warning but does not fail
//   - T-OTEL-010: serviceName defaults to "mcp-gateway"
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T-OTEL-001: Gateway starts when the opentelemetry section is omitted.
// No error should be produced when TracingConfig is nil.
func TestOTEL001_NoOpenTelemetryConfig_NoError(t *testing.T) {
	err := validateOpenTelemetryConfig(nil, true)
	require.NoError(t, err, "T-OTEL-001: nil config must not produce an error")
}

// T-OTEL-002: Gateway starts (validates) with a valid HTTPS endpoint.
func TestOTEL002_ValidHTTPSEndpoint_NoError(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint:    "https://otel-collector.example.com",
		ServiceName: "mcp-gateway",
	}
	err := validateOpenTelemetryConfig(cfg, true)
	require.NoError(t, err, "T-OTEL-002: valid HTTPS endpoint must be accepted")
}

// T-OTEL-003: Reject missing endpoint when the opentelemetry section is present.
func TestOTEL003_MissingEndpoint_Error(t *testing.T) {
	cfg := &TracingConfig{
		ServiceName: "mcp-gateway",
		// Endpoint intentionally absent
	}
	err := validateOpenTelemetryConfig(cfg, true)
	require.Error(t, err, "T-OTEL-003: missing endpoint must be rejected")
	assert.Contains(t, err.Error(), "endpoint", "error must mention the missing field")
}

// T-OTEL-004: Reject non-HTTPS endpoint.
func TestOTEL004_NonHTTPSEndpoint_Error(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint: "http://otel-collector.example.com", // HTTP, not HTTPS
	}
	err := validateOpenTelemetryConfig(cfg, true)
	require.Error(t, err, "T-OTEL-004: non-HTTPS endpoint must be rejected")
	assert.Contains(t, err.Error(), "HTTPS", "error must mention the HTTPS requirement")
}

// T-OTEL-005: TracingConfig struct carries all required spec §4.1.3.6 fields.
func TestOTEL005_TracingConfigFields(t *testing.T) {
	headers := map[string]string{"Authorization": "Bearer token"}
	cfg := &TracingConfig{
		Endpoint:    "https://otel-collector.example.com",
		Headers:     headers,
		TraceID:     "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:      "00f067aa0ba902b7",
		ServiceName: "my-service",
	}

	assert.Equal(t, "https://otel-collector.example.com", cfg.Endpoint)
	assert.Equal(t, headers, cfg.Headers)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", cfg.TraceID)
	assert.Equal(t, "00f067aa0ba902b7", cfg.SpanID)
	assert.Equal(t, "my-service", cfg.ServiceName)
}

// T-OTEL-006: Headers are preserved in TracingConfig when configured.
func TestOTEL006_HeadersPreserved(t *testing.T) {
	headers := map[string]string{
		"Authorization": "Bearer my-token",
		"X-Custom":      "value",
	}
	cfg := &TracingConfig{
		Endpoint: "https://otel-collector.example.com",
		Headers:  headers,
	}

	err := validateOpenTelemetryConfig(cfg, true)
	require.NoError(t, err)
	assert.Equal(t, headers, cfg.Headers, "T-OTEL-006: headers must be preserved unchanged")
}

// T-OTEL-007: Valid W3C traceId (32-char lowercase hex) + spanId (16-char lowercase hex) pass validation.
func TestOTEL007_ValidTraceIDAndSpanID_NoError(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint: "https://otel-collector.example.com",
		TraceID:  "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:   "00f067aa0ba902b7",
	}
	err := validateOpenTelemetryConfig(cfg, true)
	require.NoError(t, err, "T-OTEL-007: valid traceId+spanId must be accepted")
}

// T-OTEL-007b: Invalid traceId (wrong length) must be rejected.
func TestOTEL007b_InvalidTraceID_Error(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint: "https://otel-collector.example.com",
		TraceID:  "4bf92f35", // too short
	}
	err := validateOpenTelemetryConfig(cfg, true)
	require.Error(t, err, "T-OTEL-007b: invalid traceId must be rejected")
	assert.Contains(t, err.Error(), "traceId")
}

// T-OTEL-007c: Invalid spanId (wrong length) must be rejected.
func TestOTEL007c_InvalidSpanID_Error(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint: "https://otel-collector.example.com",
		TraceID:  "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:   "00f067aa", // too short
	}
	err := validateOpenTelemetryConfig(cfg, true)
	require.Error(t, err, "T-OTEL-007c: invalid spanId must be rejected")
	assert.Contains(t, err.Error(), "spanId")
}

// T-OTEL-007d: Uppercase hex in traceId must be rejected (must be lowercase).
func TestOTEL007d_UppercaseTraceID_Error(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint: "https://otel-collector.example.com",
		TraceID:  "4BF92F3577B34DA6A3CE929D0E0E4736", // uppercase
	}
	err := validateOpenTelemetryConfig(cfg, true)
	require.Error(t, err, "T-OTEL-007d: uppercase traceId must be rejected")
}

// T-OTEL-008: traceId alone (without spanId) is valid — spanId is optional.
func TestOTEL008_TraceIDOnlyIsValid(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint: "https://otel-collector.example.com",
		TraceID:  "4bf92f3577b34da6a3ce929d0e0e4736",
		// SpanID intentionally absent
	}
	err := validateOpenTelemetryConfig(cfg, true)
	require.NoError(t, err, "T-OTEL-008: traceId without spanId must be accepted")
}

// T-OTEL-009: spanId without traceId must NOT fail validation (warning only).
func TestOTEL009_SpanIDWithoutTraceID_NoError(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint: "https://otel-collector.example.com",
		SpanID:   "00f067aa0ba902b7",
		// TraceID intentionally absent
	}
	err := validateOpenTelemetryConfig(cfg, true)
	require.NoError(t, err, "T-OTEL-009: spanId without traceId must produce a warning but not an error")
}

// T-OTEL-010: serviceName defaults to "mcp-gateway" when not specified.
// Tests the actual registered defaults setter applied via applyDefaults.
func TestOTEL010_ServiceNameDefaults(t *testing.T) {
	// Test the constant
	assert.Equal(t, "mcp-gateway", DefaultTracingServiceName, "T-OTEL-010: DefaultTracingServiceName must be 'mcp-gateway'")

	// Test that the defaults setter correctly applies the default service name
	cfg := &Config{
		Gateway: &GatewayConfig{
			Tracing: &TracingConfig{
				Endpoint: "https://otel-collector.example.com",
				// ServiceName intentionally absent
			},
		},
	}
	applyDefaults(cfg)
	assert.Equal(t, "mcp-gateway", cfg.Gateway.Tracing.ServiceName,
		"T-OTEL-010: default serviceName must be 'mcp-gateway' after applyDefaults")
}

// TestValidateOpenTelemetryConfig_UnexpandedVarExpressions verifies that unexpanded
// ${VAR} expressions are rejected by validation. In practice, expandTracingVariables
// (TOML path) or ExpandRawJSONVariables (stdin JSON path) expand vars before validation,
// so unexpanded expressions should never reach the validator in normal flow.
func TestValidateOpenTelemetryConfig_UnexpandedVarExpressions(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint: "https://otel-collector.example.com",
		TraceID:  "${TRACE_ID}",
	}
	err := validateOpenTelemetryConfig(cfg, true)
	require.Error(t, err, "Unexpanded variable expressions must fail hex validation")
	assert.Contains(t, err.Error(), "traceId")
}

// TestExpandTracingVariables verifies that ${VAR} expressions in tracing config
// fields are expanded from environment variables.
func TestExpandTracingVariables(t *testing.T) {
	t.Setenv("TEST_OTEL_ENDPOINT", "https://otel.example.com")
	t.Setenv("TEST_TRACE_ID", "4bf92f3577b34da6a3ce929d0e0e4736")
	t.Setenv("TEST_SPAN_ID", "00f067aa0ba902b7")
	t.Setenv("TEST_AUTH_TOKEN", "Bearer secret-token")

	cfg := &TracingConfig{
		Endpoint: "${TEST_OTEL_ENDPOINT}",
		TraceID:  "${TEST_TRACE_ID}",
		SpanID:   "${TEST_SPAN_ID}",
		Headers:  map[string]string{"Authorization": "${TEST_AUTH_TOKEN}"},
	}

	err := expandTracingVariables(cfg)
	require.NoError(t, err)

	assert.Equal(t, "https://otel.example.com", cfg.Endpoint)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", cfg.TraceID)
	assert.Equal(t, "00f067aa0ba902b7", cfg.SpanID)
	assert.Equal(t, "Bearer secret-token", cfg.Headers["Authorization"])

	// After expansion, validation should pass
	err = validateOpenTelemetryConfig(cfg, true)
	require.NoError(t, err, "Expanded config should pass validation")
}

// TestExpandTracingVariables_UndefinedVar verifies that an undefined variable
// in tracing config causes an error during expansion.
func TestExpandTracingVariables_UndefinedVar(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint: "${UNDEFINED_OTEL_ENDPOINT_XYZZY}",
	}
	err := expandTracingVariables(cfg)
	require.Error(t, err, "Undefined variable must cause expansion error")
}

// TestValidateOpenTelemetryConfig_AllZeroTraceID verifies that an all-zero traceId
// is rejected per W3C Trace Context specification.
func TestValidateOpenTelemetryConfig_AllZeroTraceID(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint: "https://otel-collector.example.com",
		TraceID:  "00000000000000000000000000000000",
	}
	err := validateOpenTelemetryConfig(cfg, true)
	require.Error(t, err, "All-zero traceId must be rejected per W3C Trace Context")
	assert.Contains(t, err.Error(), "all zeros")
}

// TestValidateOpenTelemetryConfig_AllZeroSpanID verifies that an all-zero spanId
// is rejected per W3C Trace Context specification.
func TestValidateOpenTelemetryConfig_AllZeroSpanID(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint: "https://otel-collector.example.com",
		TraceID:  "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:   "0000000000000000",
	}
	err := validateOpenTelemetryConfig(cfg, true)
	require.Error(t, err, "All-zero spanId must be rejected per W3C Trace Context")
	assert.Contains(t, err.Error(), "all zeros")
}

// TestGetSampleRate_NewFields verifies that the new fields don't affect GetSampleRate.
func TestGetSampleRate_NewFields(t *testing.T) {
	rate := 0.5
	cfg := &TracingConfig{
		Endpoint:    "https://otel-collector.example.com",
		Headers:     map[string]string{"Authorization": "Bearer tok"},
		TraceID:     "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:      "00f067aa0ba902b7",
		ServiceName: "my-service",
		SampleRate:  &rate,
	}
	assert.InDelta(t, 0.5, cfg.GetSampleRate(), 0.001)
}

// TestValidateOpenTelemetryConfig_NonEnforcing verifies that when enforceHTTPS is false,
// a non-HTTPS endpoint is allowed (backward compat with legacy tracing section).
func TestValidateOpenTelemetryConfig_NonEnforcing(t *testing.T) {
	cfg := &TracingConfig{
		Endpoint: "http://localhost:4318", // HTTP is OK in legacy mode
	}
	err := validateOpenTelemetryConfig(cfg, false)
	require.NoError(t, err, "Non-enforcing mode should accept HTTP endpoints for backward compat")
}
