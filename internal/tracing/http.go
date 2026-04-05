// Package tracing provides OpenTelemetry OTLP trace export for the MCP Gateway.
// This file provides HTTP handler wrapping helpers.
package tracing

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// WrapHTTPHandler wraps an http.Handler with an OpenTelemetry server span.
// A span named spanName is created for every request, with http.method and
// http.path set automatically. Extra attrs are merged in.
//
// Incoming W3C traceparent/tracestate headers are extracted so that an
// agent-originated trace is continued; if no such headers are present a fresh
// root span (and new trace ID) is created automatically.
//
// This is a low-level helper used by both the MCP gateway middleware and the
// GitHub API proxy. Callers that need session-level attributes (e.g. session.id)
// should add them as extra attrs or extend the context themselves.
func WrapHTTPHandler(next http.Handler, spanName string, extraAttrs ...attribute.KeyValue) http.Handler {
	logTracing.Printf("Registering HTTP handler with OTel span: span=%s, extraAttrs=%d", spanName, len(extraAttrs))
	t := Tracer()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract incoming W3C trace context (traceparent / tracestate).
		// If the headers are absent the returned ctx is unchanged and OTEL
		// will generate a fresh trace ID when the span is started.
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		hasRemoteParent := oteltrace.SpanContextFromContext(ctx).IsRemote()
		logTracing.Printf("Handling request: span=%s, method=%s, path=%s, remoteParent=%v", spanName, r.Method, r.URL.Path, hasRemoteParent)

		attrs := append([]attribute.KeyValue{
			attribute.String("http.method", r.Method),
			attribute.String("http.path", r.URL.Path),
		}, extraAttrs...)

		ctx, span := t.Start(ctx, spanName,
			oteltrace.WithAttributes(attrs...),
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
		)
		defer span.End()

		logTracing.Printf("Span started: span=%s, traceID=%s", spanName, span.SpanContext().TraceID())

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
