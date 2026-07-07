// Package tracing provides OpenTelemetry OTLP trace export for the MCP Gateway.
// This file provides span error recording helpers and span start constructors.
package tracing

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// RecordSpanError records err on span with a stack trace and sets the span status to Error.
// Use this instead of calling RecordError + SetStatus individually to ensure consistent
// behavior (stack traces enabled, status always set) across all error paths.
func RecordSpanError(span oteltrace.Span, err error, msg string) {
	logTracing.Printf("Recording span error: msg=%s, err=%v", msg, err)
	span.RecordError(err, oteltrace.WithStackTrace(true))
	if err != nil {
		span.SetAttributes(ErrorType(err))
	}
	span.SetStatus(codes.Error, msg)
}

// RecordSpanErrorSafe records a scrubbed error on span for security-sensitive paths.
// internalErr is the actual error (logged and returned to the caller via normal channels);
// publicMsg is the message recorded on the span and set as the status description.
// This prevents internal error details from leaking to trace backends, which may be
// operated by third parties.
func RecordSpanErrorSafe(span oteltrace.Span, internalErr error, publicMsg string) {
	if !span.IsRecording() {
		return
	}
	logTracing.Printf("Recording span error (safe): msg=%s, internal=%v", publicMsg, internalErr)
	publicErr := errors.New(publicMsg)
	span.RecordError(publicErr, oteltrace.WithStackTrace(true))
	span.SetAttributes(ErrorType(publicErr))
	span.SetStatus(codes.Error, publicMsg)
}

// RecordSpanErrorOnAll records err on all provided spans with a stack trace and sets their
// status to Error. Useful when both a parent and child span must reflect the same failure.
func RecordSpanErrorOnAll(err error, msg string, spans ...oteltrace.Span) {
	logTracing.Printf("Recording span error on %d spans: msg=%s", len(spans), msg)
	for _, span := range spans {
		RecordSpanError(span, err, msg)
	}
}

// startSpan is the shared inner implementation used by all public Start*Span helpers.
// It starts a span with the given name, kind, and attributes.
func startSpan(ctx context.Context, tracer oteltrace.Tracer, spanName string, kind oteltrace.SpanKind, attrs ...attribute.KeyValue) (context.Context, oteltrace.Span) {
	return tracer.Start(ctx, spanName,
		oteltrace.WithAttributes(attrs...),
		oteltrace.WithSpanKind(kind),
	)
}

// StartToolCallSpan starts the outer tool-call OTEL span with standard gen_ai attributes.
// It covers the full tool call lifecycle (all DIFC pipeline phases) in unified server mode.
func StartToolCallSpan(ctx context.Context, tracer oteltrace.Tracer, serverID, toolName string) (context.Context, oteltrace.Span) {
	logTracing.Printf("Starting tool call span: serverID=%s, toolName=%s", serverID, toolName)
	return startSpan(ctx, tracer, "mcp.tool_call", oteltrace.SpanKindInternal,
		GenAISystem.String("mcp"),
		GenAIOperationName.String("execute_tool"),
		GenAIAgentName.String("mcp-gateway"),
		GenAIAgentID.String(serverID),
		MCPMethod.String("tools/call"),
		GenAIToolName.String(toolName),
	)
}

// StartBackendExecuteSpan starts the backend execution child span for the unified server.
// It is a client-kind span that covers the actual RPC to the backend MCP server.
func StartBackendExecuteSpan(ctx context.Context, tracer oteltrace.Tracer, serverID, toolName string) (context.Context, oteltrace.Span) {
	logTracing.Printf("Starting backend execute span: serverID=%s, toolName=%s", serverID, toolName)
	return startSpan(ctx, tracer, "gateway.backend.execute", oteltrace.SpanKindClient,
		GenAIToolName.String(toolName),
		GenAIAgentID.String(serverID),
	)
}

// StartDIFCPipelineSpan starts the DIFC pipeline OTEL span for the proxy handler.
// It covers all phases of the DIFC pipeline for a single proxied request.
func StartDIFCPipelineSpan(ctx context.Context, tracer oteltrace.Tracer, toolName, urlPath string) (context.Context, oteltrace.Span) {
	logTracing.Printf("Starting DIFC pipeline span: toolName=%s, urlPath=%s", toolName, urlPath)
	return startSpan(ctx, tracer, "proxy.difc_pipeline", oteltrace.SpanKindInternal,
		GenAIToolName.String(toolName),
		URLPathKey.String(urlPath),
	)
}

// StartProxyForwardSpan starts the backend forward child span for the proxy handler.
// It is a client-kind span that covers the HTTP request forwarded to the upstream API.
func StartProxyForwardSpan(ctx context.Context, tracer oteltrace.Tracer, toolName, urlPath, serverAddress string) (context.Context, oteltrace.Span) {
	logTracing.Printf("Starting proxy forward span: toolName=%s, urlPath=%s", toolName, urlPath)
	return startSpan(ctx, tracer, "proxy.backend.forward", oteltrace.SpanKindClient,
		URLPathKey.String(urlPath),
		ServerAddressKey.String(serverAddress),
		GenAIToolName.String(toolName),
	)
}
