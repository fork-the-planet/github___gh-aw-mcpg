// Package tracing provides OpenTelemetry OTLP trace export for the MCP Gateway.
// This file is the single source of truth for go.opentelemetry.io/otel/semconv/v1.41.0
// imports in the tracing package. All callers inside and outside this package should
// reference these re-exports so that upgrading semconv only requires editing this file.
package tracing

import (
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// SchemaURL is the semconv schema URL for this semantic conventions version.
const SchemaURL = semconv.SchemaURL

// HTTP and URL semantic convention attribute keys.
const (
	// HTTPRequestMethodKey is the HTTP request method.
	HTTPRequestMethodKey = semconv.HTTPRequestMethodKey
	// HTTPRouteKey is the matched HTTP route template.
	HTTPRouteKey = semconv.HTTPRouteKey
	// HTTPResponseStatusCodeKey is the HTTP response status code.
	HTTPResponseStatusCodeKey = semconv.HTTPResponseStatusCodeKey
	// URLPathKey is the full URL path.
	URLPathKey = semconv.URLPathKey
	// ServerAddressKey is the server domain name or IP address.
	ServerAddressKey = semconv.ServerAddressKey
)

// Error semantic convention keys.
const (
	// ErrorTypeKey is the attribute key for error.type.
	ErrorTypeKey = semconv.ErrorTypeKey
)

// GenAI semantic convention attribute keys (semconv/v1.41.0).
// gen_ai.system was removed from the spec; see GenAISystem in genai_attrs.go.
const (
	// GenAIToolName is the name of the tool utilized by the agent.
	GenAIToolName = semconv.GenAIToolNameKey
	// GenAIOperationName is the name of the operation being performed.
	GenAIOperationName = semconv.GenAIOperationNameKey
	// GenAIConversationID is the unique identifier for a conversation (session).
	GenAIConversationID = semconv.GenAIConversationIDKey
	// GenAIAgentName is the human-readable name of the GenAI agent.
	GenAIAgentName = semconv.GenAIAgentNameKey
	// GenAIAgentID is the unique identifier of the GenAI agent (server ID).
	GenAIAgentID = semconv.GenAIAgentIDKey
)

// ErrorType returns the error.type attribute KeyValue for err.
// It wraps semconv.ErrorType so callers outside this package can use it
// without importing the semconv package directly.
func ErrorType(err error) attribute.KeyValue {
	return semconv.ErrorType(err)
}

// ServiceName returns the service.name attribute KeyValue for val.
func ServiceName(val string) attribute.KeyValue {
	return semconv.ServiceName(val)
}

// ServiceVersion returns the service.version attribute KeyValue for val.
func ServiceVersion(val string) attribute.KeyValue {
	return semconv.ServiceVersion(val)
}
