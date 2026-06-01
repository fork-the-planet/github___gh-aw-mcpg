// Package tracing provides OpenTelemetry OTLP trace export for the MCP Gateway.
// This file provides span error recording helpers and span start constructors.
package tracing

import (
	"context"

	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// RecordSpanError records err on span with a stack trace and sets the span status to Error.
// Use this instead of calling RecordError + SetStatus individually to ensure consistent
// behavior (stack traces enabled, status always set) across all error paths.
func RecordSpanError(span oteltrace.Span, err error, msg string) {
	logTracing.Printf("Recording span error: msg=%s, err=%v", msg, err)
	span.RecordError(err, oteltrace.WithStackTrace(true))
	span.SetStatus(codes.Error, msg)
}

// RecordSpanErrorOnAll records err on all provided spans with a stack trace and sets their
// status to Error. Useful when both a parent and child span must reflect the same failure.
func RecordSpanErrorOnAll(err error, msg string, spans ...oteltrace.Span) {
	logTracing.Printf("Recording span error on %d spans: msg=%s", len(spans), msg)
	for _, span := range spans {
		RecordSpanError(span, err, msg)
	}
}

// StartToolCallSpan starts the outer tool-call OTEL span with standard gen_ai attributes.
// It covers the full tool call lifecycle (all DIFC pipeline phases) in unified server mode.
func StartToolCallSpan(ctx context.Context, tracer oteltrace.Tracer, serverID, toolName string) (context.Context, oteltrace.Span) {
	logTracing.Printf("Starting tool call span: serverID=%s, toolName=%s", serverID, toolName)
	return tracer.Start(ctx, "mcp.tool_call",
		oteltrace.WithAttributes(
			GenAIAgentID.String(serverID),
			MCPMethod.String("tools/call"),
			GenAIToolName.String(toolName),
		),
		oteltrace.WithSpanKind(oteltrace.SpanKindInternal),
	)
}

// StartBackendExecuteSpan starts the backend execution child span for the unified server.
// It is a client-kind span that covers the actual RPC to the backend MCP server.
func StartBackendExecuteSpan(ctx context.Context, tracer oteltrace.Tracer, serverID, toolName string) (context.Context, oteltrace.Span) {
	logTracing.Printf("Starting backend execute span: serverID=%s, toolName=%s", serverID, toolName)
	return tracer.Start(ctx, "gateway.backend.execute",
		oteltrace.WithAttributes(
			GenAIToolName.String(toolName),
			GenAIAgentID.String(serverID),
		),
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
	)
}

// StartDIFCPipelineSpan starts the DIFC pipeline OTEL span for the proxy handler.
// It covers all phases of the DIFC pipeline for a single proxied request.
func StartDIFCPipelineSpan(ctx context.Context, tracer oteltrace.Tracer, toolName, urlPath string) (context.Context, oteltrace.Span) {
	logTracing.Printf("Starting DIFC pipeline span: toolName=%s, urlPath=%s", toolName, urlPath)
	return tracer.Start(ctx, "proxy.difc_pipeline",
		oteltrace.WithAttributes(
			GenAIToolName.String(toolName),
			semconv.URLPathKey.String(urlPath),
		),
		oteltrace.WithSpanKind(oteltrace.SpanKindInternal),
	)
}

// StartProxyForwardSpan starts the backend forward child span for the proxy handler.
// It is a client-kind span that covers the HTTP request forwarded to the upstream API.
func StartProxyForwardSpan(ctx context.Context, tracer oteltrace.Tracer, toolName, urlPath string) (context.Context, oteltrace.Span) {
	logTracing.Printf("Starting proxy forward span: toolName=%s, urlPath=%s", toolName, urlPath)
	return tracer.Start(ctx, "proxy.backend.forward",
		oteltrace.WithAttributes(
			semconv.URLPathKey.String(urlPath),
			GenAIToolName.String(toolName),
		),
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
	)
}
