// Package config tests: coverage for validateGatewayConfig and validateTrustedBots.
//
// These tests cover branches not exercised in validation_test.go:
//   - PayloadSizeThreshold validation (< 1 rejected, >= 1 accepted)
//   - TrustedBots validation via validateGatewayConfig delegation
//   - OpenTelemetry config validation via validateGatewayConfig delegation
//   - Direct unit tests for validateTrustedBots (nil, empty, whitespace, valid)
//   - expandTracingVariables nil-config fast-path and partial-field expansion
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper shared within this file; intPtr is declared in validation_string_patterns_test.go.

// TestValidateGatewayConfig_PayloadSizeThreshold covers the payloadSizeThreshold
// branch of validateGatewayConfig, which was not exercised by the existing tests.
func TestValidateGatewayConfig_PayloadSizeThreshold(t *testing.T) {
	tests := []struct {
		name      string
		threshold *int
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "nil threshold is valid (omitted)",
			threshold: nil,
			wantErr:   false,
		},
		{
			name:      "threshold of 1 is valid (minimum positive)",
			threshold: intPtr(1),
			wantErr:   false,
		},
		{
			name:      "threshold of 524288 is valid (default)",
			threshold: intPtr(524288),
			wantErr:   false,
		},
		{
			name:      "threshold of 0 is rejected",
			threshold: intPtr(0),
			wantErr:   true,
			errMsg:    "payloadSizeThreshold must be a positive integer",
		},
		{
			name:      "negative threshold is rejected",
			threshold: intPtr(-1),
			wantErr:   true,
			errMsg:    "payloadSizeThreshold must be a positive integer",
		},
		{
			name:      "large negative threshold is rejected",
			threshold: intPtr(-99999),
			wantErr:   true,
			errMsg:    "payloadSizeThreshold must be a positive integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &StdinGatewayConfig{PayloadSizeThreshold: tt.threshold}
			err := validateGatewayConfig(gw)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateGatewayConfig_TrustedBots covers the trustedBots validation branch
// delegated from validateGatewayConfig to validateTrustedBots.
func TestValidateGatewayConfig_TrustedBots(t *testing.T) {
	tests := []struct {
		name    string
		bots    []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil trusted_bots is valid (omitted)",
			bots:    nil,
			wantErr: false,
		},
		{
			name:    "non-empty trusted_bots list is valid",
			bots:    []string{"my-bot[bot]", "another-bot[bot]"},
			wantErr: false,
		},
		{
			name:    "single valid bot is valid",
			bots:    []string{"copilot-swe-agent[bot]"},
			wantErr: false,
		},
		{
			name:    "empty trusted_bots array is rejected (spec §4.1.3.4)",
			bots:    []string{},
			wantErr: true,
			errMsg:  "trusted_bots must be a non-empty array",
		},
		{
			name:    "whitespace-only entry is rejected",
			bots:    []string{"   "},
			wantErr: true,
			errMsg:  "trusted_bots[0] must be a non-empty string",
		},
		{
			name:    "empty string entry is rejected",
			bots:    []string{""},
			wantErr: true,
			errMsg:  "trusted_bots[0] must be a non-empty string",
		},
		{
			name:    "second entry empty is rejected with correct index",
			bots:    []string{"valid-bot[bot]", ""},
			wantErr: true,
			errMsg:  "trusted_bots[1] must be a non-empty string",
		},
		{
			name:    "tab-only entry is rejected",
			bots:    []string{"valid-bot[bot]", "\t"},
			wantErr: true,
			errMsg:  "trusted_bots[1] must be a non-empty string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &StdinGatewayConfig{TrustedBots: tt.bots}
			err := validateGatewayConfig(gw)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateGatewayConfig_OpenTelemetry covers the OpenTelemetry sub-validation
// branch in validateGatewayConfig.  The per-field traceId/spanId rules are tested
// thoroughly in config_tracing_test.go; here we test the delegation path itself.
func TestValidateGatewayConfig_OpenTelemetry(t *testing.T) {
	tests := []struct {
		name    string
		otel    *StdinOpenTelemetryConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil opentelemetry is valid (omitted)",
			otel:    nil,
			wantErr: false,
		},
		{
			name: "valid HTTPS endpoint passes",
			otel: &StdinOpenTelemetryConfig{
				Endpoint: "https://otel-collector.example.com",
			},
			wantErr: false,
		},
		{
			name: "valid endpoint with traceId and spanId passes",
			otel: &StdinOpenTelemetryConfig{
				Endpoint: "https://otel-collector.example.com",
				TraceID:  "4bf92f3577b34da6a3ce929d0e0e4736",
				SpanID:   "00f067aa0ba902b7",
			},
			wantErr: false,
		},
		{
			name:    "missing endpoint is rejected (enforceHTTPS=true)",
			otel:    &StdinOpenTelemetryConfig{},
			wantErr: true,
			errMsg:  "endpoint",
		},
		{
			name: "non-HTTPS endpoint is rejected",
			otel: &StdinOpenTelemetryConfig{
				Endpoint: "http://otel-collector.example.com",
			},
			wantErr: true,
			errMsg:  "HTTPS",
		},
		{
			name: "invalid traceId is rejected",
			otel: &StdinOpenTelemetryConfig{
				Endpoint: "https://otel-collector.example.com",
				TraceID:  "not-a-valid-trace-id",
			},
			wantErr: true,
			errMsg:  "traceId",
		},
		{
			name: "all-zero traceId is rejected",
			otel: &StdinOpenTelemetryConfig{
				Endpoint: "https://otel-collector.example.com",
				TraceID:  "00000000000000000000000000000000",
			},
			wantErr: true,
			errMsg:  "all zeros",
		},
		{
			name: "invalid spanId is rejected",
			otel: &StdinOpenTelemetryConfig{
				Endpoint: "https://otel-collector.example.com",
				TraceID:  "4bf92f3577b34da6a3ce929d0e0e4736",
				SpanID:   "tooshort",
			},
			wantErr: true,
			errMsg:  "spanId",
		},
		{
			name: "service name is accepted",
			otel: &StdinOpenTelemetryConfig{
				Endpoint:    "https://otel-collector.example.com",
				ServiceName: "my-custom-service",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &StdinGatewayConfig{OpenTelemetry: tt.otel}
			err := validateGatewayConfig(gw)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.ErrorContains(t, err, tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateTrustedBots directly tests the validateTrustedBots helper function,
// covering all four branches: nil, empty slice, entry with whitespace, and valid.
func TestValidateTrustedBots(t *testing.T) {
	tests := []struct {
		name    string
		bots    []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil bots returns nil (omitted config)",
			bots:    nil,
			wantErr: false,
		},
		{
			name:    "empty array returns error (spec §4.1.3.4)",
			bots:    []string{},
			wantErr: true,
			errMsg:  "trusted_bots must be a non-empty array",
		},
		{
			name:    "single valid bot passes",
			bots:    []string{"my-bot[bot]"},
			wantErr: false,
		},
		{
			name:    "multiple valid bots pass",
			bots:    []string{"my-bot[bot]", "other-bot[bot]", "plain-bot"},
			wantErr: false,
		},
		{
			name:    "first entry empty string is rejected",
			bots:    []string{""},
			wantErr: true,
			errMsg:  "trusted_bots[0] must be a non-empty string",
		},
		{
			name:    "first entry whitespace-only is rejected",
			bots:    []string{"   "},
			wantErr: true,
			errMsg:  "trusted_bots[0] must be a non-empty string",
		},
		{
			name:    "tab-only entry is rejected",
			bots:    []string{"\t"},
			wantErr: true,
			errMsg:  "trusted_bots[0] must be a non-empty string",
		},
		{
			name:    "second entry empty string is rejected with correct index",
			bots:    []string{"valid-bot[bot]", ""},
			wantErr: true,
			errMsg:  "trusted_bots[1] must be a non-empty string",
		},
		{
			name:    "third entry whitespace-only is rejected with correct index",
			bots:    []string{"a[bot]", "b[bot]", "  \t  "},
			wantErr: true,
			errMsg:  "trusted_bots[2] must be a non-empty string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTrustedBots(tt.bots)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestExpandTracingVariables_NilConfig ensures the nil fast-path in
// expandTracingVariables returns nil without panicking.
func TestExpandTracingVariables_NilConfig(t *testing.T) {
	err := expandTracingVariables(nil)
	assert.NoError(t, err, "nil config must be a no-op")
}

// TestExpandTracingVariables_PartialFields covers the case where only some
// TracingConfig fields are set, ensuring each field is expanded independently
// and empty fields are skipped without error.
func TestExpandTracingVariables_PartialFields(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T)
		input    *TracingConfig
		wantErr  bool
		validate func(t *testing.T, cfg *TracingConfig)
	}{
		{
			name: "only endpoint set and expanded",
			setup: func(t *testing.T) {
				t.Setenv("TEST_EP_ONLY", "https://ep.example.com")
			},
			input:   &TracingConfig{Endpoint: "${TEST_EP_ONLY}"},
			wantErr: false,
			validate: func(t *testing.T, cfg *TracingConfig) {
				assert.Equal(t, "https://ep.example.com", cfg.Endpoint)
				assert.Empty(t, cfg.TraceID)
				assert.Empty(t, cfg.SpanID)
				assert.Empty(t, cfg.Headers)
			},
		},
		{
			name: "only traceId set and expanded",
			setup: func(t *testing.T) {
				t.Setenv("TEST_TID_ONLY", "4bf92f3577b34da6a3ce929d0e0e4736")
			},
			input:   &TracingConfig{TraceID: "${TEST_TID_ONLY}"},
			wantErr: false,
			validate: func(t *testing.T, cfg *TracingConfig) {
				assert.Empty(t, cfg.Endpoint)
				assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", cfg.TraceID)
				assert.Empty(t, cfg.SpanID)
			},
		},
		{
			name: "only spanId set and expanded",
			setup: func(t *testing.T) {
				t.Setenv("TEST_SID_ONLY", "00f067aa0ba902b7")
			},
			input:   &TracingConfig{SpanID: "${TEST_SID_ONLY}"},
			wantErr: false,
			validate: func(t *testing.T, cfg *TracingConfig) {
				assert.Empty(t, cfg.Endpoint)
				assert.Empty(t, cfg.TraceID)
				assert.Equal(t, "00f067aa0ba902b7", cfg.SpanID)
			},
		},
		{
			name: "only headers set and expanded",
			setup: func(t *testing.T) {
				t.Setenv("TEST_HDR_ONLY", "Bearer secret-token")
			},
			input:   &TracingConfig{Headers: "Authorization=${TEST_HDR_ONLY}"},
			wantErr: false,
			validate: func(t *testing.T, cfg *TracingConfig) {
				assert.Equal(t, "Authorization=Bearer secret-token", cfg.Headers)
			},
		},
		{
			name:    "all empty fields is a no-op",
			setup:   func(t *testing.T) {},
			input:   &TracingConfig{},
			wantErr: false,
			validate: func(t *testing.T, cfg *TracingConfig) {
				assert.Empty(t, cfg.Endpoint)
				assert.Empty(t, cfg.TraceID)
				assert.Empty(t, cfg.SpanID)
				assert.Empty(t, cfg.Headers)
			},
		},
		{
			name:    "undefined variable in traceId returns error",
			setup:   func(t *testing.T) {},
			input:   &TracingConfig{TraceID: "${UNDEFINED_TRACE_ID_XYZZY_789}"},
			wantErr: true,
		},
		{
			name:    "undefined variable in spanId returns error",
			setup:   func(t *testing.T) {},
			input:   &TracingConfig{SpanID: "${UNDEFINED_SPAN_ID_XYZZY_789}"},
			wantErr: true,
		},
		{
			name:    "undefined variable in headers returns error",
			setup:   func(t *testing.T) {},
			input:   &TracingConfig{Headers: "X-Token=${UNDEFINED_HEADER_XYZZY_789}"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}
			err := expandTracingVariables(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, tt.input)
				}
			}
		})
	}
}
