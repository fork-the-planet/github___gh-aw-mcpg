package tracing

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestResolveExtraEndpoints_NotSet returns nil when GH_AW_OTLP_ENDPOINTS is unset.
func TestResolveExtraEndpoints_NotSet(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", "")
	got := resolveExtraEndpoints(nil)
	assert.Nil(t, got)
}

// TestResolveExtraEndpoints_SingleEndpoint parses one URL and appends /v1/traces.
func TestResolveExtraEndpoints_SingleEndpoint(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", "http://collector.example.com:4318")
	got := resolveExtraEndpoints(nil)
	require.Len(t, got, 1)
	assert.Equal(t, "http://collector.example.com:4318/v1/traces", got[0])
}

// TestResolveExtraEndpoints_MultipleEndpoints parses several comma-separated URLs.
func TestResolveExtraEndpoints_MultipleEndpoints(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", "http://ep1:4318,https://ep2.example.com")
	got := resolveExtraEndpoints(nil)
	require.Len(t, got, 2)
	assert.Equal(t, "http://ep1:4318/v1/traces", got[0])
	assert.Equal(t, "https://ep2.example.com/v1/traces", got[1])
}

// TestResolveExtraEndpoints_SkipsEmpty skips empty entries.
func TestResolveExtraEndpoints_SkipsEmpty(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", "http://ep1:4318,,  ,http://ep2:4318")
	got := resolveExtraEndpoints(nil)
	require.Len(t, got, 2)
	assert.Equal(t, "http://ep1:4318/v1/traces", got[0])
	assert.Equal(t, "http://ep2:4318/v1/traces", got[1])
}

// TestResolveExtraEndpoints_AllEmpty returns nil when all entries are whitespace/empty.
func TestResolveExtraEndpoints_AllEmpty(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", "  ,  ,")
	got := resolveExtraEndpoints(nil)
	assert.Nil(t, got)
}

// TestResolveExtraEndpoints_RespectsSignalPath uses the custom signal path from cfg.
func TestResolveExtraEndpoints_RespectsSignalPath(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", "http://collector.example.com:4318")
	cfg := &config.TracingConfig{SignalPath: "/v2/traces"}
	got := resolveExtraEndpoints(cfg)
	require.Len(t, got, 1)
	assert.Equal(t, "http://collector.example.com:4318/v2/traces", got[0])
}

// TestResolveExtraEndpoints_AlreadyHasSignalPath does not duplicate the path.
func TestResolveExtraEndpoints_AlreadyHasSignalPath(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", "http://collector.example.com:4318/v1/traces")
	got := resolveExtraEndpoints(nil)
	require.Len(t, got, 1)
	assert.Equal(t, "http://collector.example.com:4318/v1/traces", got[0])
}

func TestResolveExtraEndpoints_JSONArray(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", `[{"url":"http://ep1:4318","headers":"Authorization=******"},{"url":"https://ep2.example.com"}]`)
	got := resolveExtraEndpoints(nil)
	require.Len(t, got, 2)
	assert.Equal(t, "http://ep1:4318/v1/traces", got[0])
	assert.Equal(t, "https://ep2.example.com/v1/traces", got[1])
}

func TestResolveExtraEndpointConfigs_JSONHeaders(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", `[{"url":"http://collector.example.com:4318","headers":" Authorization = ****** , X-Trace-Id = trace-123 "}]`)
	got := resolveExtraEndpointConfigs(nil)
	require.Len(t, got, 1)
	assert.Equal(t, "http://collector.example.com:4318/v1/traces", got[0].URL)
	assert.Equal(t, map[string]string{
		"Authorization": "******",
		"X-Trace-Id":    "trace-123",
	}, got[0].Headers)
}

func TestResolveExtraEndpoints_InvalidJSONReturnsNil(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", `[{"url":"http://collector.example.com:4318"`)
	got := resolveExtraEndpoints(nil)
	assert.Nil(t, got)
}

func TestResolveExtraEndpoints_EmptyJSONArrayReturnsNil(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", `[]`)
	got := resolveExtraEndpoints(nil)
	assert.Nil(t, got)
}

func TestNormalizeExtraEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		signalPath string
		want       string
	}{
		{
			name:     "empty string returns empty",
			endpoint: "",
			want:     "",
		},
		{
			name:     "whitespace only returns empty",
			endpoint: "   ",
			want:     "",
		},
		{
			name:     "default signal path appended",
			endpoint: "http://collector.example.com:4318",
			want:     "http://collector.example.com:4318/v1/traces",
		},
		{
			name:       "custom signal path appended",
			endpoint:   "http://collector.example.com:4318",
			signalPath: "/v2/traces",
			want:       "http://collector.example.com:4318/v2/traces",
		},
		{
			name:     "invalid URL falls back to string append",
			endpoint: "http://host\x7f:4318/path",
			want:     "http://host\x7f:4318/path/v1/traces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeExtraEndpoint(tt.endpoint, tt.signalPath))
		})
	}
}

// TestResolveExtraEndpoints_JSONArray_EmptyURL verifies that a JSON-format
// GH_AW_OTLP_ENDPOINTS entry with an empty URL field is silently skipped
// while valid entries are still returned.
func TestResolveExtraEndpoints_JSONArray_EmptyURL(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", `[{"url":""},{"url":"http://ep1:4318"}]`)
	got := resolveExtraEndpoints(nil)
	require.Len(t, got, 1)
	assert.Equal(t, "http://ep1:4318/v1/traces", got[0])
}

// TestResolveExtraEndpoints_JSONArray_WhitespaceURL verifies that a JSON-format
// GH_AW_OTLP_ENDPOINTS entry with a whitespace-only URL field is silently skipped
// while valid entries are still returned.
func TestResolveExtraEndpoints_JSONArray_WhitespaceURL(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", `[{"url":"   "},{"url":"http://ep1:4318"}]`)
	got := resolveExtraEndpoints(nil)
	require.Len(t, got, 1)
	assert.Equal(t, "http://ep1:4318/v1/traces", got[0])
}

// TestResolveExtraEndpoints_JSONArray_AllEmptyURLsReturnsNil verifies that when
// all JSON-format GH_AW_OTLP_ENDPOINTS entries have empty or whitespace URLs,
// resolveExtraEndpoints returns nil (no valid endpoints).
func TestResolveExtraEndpoints_JSONArray_AllEmptyURLsReturnsNil(t *testing.T) {
	t.Setenv("GH_AW_OTLP_ENDPOINTS", `[{"url":""},{"url":"  "}]`)
	got := resolveExtraEndpoints(nil)
	assert.Nil(t, got)
}
