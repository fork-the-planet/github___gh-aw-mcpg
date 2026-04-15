package cmd

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestGetDefaultOTLPServiceName_DefaultIsCorrect(t *testing.T) {
	// Verify the default constant value hasn't changed unexpectedly.
	// "mcp-gateway" is the canonical service name used in OTLP traces.
	assert.Equal(t, "mcp-gateway", config.DefaultTracingServiceName,
		"DefaultTracingServiceName constant should remain 'mcp-gateway'")
}
