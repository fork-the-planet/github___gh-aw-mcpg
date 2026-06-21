// Package tracing provides OpenTelemetry OTLP trace export for the MCP Gateway.
// This file defines gen_ai semantic convention attribute keys per
// https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/
package tracing

import (
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// GenAI semantic convention attribute keys.
// Most are aliases for the official OpenTelemetry gen_ai semconv constants
// (semconv/v1.41.0), re-exported here for convenience. GenAISystem is the
// exception: it was removed in semconv/v1.41.0 and is defined as a raw
// attribute.Key to preserve wire compatibility.
const (
	// GenAISystem identifies the GenAI system family for MCP spans.
	// gen_ai.system was removed from semconv/v1.41.0; the key string is preserved for compatibility.
	GenAISystem = attribute.Key("gen_ai.system")

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

// MCP-specific attribute keys (no gen_ai equivalent in the spec).
const (
	// MCPMethod is the JSON-RPC method name (e.g. "tools/call").
	MCPMethod = attribute.Key("mcp.method")

	// MCPResponseStatus is the conceptual HTTP status of the proxied MCP response.
	// Used on internal MCP spans (e.g. mcp.tool_call) instead of the HTTP-specific
	// semconv.HTTPResponseStatusCodeKey, which is semantically reserved for HTTP spans.
	MCPResponseStatus = attribute.Key("mcp.response.status")

	// RateLimitHit indicates a rate limit was triggered.
	RateLimitHit = attribute.Key("rate_limit.hit")

	// GatewayTag is the routing mode tag (unified/routed).
	GatewayTag = attribute.Key("gateway.tag")
)
