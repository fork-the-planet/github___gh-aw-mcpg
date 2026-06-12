package tracing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// stubExporter is a minimal SpanExporter that records calls for test assertions.
type stubExporter struct {
	exported    [][]sdktrace.ReadOnlySpan
	shutdowns   int
	exportErr   error
	shutdownErr error
}

func (s *stubExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	s.exported = append(s.exported, spans)
	return s.exportErr
}

func (s *stubExporter) Shutdown(_ context.Context) error {
	s.shutdowns++
	return s.shutdownErr
}

// TestNewFanoutExporter_SingleExporter returns the exporter directly.
func TestNewFanoutExporter_SingleExporter(t *testing.T) {
	exp := &stubExporter{}
	result := newFanoutExporter([]sdktrace.SpanExporter{exp})
	assert.Same(t, exp, result)
}

// TestNewFanoutExporter_MultipleExporters wraps in fanoutExporter.
func TestNewFanoutExporter_MultipleExporters(t *testing.T) {
	a, b := &stubExporter{}, &stubExporter{}
	result := newFanoutExporter([]sdktrace.SpanExporter{a, b})
	_, ok := result.(*fanoutExporter)
	assert.True(t, ok, "expected *fanoutExporter when more than one exporter is given")
}

// TestFanoutExporter_ExportSpans_AllReceiveSpans verifies that all exporters
// receive the same spans on each ExportSpans call.
func TestFanoutExporter_ExportSpans_AllReceiveSpans(t *testing.T) {
	ctx := context.Background()
	a, b := &stubExporter{}, &stubExporter{}
	exp := &fanoutExporter{exporters: []sdktrace.SpanExporter{a, b}}

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(ctx, "op")
	span.End()
	require.NoError(t, tp.Shutdown(ctx))

	spans := sr.Ended()
	require.NotEmpty(t, spans)

	err := exp.ExportSpans(ctx, spans)
	require.NoError(t, err)

	assert.Len(t, a.exported, 1, "exporter A should have received one batch")
	assert.Len(t, b.exported, 1, "exporter B should have received one batch")
	assert.Equal(t, spans, a.exported[0])
	assert.Equal(t, spans, b.exported[0])
}

// TestFanoutExporter_ExportSpans_ContinuesOnError verifies partial-failure
// tolerance: a failing exporter does not prevent delivery to later exporters.
func TestFanoutExporter_ExportSpans_ContinuesOnError(t *testing.T) {
	ctx := context.Background()
	failing := &stubExporter{exportErr: errors.New("backend unavailable")}
	ok := &stubExporter{}

	exp := &fanoutExporter{exporters: []sdktrace.SpanExporter{failing, ok}}

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	_, span := tp.Tracer("test").Start(ctx, "op")
	span.End()
	require.NoError(t, tp.Shutdown(ctx))

	spans := sr.Ended()
	err := exp.ExportSpans(ctx, spans)

	assert.Error(t, err, "error from failing exporter should be returned")
	assert.Contains(t, err.Error(), "backend unavailable")
	assert.Len(t, ok.exported, 1, "healthy exporter should still receive spans")
}

// TestFanoutExporter_ExportSpans_BothErrors returns all errors joined.
func TestFanoutExporter_ExportSpans_BothErrors(t *testing.T) {
	ctx := context.Background()
	e1 := &stubExporter{exportErr: errors.New("error one")}
	e2 := &stubExporter{exportErr: errors.New("error two")}
	exp := &fanoutExporter{exporters: []sdktrace.SpanExporter{e1, e2}}

	err := exp.ExportSpans(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error one")
	assert.Contains(t, err.Error(), "error two")
}

// TestFanoutExporter_Shutdown_CallsAll verifies Shutdown is forwarded to every exporter.
func TestFanoutExporter_Shutdown_CallsAll(t *testing.T) {
	ctx := context.Background()
	a, b, c := &stubExporter{}, &stubExporter{}, &stubExporter{}
	exp := &fanoutExporter{exporters: []sdktrace.SpanExporter{a, b, c}}

	require.NoError(t, exp.Shutdown(ctx))
	assert.Equal(t, 1, a.shutdowns)
	assert.Equal(t, 1, b.shutdowns)
	assert.Equal(t, 1, c.shutdowns)
}

// TestFanoutExporter_Shutdown_ContinuesOnError verifies that a Shutdown error
// does not prevent subsequent exporters from being shut down.
func TestFanoutExporter_Shutdown_ContinuesOnError(t *testing.T) {
	ctx := context.Background()
	failing := &stubExporter{shutdownErr: errors.New("shutdown error")}
	ok := &stubExporter{}
	exp := &fanoutExporter{exporters: []sdktrace.SpanExporter{failing, ok}}

	err := exp.Shutdown(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shutdown error")
	assert.Equal(t, 1, ok.shutdowns, "healthy exporter should still be shut down")
}

// TestFanoutExporter_ExportSpans_Empty verifies that ExportSpans with a nil/empty
// span slice is forwarded to the underlying exporter (which may be a no-op internally).
func TestFanoutExporter_ExportSpans_Empty(t *testing.T) {
	ctx := context.Background()
	a := &stubExporter{}
	exp := &fanoutExporter{exporters: []sdktrace.SpanExporter{a}}

	require.NoError(t, exp.ExportSpans(ctx, nil))
	assert.Len(t, a.exported, 1)
}
