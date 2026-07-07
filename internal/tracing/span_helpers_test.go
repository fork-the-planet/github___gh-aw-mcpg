package tracing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
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
	assert.True(t, hasAttr(recorded.Attributes, ErrorTypeKey, "*errors.errorString"))

	var foundStackTrace bool
	for _, event := range recorded.Events {
		if event.Name != "exception" {
			continue
		}
		for _, attr := range event.Attributes {
			if attr.Key == "exception.stacktrace" {
				assert.NotEmpty(t, attr.Value.AsString(), "exception event should include stacktrace")
				foundStackTrace = true
			}
		}
	}
	assert.True(t, foundStackTrace, "exception event should include stacktrace")
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

func TestRecordSpanErrorSafe_RecordsPublicMsgNotInternalError(t *testing.T) {
	span, getSpans := newRecordingSpan(t, "op")
	// Use a distinctive string in the internal error to verify it is never surfaced.
	internalErr := errors.New("transport error: secret-token-12345 expired")
	publicMsg := "tool execution failed"

	RecordSpanErrorSafe(span, internalErr, publicMsg)

	spans := getSpans()
	require.Len(t, spans, 1)
	recorded := spans[0]

	assert.Equal(t, "Error", recorded.Status.Code.String(), "span status should be Error")
	assert.Equal(t, publicMsg, recorded.Status.Description)

	// Verify the public message — not the internal error — is what's recorded on the span.
	require.NotEmpty(t, recorded.Events)
	exceptionEvent := recorded.Events[0]
	assert.Equal(t, "exception", exceptionEvent.Name)
	for _, attr := range exceptionEvent.Attributes {
		if attr.Key == "exception.message" {
			assert.Equal(t, publicMsg, attr.Value.AsString(),
				"exception.message should be the public message, not the internal error")
			assert.NotContains(t, attr.Value.AsString(), "secret-token-12345",
				"internal error details must not leak to the trace backend")
		}
	}
}

func TestRecordSpanErrorSafe_SetsErrorTypeFromPublicErr(t *testing.T) {
	span, getSpans := newRecordingSpan(t, "op")

	RecordSpanErrorSafe(span, errors.New("sensitive internal details"), "tool execution failed")

	spans := getSpans()
	require.Len(t, spans, 1)
	// error.type attribute should reflect *errors.errorString (the public error type),
	// not expose anything from the internal error.
	assert.True(t, hasAttr(spans[0].Attributes, ErrorTypeKey, "*errors.errorString"))
}

// newRecordingTracer creates an in-memory tracer provider and returns the tracer
// together with a function that flushes and returns all recorded spans.
func newRecordingTracer(t *testing.T) (oteltrace.Tracer, func() []tracetest.SpanStub) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	sp := sdktrace.NewSimpleSpanProcessor(exporter)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })
	return tp.Tracer("test"), func() []tracetest.SpanStub { return exporter.GetSpans() }
}

// hasAttr returns true when the attribute with key k and string value v is present in attrs.
func hasAttr(attrs []attribute.KeyValue, k attribute.Key, v string) bool {
	for _, a := range attrs {
		if a.Key == k && a.Value.AsString() == v {
			return true
		}
	}
	return false
}

func TestStartToolCallSpan(t *testing.T) {
	tracer, getSpans := newRecordingTracer(t)

	_, span := StartToolCallSpan(context.Background(), tracer, "srv1", "my_tool")
	span.End()

	spans := getSpans()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, "mcp.tool_call", s.Name)
	assert.Equal(t, oteltrace.SpanKindInternal, s.SpanKind)
	assert.True(t, hasAttr(s.Attributes, GenAISystem, "mcp"))
	assert.True(t, hasAttr(s.Attributes, GenAIOperationName, "execute_tool"))
	assert.True(t, hasAttr(s.Attributes, GenAIAgentName, "mcp-gateway"))
	assert.True(t, hasAttr(s.Attributes, GenAIAgentID, "srv1"))
	assert.True(t, hasAttr(s.Attributes, MCPMethod, "tools/call"))
	assert.True(t, hasAttr(s.Attributes, GenAIToolName, "my_tool"))
}

func TestStartBackendExecuteSpan(t *testing.T) {
	tracer, getSpans := newRecordingTracer(t)

	_, span := StartBackendExecuteSpan(context.Background(), tracer, "srv1", "my_tool")
	span.End()

	spans := getSpans()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, "gateway.backend.execute", s.Name)
	assert.Equal(t, oteltrace.SpanKindClient, s.SpanKind)
	assert.True(t, hasAttr(s.Attributes, GenAIToolName, "my_tool"))
	assert.True(t, hasAttr(s.Attributes, GenAIAgentID, "srv1"))
}

func TestStartDIFCPipelineSpan(t *testing.T) {
	tracer, getSpans := newRecordingTracer(t)

	_, span := StartDIFCPipelineSpan(context.Background(), tracer, "my_tool", "/api/v3/repos")
	span.End()

	spans := getSpans()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, "proxy.difc_pipeline", s.Name)
	assert.Equal(t, oteltrace.SpanKindInternal, s.SpanKind)
	assert.True(t, hasAttr(s.Attributes, GenAIToolName, "my_tool"))
	assert.True(t, hasAttr(s.Attributes, URLPathKey, "/api/v3/repos"))
}

func TestStartProxyForwardSpan(t *testing.T) {
	tracer, getSpans := newRecordingTracer(t)

	_, span := StartProxyForwardSpan(context.Background(), tracer, "my_tool", "/api/v3/repos", "api.github.com")
	span.End()

	spans := getSpans()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, "proxy.backend.forward", s.Name)
	assert.Equal(t, oteltrace.SpanKindClient, s.SpanKind)
	assert.True(t, hasAttr(s.Attributes, URLPathKey, "/api/v3/repos"))
	assert.True(t, hasAttr(s.Attributes, ServerAddressKey, "api.github.com"))
	assert.True(t, hasAttr(s.Attributes, GenAIToolName, "my_tool"))
}
