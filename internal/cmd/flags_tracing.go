package cmd

// Tracing-related flags for OpenTelemetry OTLP trace export.

import (
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
		registerTracingFlags(cmd.Flags(), &otlpEndpoint, &otlpServiceName, &otlpSampleRate,
			"OTLP HTTP endpoint for trace export (e.g. http://localhost:4318). Defaults from OTEL_EXPORTER_OTLP_ENDPOINT when set. Tracing is disabled when empty.",
			"Service name reported in traces. Defaults from OTEL_SERVICE_NAME when set.",
			"Fraction of traces to sample and export (0.0–1.0). Default 1.0 samples everything.")
	})
}
