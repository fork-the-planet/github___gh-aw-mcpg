package tracing

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"

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
	return parseOTLPHeadersWithDecoder(raw, false)
}

// resolveExtraEndpoints returns the additional OTLP endpoints from the
// GH_AW_OTLP_ENDPOINTS environment variable as a slice of fully-qualified URL
// strings (with the signal path appended). Each URL is normalised using the
// same signal-path rules as resolveEndpoint. Empty and whitespace-only entries
// are skipped. Returns nil when the variable is unset or yields no valid URLs.
func resolveExtraEndpoints(cfg *config.TracingConfig) []string {
	endpointConfigs := resolveExtraEndpointConfigs(cfg)
	endpoints := make([]string, 0, len(endpointConfigs))
	for _, endpoint := range endpointConfigs {
		endpoints = append(endpoints, endpoint.URL)
	}
	if len(endpoints) == 0 {
		return nil
	}
	return endpoints
}

type extraEndpointConfig struct {
	URL     string
	Headers map[string]string
}

type extraEndpointJSONConfig struct {
	URL     string `json:"url"`
	Headers string `json:"headers"`
}

func resolveExtraEndpointConfigs(cfg *config.TracingConfig) []extraEndpointConfig {
	raw := strings.TrimSpace(os.Getenv("GH_AW_OTLP_ENDPOINTS"))
	if raw == "" {
		return nil
	}

	signalPath := ""
	if cfg != nil {
		signalPath = cfg.SignalPath
	}

	var endpoints []extraEndpointConfig
	if strings.HasPrefix(raw, "[") {
		endpoints = resolveJSONExtraEndpoints(raw, signalPath)
	} else {
		endpoints = resolveCommaSeparatedExtraEndpoints(raw, signalPath)
	}
	if len(endpoints) == 0 {
		return nil
	}
	logTracing.Printf("GH_AW_OTLP_ENDPOINTS: resolved %d extra endpoint(s)", len(endpoints))
	return endpoints
}

func resolveCommaSeparatedExtraEndpoints(raw, signalPath string) []extraEndpointConfig {
	var endpoints []extraEndpointConfig
	for _, ep := range strings.Split(raw, ",") {
		normalized := normalizeExtraEndpoint(ep, signalPath)
		if normalized == "" {
			continue
		}
		endpoints = append(endpoints, extraEndpointConfig{URL: normalized})
	}
	return endpoints
}

func resolveJSONExtraEndpoints(raw, signalPath string) []extraEndpointConfig {
	var parsed []extraEndpointJSONConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		logTracing.Printf("Warning: failed to parse GH_AW_OTLP_ENDPOINTS as JSON array: %v", err)
		return nil
	}

	endpoints := make([]extraEndpointConfig, 0, len(parsed))
	for _, endpoint := range parsed {
		normalized := normalizeExtraEndpoint(endpoint.URL, signalPath)
		if normalized == "" {
			continue
		}

		resolved := extraEndpointConfig{URL: normalized}
		if endpoint.Headers != "" {
			if headers := parseOTLPHeadersWithDecoder(endpoint.Headers, true); len(headers) > 0 {
				resolved.Headers = headers
			}
		}
		endpoints = append(endpoints, resolved)
	}
	return endpoints
}

func normalizeExtraEndpoint(endpoint, signalPath string) string {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		return ""
	}
	return resolveEndpoint(&config.TracingConfig{
		Endpoint:   ep,
		SignalPath: signalPath,
	})
}
