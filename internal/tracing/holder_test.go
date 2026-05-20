package tracing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/github/gh-aw-mcpg/internal/tracing"
)

func TestCachedTracer_GetTracer_ReturnsCached(t *testing.T) {
	cached := noop.NewTracerProvider().Tracer("cached")
	holder := tracing.CachedTracer{Tracer: cached}
	assert.Equal(t, cached, holder.GetTracer())
}

func TestCachedTracer_GetTracer_WithNilCachedTracer_ReturnsNonNilTracer(t *testing.T) {
	holder := tracing.CachedTracer{}
	assert.NotNil(t, holder.GetTracer())
}
