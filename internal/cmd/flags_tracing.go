package cmd

// Tracing-related flags for OpenTelemetry OTLP trace export.

import (
	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/spf13/cobra"
)

// Tracing flag variables
var (
	otlpEndpoint    string
	otlpServiceName string
	otlpSampleRate  float64
)

func init() {
	RegisterFlag(func(cmd *cobra.Command) {
		cmd.Flags().StringVar(&otlpEndpoint, "otlp-endpoint", getDefaultOTLPEndpoint(),
			"OTLP HTTP endpoint for trace export (e.g. http://localhost:4318). Defaults from OTEL_EXPORTER_OTLP_ENDPOINT when set. Tracing is disabled when empty.")
		cmd.Flags().StringVar(&otlpServiceName, "otlp-service-name", getDefaultOTLPServiceName(),
			"Service name reported in traces. Defaults from OTEL_SERVICE_NAME when set.")
		cmd.Flags().Float64Var(&otlpSampleRate, "otlp-sample-rate", config.DefaultTracingSampleRate,
			"Fraction of traces to sample and export (0.0–1.0). Default 1.0 samples everything.")
	})
}

// getDefaultOTLPEndpoint returns the OTLP endpoint, checking OTEL_EXPORTER_OTLP_ENDPOINT
// environment variable first, then falling back to empty (disabled).
func getDefaultOTLPEndpoint() string {
	return envutil.GetEnvString("OTEL_EXPORTER_OTLP_ENDPOINT", "")
}

// getDefaultOTLPServiceName returns the OTLP service name, checking OTEL_SERVICE_NAME
// environment variable first, then falling back to the default.
func getDefaultOTLPServiceName() string {
	return envutil.GetEnvString("OTEL_SERVICE_NAME", config.DefaultTracingServiceName)
}
