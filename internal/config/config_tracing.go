// Package config provides configuration loading and parsing.
// This file defines the tracing configuration for OpenTelemetry OTLP export.
package config

// DefaultTracingSampleRate is the default sample rate for tracing (100% sampling).
const DefaultTracingSampleRate = 1.0

// DefaultTracingServiceName is the default service name for tracing.
const DefaultTracingServiceName = "mcp-gateway"

// TracingConfig holds OpenTelemetry tracing configuration.
// Tracing is disabled when Endpoint is empty.
//
// Configuration can also be provided via standard OTEL environment variables:
//   - OTEL_EXPORTER_OTLP_ENDPOINT — overrides Endpoint
//   - OTEL_SERVICE_NAME — overrides ServiceName
//
// Example TOML:
//
//	[gateway.tracing]
//	endpoint = "http://localhost:4318"
//	service_name = "mcp-gateway"
//	sample_rate = 1.0
type TracingConfig struct {
	// Endpoint is the OTLP HTTP endpoint to export traces to.
	// Example: "http://localhost:4318" (Jaeger, Grafana Tempo, Honeycomb, etc.)
	// If empty, tracing is disabled and a noop tracer is used.
	Endpoint string `toml:"endpoint" json:"endpoint,omitempty"`

	// ServiceName is the service name reported in traces.
	// Defaults to "mcp-gateway".
	ServiceName string `toml:"service_name" json:"service_name,omitempty"`

	// SampleRate controls the fraction of traces that are sampled and exported.
	// Valid range: 0.0 (no sampling) to 1.0 (sample everything).
	// Defaults to 1.0 (100% sampling).
	// Uses a pointer so that 0.0 can be distinguished from "unset".
	SampleRate *float64 `toml:"sample_rate" json:"sample_rate,omitempty"`
}

// GetSampleRate returns the configured sample rate, defaulting to 1.0 if unset.
func (c *TracingConfig) GetSampleRate() float64 {
	if c == nil || c.SampleRate == nil {
		return DefaultTracingSampleRate
	}
	return *c.SampleRate
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
}
