// Package tracing provides OpenTelemetry OTLP trace export for the MCP Gateway.
// This file provides HTTP handler wrapping helpers.
package tracing

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/github/gh-aw-mcpg/internal/httputil"
)

// statusResponseWriter wraps http.ResponseWriter to capture the HTTP response
// status code. It embeds httputil.BaseResponseWriter which provides WriteHeader,
// Write (with implicit-200 capture), and Unwrap for transparent interface
// delegation (e.g. http.Flusher, http.Hijacker).
type statusResponseWriter struct {
	httputil.BaseResponseWriter
}

// WrapHTTPHandler wraps an http.Handler with an OpenTelemetry server span.
// A span named spanName is created for every request, with
// http.request.method and http.route set automatically. Extra attrs are merged in.
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

		route := r.Pattern
		if method, path, ok := strings.Cut(route, " "); ok {
			if strings.EqualFold(method, r.Method) {
				route = path
			} else {
				route = ""
			}
		}

		attrs := append([]attribute.KeyValue{
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.URLPathKey.String(r.URL.Path),
		}, extraAttrs...)
		if route != "" {
			attrs = append(attrs, semconv.HTTPRouteKey.String(route))
		}

		ctx, span := t.Start(ctx, spanName,
			oteltrace.WithAttributes(attrs...),
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
		)
		defer span.End()

		logTracing.Printf("Span started: span=%s, traceID=%s", spanName, span.SpanContext().TraceID())

		srw := &statusResponseWriter{BaseResponseWriter: httputil.BaseResponseWriter{ResponseWriter: w, StatusCode: http.StatusOK}}
		defer func() {
			span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(srw.StatusCode))
			if srw.StatusCode >= 500 {
				msg := http.StatusText(srw.StatusCode)
				if msg == "" {
					msg = fmt.Sprintf("HTTP %d", srw.StatusCode)
				}
				RecordSpanError(span, errors.New(msg), msg)
			}
		}()
		next.ServeHTTP(srw, r.WithContext(ctx))
	})
}
