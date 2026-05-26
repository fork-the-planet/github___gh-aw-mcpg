package tracing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/config"
)

// TestParseOTLPHeaders covers the parseOTLPHeaders helper with a range of inputs.
func TestParseOTLPHeaders(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: map[string]string{},
		},
		{
			name:  "single well-formed pair",
			input: "Authorization=Bearer test-token",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
			},
		},
		{
			name:  "multiple well-formed pairs",
			input: "Authorization=Bearer test-token,X-Request-ID=req-123",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
				"X-Request-ID":  "req-123",
			},
		},
		{
			name:  "whitespace is trimmed around keys and values",
			input: " Authorization = Bearer test-token , X-Request-ID = req-123 ",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
				"X-Request-ID":  "req-123",
			},
		},
		{
			name:  "value containing '=' is preserved",
			input: "Authorization=Basic dXNlcjpwYXNz==",
			expected: map[string]string{
				"Authorization": "Basic dXNlcjpwYXNz==",
			},
		},
		{
			name:  "malformed pair without '=' is skipped",
			input: "Authorization=Bearer test-token,malformed,X-Trace-ID=trace-123",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
				"X-Trace-ID":    "trace-123",
			},
		},
		{
			name:  "pair with empty key is skipped",
			input: "Authorization=Bearer test-token,=empty-key,X-Trace-ID=trace-123",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
				"X-Trace-ID":    "trace-123",
			},
		},
		{
			name:  "pair with whitespace-only key is skipped",
			input: "Authorization=Bearer test-token,  =whitespace-key",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
			},
		},
		{
			name:  "empty trailing comma is skipped",
			input: "Authorization=Bearer test-token,",
			expected: map[string]string{
				"Authorization": "Bearer test-token",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseOTLPHeaders(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestResolveHeaders_ConfigTakesPrecedence verifies that config headers
// take precedence over the OTEL_EXPORTER_OTLP_HEADERS environment variable.
func TestResolveHeaders_ConfigTakesPrecedence(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer env-token")

	cfg := &config.TracingConfig{
		Headers: "Authorization=Bearer config-token",
	}
	headers := resolveHeaders(cfg)
	require.NotNil(t, headers)
	assert.Equal(t, "Bearer config-token", headers["Authorization"])
}

// TestResolveHeaders_FallsBackToEnvVar verifies that when config headers
// are empty, the OTEL_EXPORTER_OTLP_HEADERS env var is used as a fallback.
func TestResolveHeaders_FallsBackToEnvVar(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer%20env-token,X-Custom=value")

	cfg := &config.TracingConfig{
		Headers: "",
	}
	headers := resolveHeaders(cfg)
	require.NotNil(t, headers)
	assert.Equal(t, "Bearer env-token", headers["Authorization"])
	assert.Equal(t, "value", headers["X-Custom"])
}

// TestResolveHeaders_NilConfig_FallsBackToEnvVar verifies env var fallback
// when the TracingConfig itself is nil.
func TestResolveHeaders_NilConfig_FallsBackToEnvVar(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer%20env-token")

	headers := resolveHeaders(nil)
	require.NotNil(t, headers)
	assert.Equal(t, "Bearer env-token", headers["Authorization"])
}

// TestResolveHeaders_ConfigPreservesLiteralValue verifies that config headers
// are parsed as literal header values rather than W3C-baggage-decoded values.
func TestResolveHeaders_ConfigPreservesLiteralValue(t *testing.T) {
	cfg := &config.TracingConfig{
		Headers: "Authorization=Bearer%20config-token",
	}

	headers := resolveHeaders(cfg)
	require.NotNil(t, headers)
	assert.Equal(t, "Bearer%20config-token", headers["Authorization"])
}

// TestResolveHeaders_NoConfigNoEnvVar returns nil when neither config
// nor env var provides headers.
func TestResolveHeaders_NoConfigNoEnvVar(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")

	cfg := &config.TracingConfig{}
	headers := resolveHeaders(cfg)
	assert.Nil(t, headers)
}

// TestParseOTLPHeadersWithDecoder_DecodeValues exercises the decodeValues=true
// path, including the invalid percent-encoding fallback.
func TestParseOTLPHeadersWithDecoder_DecodeValues(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:  "valid percent-encoded value is decoded",
			input: "Authorization=Bearer%20token,X-Custom=val%3Dwithin",
			expected: map[string]string{
				"Authorization": "Bearer token",
				"X-Custom":      "val=within",
			},
		},
		{
			name:  "invalid percent-encoding falls back to raw value",
			input: "Authorization=Bearer%20token,X-Bad=%ZZ",
			expected: map[string]string{
				"Authorization": "Bearer token",
				"X-Bad":         "%ZZ",
			},
		},
		{
			name:  "lone percent sign is invalid and falls back to raw value",
			input: "X-Token=value%",
			expected: map[string]string{
				"X-Token": "value%",
			},
		},
		{
			name:  "unencoded value with no percent signs is preserved",
			input: "Authorization=Bearer plain-token",
			expected: map[string]string{
				"Authorization": "Bearer plain-token",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseOTLPHeadersWithDecoder(tt.input, true)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestResolveHeaders_EnvVar_InvalidPercentEncoding verifies that invalid
// percent-encoding in OTEL_EXPORTER_OTLP_HEADERS falls back to the raw value.
func TestResolveHeaders_EnvVar_InvalidPercentEncoding(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer%20token,X-Bad=%ZZ")

	headers := resolveHeaders(nil)
	require.NotNil(t, headers)
	assert.Equal(t, "Bearer token", headers["Authorization"])
	// Invalid percent-encoding falls back to the raw value.
	assert.Equal(t, "%ZZ", headers["X-Bad"])
}
