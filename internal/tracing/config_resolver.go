package tracing

import (
	"context"
	"encoding/hex"
	"net/url"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/github/gh-aw-mcpg/internal/config"
)

// defaultSignalPath is the OTLP traces signal path per the OpenTelemetry spec.
const defaultSignalPath = "/v1/traces"

// resolveEndpoint returns the OTLP endpoint from config.
// CLI flags set the config value using env vars as defaults, so config already
// reflects the correct precedence: CLI flag > env var > config file.
//
// Per the OpenTelemetry specification, OTEL_EXPORTER_OTLP_ENDPOINT is a base URL
// and SDKs must append the signal path (/v1/traces for traces). Since we use
// WithEndpointURL (which takes the URL as-is), we append the signal path here
// when it is not already present. The path defaults to /v1/traces but can be
// overridden via TracingConfig.SignalPath.
func resolveEndpoint(cfg *config.TracingConfig) string {
	if cfg == nil || cfg.Endpoint == "" {
		return ""
	}
	endpoint := cfg.Endpoint
	signalPath := cfg.SignalPath
	if signalPath == "" {
		signalPath = defaultSignalPath
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		// If unparseable, fall back to string append for best-effort.
		// Normalize trailing slashes before the suffix check to avoid
		// duplicating the signal path when input already ends with it.
		normalized := strings.TrimRight(endpoint, "/")
		if !strings.HasSuffix(normalized, signalPath) {
			normalized += signalPath
		}
		return normalized
	}

	// Normalize path and check whether signal path is already the suffix
	normalizedPath := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(normalizedPath, signalPath) {
		u.Path = normalizedPath + signalPath
	} else {
		u.Path = normalizedPath
	}
	return u.String()
}

// resolveServiceName returns the service name from config.
func resolveServiceName(cfg *config.TracingConfig) string {
	if cfg != nil && cfg.ServiceName != "" {
		return cfg.ServiceName
	}
	return config.DefaultTracingServiceName
}

// resolveSampleRate returns the sample rate from config (defaults to 1.0).
// Valid configured values are in the range [0.0, 1.0], where 0.0 disables sampling.
func resolveSampleRate(cfg *config.TracingConfig) float64 {
	rate := cfg.GetSampleRate()

	if rate >= 0.0 && rate <= 1.0 {
		return rate
	}

	logTracing.Printf("Warning: invalid tracing sample rate %.4f; using default %.2f", rate, config.DefaultTracingSampleRate)
	return config.DefaultTracingSampleRate
}

// parseOTLPHeaders parses a comma-separated "key=value" string into a map.
// Empty pairs, pairs without "=", and pairs with an empty key are logged as
// warnings and skipped to avoid invalid HTTP header field names.
// Leading/trailing whitespace around keys and values is trimmed.
func parseOTLPHeaders(raw string) map[string]string {
	return parseOTLPHeadersWithDecoder(raw, false)
}

func parseOTLPHeadersWithDecoder(raw string, decodeValues bool) map[string]string {
	headers := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(pair)
		if trimmed == "" {
			continue
		}
		k, v, ok := strings.Cut(trimmed, "=")
		if !ok {
			logTracing.Printf("Warning: skipping malformed OTLP header pair (missing '=')")
			continue
		}
		key := strings.TrimSpace(k)
		if key == "" {
			logTracing.Printf("Warning: skipping OTLP header pair with empty key")
			continue
		}
		value := strings.TrimSpace(v)
		if decodeValues {
			decoded, err := url.PathUnescape(value)
			if err != nil {
				logTracing.Printf("Warning: invalid percent-encoding in OTLP header value for key %q; using raw value", key)
			} else {
				value = decoded
			}
		}
		headers[key] = value
	}
	return headers
}

// resolveHeaders parses the configured OTLP export headers string (or returns nil).
// When no headers are configured via config, it falls back to the standard
// OTEL_EXPORTER_OTLP_HEADERS environment variable (W3C Baggage format:
// "key1=value1,key2=value2") per the OTel OTLP Exporter specification.
func resolveHeaders(cfg *config.TracingConfig) map[string]string {
	raw := ""
	if cfg != nil {
		raw = cfg.Headers
	}
	if raw == "" {
		raw = os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")
		if raw != "" {
			logTracing.Printf("Using OTEL_EXPORTER_OTLP_HEADERS env var for OTLP export headers")
		}
	}
	if raw == "" {
		return nil
	}
	if cfg == nil || cfg.Headers == "" {
		return parseOTLPHeadersWithDecoder(raw, true)
	}
	return parseOTLPHeaders(raw)
}

// resolveParentContext builds a context carrying the W3C remote parent span context
// from the configured traceId and spanId (spec §4.1.3.6).
// If traceId is absent, or either ID is malformed, the original context is returned unchanged.
// A missing spanId is replaced with a random span ID so the traceparent is still valid.
func resolveParentContext(ctx context.Context, cfg *config.TracingConfig) context.Context {
	if cfg == nil || cfg.TraceID == "" {
		return ctx
	}

	traceIDBytes, err := hex.DecodeString(cfg.TraceID)
	if err != nil || len(traceIDBytes) != 16 {
		logTracing.Printf("Warning: invalid traceId '%s'; skipping W3C parent context", cfg.TraceID)
		return ctx
	}
	var traceID trace.TraceID
	copy(traceID[:], traceIDBytes)

	var spanID trace.SpanID
	if cfg.SpanID != "" {
		spanIDBytes, err := hex.DecodeString(cfg.SpanID)
		if err != nil || len(spanIDBytes) != 8 {
			logTracing.Printf("Warning: invalid spanId '%s'; generating a random span ID", cfg.SpanID)
			// Fall through to generate a random span ID below
		} else {
			copy(spanID[:], spanIDBytes)
		}
	}

	// When spanId is all-zeros (absent or invalid), generate a random span ID.
	// A valid SpanContext requires a non-zero SpanID (W3C Trace Context spec).
	// T-OTEL-008: when only traceId is provided, a random spanId is generated.
	if spanID == (trace.SpanID{}) {
		generatedID, genErr := generateRandomSpanID()
		if genErr != nil {
			logTracing.Printf("Warning: failed to generate random span ID: %v; skipping W3C parent context", genErr)
			return ctx
		}
		spanID = generatedID
		logTracing.Printf("Generated random spanId for W3C parent context")
	}

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	if !sc.IsValid() {
		logTracing.Printf("Warning: constructed parent SpanContext is not valid; skipping W3C parent context")
		return ctx
	}
	logTracing.Printf("W3C parent context resolved: traceId=%s, spanId=%s", traceID, spanID)
	return trace.ContextWithRemoteSpanContext(ctx, sc)
}
