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
			name:     "base URL without path",
			endpoint: "https://o123.ingest.us.sentry.io/api/456/envelope",
			want:     "https://o123.ingest.us.sentry.io/api/456/envelope/v1/traces",
		},
		{
			name:     "base URL with trailing slash",
			endpoint: "https://o123.ingest.us.sentry.io/api/456/envelope/",
			want:     "https://o123.ingest.us.sentry.io/api/456/envelope/v1/traces",
		},
		{
			name:     "already has /v1/traces",
			endpoint: "https://o123.ingest.us.sentry.io/api/456/envelope/v1/traces",
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
