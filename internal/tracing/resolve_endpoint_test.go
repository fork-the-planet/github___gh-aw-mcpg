package tracing

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestResolveEndpoint_AppendsV1Traces(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{
			name:     "URL without /v1/traces",
			endpoint: "https://o123.ingest.us.sentry.io/api/456/envelope",
			want:     "https://o123.ingest.us.sentry.io/api/456/envelope/v1/traces",
		},
		{
			name:     "URL with trailing slash",
			endpoint: "https://o123.ingest.us.sentry.io/api/456/envelope/",
			want:     "https://o123.ingest.us.sentry.io/api/456/envelope/v1/traces",
		},
		{
			name:     "already has /v1/traces",
			endpoint: "https://o123.ingest.us.sentry.io/api/456/envelope/v1/traces",
			want:     "https://o123.ingest.us.sentry.io/api/456/envelope/v1/traces",
		},
		{
			name:     "already has /v1/traces with trailing slash",
			endpoint: "https://o123.ingest.us.sentry.io/api/456/envelope/v1/traces/",
			want:     "https://o123.ingest.us.sentry.io/api/456/envelope/v1/traces",
		},
		{
			name:     "simple localhost endpoint",
			endpoint: "http://localhost:4318",
			want:     "http://localhost:4318/v1/traces",
		},
		{
			name:     "localhost with trailing slash",
			endpoint: "http://localhost:4318/",
			want:     "http://localhost:4318/v1/traces",
		},
		{
			name:     "URL with query parameters preserved",
			endpoint: "https://collector.example.com/ingest?token=abc",
			want:     "https://collector.example.com/ingest/v1/traces?token=abc",
		},
		{
			name:     "URL with fragment preserved",
			endpoint: "https://collector.example.com/ingest#section",
			want:     "https://collector.example.com/ingest/v1/traces#section",
		},
		{
			name:     "empty endpoint",
			endpoint: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.TracingConfig{Endpoint: tt.endpoint}
			if tt.endpoint == "" {
				cfg = &config.TracingConfig{}
			}
			got := resolveEndpoint(cfg)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveEndpoint_NilConfig(t *testing.T) {
	got := resolveEndpoint(nil)
	assert.Equal(t, "", got)
}

func TestResolveEndpoint_ParseErrorFallback(t *testing.T) {
	// A control character makes url.Parse return an error, exercising the fallback.
	cfg := &config.TracingConfig{Endpoint: "http://host\x7f:4318/path"}
	got := resolveEndpoint(cfg)
	assert.Equal(t, "http://host\x7f:4318/path/v1/traces", got)
}

func TestResolveEndpoint_ParseErrorFallbackAlreadyHasPath(t *testing.T) {
	cfg := &config.TracingConfig{Endpoint: "http://host\x7f:4318/v1/traces/"}
	got := resolveEndpoint(cfg)
	assert.Equal(t, "http://host\x7f:4318/v1/traces", got)
}

func TestResolveEndpoint_CustomSignalPath(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		signalPath string
		want       string
	}{
		{
			name:       "custom signal path appended",
			endpoint:   "https://collector.example.com/ingest",
			signalPath: "/v2/traces",
			want:       "https://collector.example.com/ingest/v2/traces",
		},
		{
			name:       "custom signal path already present",
			endpoint:   "https://collector.example.com/ingest/v2/traces",
			signalPath: "/v2/traces",
			want:       "https://collector.example.com/ingest/v2/traces",
		},
		{
			name:       "empty signal path uses default",
			endpoint:   "https://collector.example.com",
			signalPath: "",
			want:       "https://collector.example.com/v1/traces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.TracingConfig{
				Endpoint:   tt.endpoint,
				SignalPath: tt.signalPath,
			}
			got := resolveEndpoint(cfg)
			assert.Equal(t, tt.want, got)
		})
	}
}
