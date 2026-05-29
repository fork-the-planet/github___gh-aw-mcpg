package tracing

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// newRecordingSpan creates an in-memory tracer, starts a span named spanName,
// and returns the span together with a function that flushes and returns all
// recorded spans from the exporter.
func newRecordingSpan(t *testing.T, spanName string) (sdktrace.ReadWriteSpan, func() []tracetest.SpanStub) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	sp := sdktrace.NewSimpleSpanProcessor(exporter)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	tracer := tp.Tracer("test")
	_, span := tracer.Start(t.Context(), spanName)
	recordingSpan, ok := span.(sdktrace.ReadWriteSpan)
	require.True(t, ok, "expected span to implement ReadWriteSpan")

	return recordingSpan, func() []tracetest.SpanStub {
		span.End()
		return exporter.GetSpans()
	}
}

func TestRecordSpanError_SetsStatusAndRecordsEvent(t *testing.T) {
	span, getSpans := newRecordingSpan(t, "op")
	testErr := errors.New("something went wrong")

	RecordSpanError(span, testErr, "test failure")

	spans := getSpans()
	require.Len(t, spans, 1)
	recorded := spans[0]

	assert.Equal(t, "Error", recorded.Status.Code.String(), "span status should be Error")
	assert.Equal(t, "test failure", recorded.Status.Description)

	require.NotEmpty(t, recorded.Events, "span should have at least one event")
	assert.Equal(t, "exception", recorded.Events[0].Name)
}

func TestRecordSpanErrorOnAll_RecordsOnAllSpans(t *testing.T) {
	span1, getSpans1 := newRecordingSpan(t, "span1")
	span2, getSpans2 := newRecordingSpan(t, "span2")
	testErr := errors.New("multi-span failure")

	RecordSpanErrorOnAll(testErr, "multi failure", span1, span2)

	for _, getSpans := range []func() []tracetest.SpanStub{getSpans1, getSpans2} {
		spans := getSpans()
		require.Len(t, spans, 1)
		recorded := spans[0]
		assert.Equal(t, "Error", recorded.Status.Code.String())
		assert.Equal(t, "multi failure", recorded.Status.Description)
		require.NotEmpty(t, recorded.Events)
		assert.Equal(t, "exception", recorded.Events[0].Name)
	}
}

func TestRecordSpanErrorOnAll_NoSpans_DoesNotPanic(t *testing.T) {
	testErr := errors.New("no spans")
	assert.NotPanics(t, func() {
		RecordSpanErrorOnAll(testErr, "no-op")
	})
}

func TestRecordSpanErrorOnAll_SingleSpan_BehavesLikeRecordSpanError(t *testing.T) {
	span, getSpans := newRecordingSpan(t, "single")
	testErr := errors.New("single span error")

	RecordSpanErrorOnAll(testErr, "single span msg", span)

	spans := getSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "Error", spans[0].Status.Code.String())
	assert.Equal(t, "single span msg", spans[0].Status.Description)
}
