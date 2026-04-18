package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/config"
)

func TestRegisterTracingFlags_DefaultsFromEnvAndConfig(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	t.Setenv("OTEL_SERVICE_NAME", "test-service")

	cmd := &cobra.Command{Use: "test"}

	var endpoint string
	var service string
	var sampleRate float64

	registerTracingFlags(
		cmd.Flags(),
		&endpoint,
		&service,
		&sampleRate,
		"endpoint help",
		"service help",
		"sample help",
	)

	actualEndpoint, err := cmd.Flags().GetString("otlp-endpoint")
	require.NoError(t, err)
	assert.Equal(t, "http://collector:4318", actualEndpoint)

	actualService, err := cmd.Flags().GetString("otlp-service-name")
	require.NoError(t, err)
	assert.Equal(t, "test-service", actualService)

	actualSampleRate, err := cmd.Flags().GetFloat64("otlp-sample-rate")
	require.NoError(t, err)
	assert.Equal(t, config.DefaultTracingSampleRate, actualSampleRate)
}
