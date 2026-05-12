package cmd

// Tracing-related flags and helpers for OpenTelemetry OTLP trace export.

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/github/gh-aw-mcpg/internal/tracing"
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

func registerTracingFlags(flags *pflag.FlagSet, endpoint *string, serviceName *string, sampleRate *float64, endpointUsage string, serviceUsage string, sampleUsage string) {
	flags.StringVar(endpoint, "otlp-endpoint", envutil.GetEnvString("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		endpointUsage)
	flags.StringVar(serviceName, "otlp-service-name", envutil.GetEnvString("OTEL_SERVICE_NAME", config.DefaultTracingServiceName),
		serviceUsage)
	flags.Float64Var(sampleRate, "otlp-sample-rate", config.DefaultTracingSampleRate,
		sampleUsage)
}

// ensureTracingConfig returns cfg.Gateway.Tracing, initializing it if nil.
func ensureTracingConfig(cfg *config.Config) *config.TracingConfig {
	if cfg.Gateway.Tracing == nil {
		cfg.Gateway.Tracing = &config.TracingConfig{}
	}
	return cfg.Gateway.Tracing
}

func initTracingProviderWithFallback(
	ctx context.Context,
	tracingCfg *config.TracingConfig,
	initWarningFormat string,
	warnf func(format string, args ...any),
) *tracing.Provider {
	debugLog.Print("Initializing tracing provider")
	tracingProvider, err := tracing.InitProvider(ctx, tracingCfg)
	if err != nil {
		debugLog.Printf("Tracing provider init failed, falling back to no-op provider: %v", err)
		warnf(initWarningFormat, err)
		tracingProvider, _ = tracing.InitProvider(ctx, nil)
	} else {
		debugLog.Print("Tracing provider initialized successfully")
	}

	return tracingProvider
}

func shutdownTracingProviderWithTimeout(tracingProvider *tracing.Provider, warnf func(format string, args ...any)) {
	debugLog.Print("Shutting down tracing provider")
	shutdownCtxTracing, cancelTracing := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelTracing()

	if err := tracingProvider.Shutdown(shutdownCtxTracing); err != nil {
		debugLog.Printf("Tracing provider shutdown error: %v", err)
		warnf("tracing provider shutdown error: %v", err)
	} else {
		debugLog.Print("Tracing provider shut down successfully")
	}
}
