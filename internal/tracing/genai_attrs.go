// Package tracing provides OpenTelemetry OTLP trace export for the MCP Gateway.
// This file defines gen_ai semantic convention attribute keys per
// https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/
package tracing

import "go.opentelemetry.io/otel/attribute"

// GenAI semantic convention attribute keys.
// These follow the OpenTelemetry gen_ai specification (development stability).
const (
	// GenAIToolName is the name of the tool utilized by the agent.
	GenAIToolName = attribute.Key("gen_ai.tool.name")

	// GenAIOperationName is the name of the operation being performed.
	GenAIOperationName = attribute.Key("gen_ai.operation.name")

	// GenAIConversationID is the unique identifier for a conversation (session).
	GenAIConversationID = attribute.Key("gen_ai.conversation.id")

	// GenAIAgentName is the human-readable name of the GenAI agent.
	GenAIAgentName = attribute.Key("gen_ai.agent.name")

	// GenAIAgentID is the unique identifier of the GenAI agent (server ID).
	GenAIAgentID = attribute.Key("gen_ai.agent.id")
)

// MCP-specific attribute keys (no gen_ai equivalent in the spec).
const (
	// MCPMethod is the JSON-RPC method name (e.g. "tools/call").
	MCPMethod = attribute.Key("mcp.method")

	// RateLimitHit indicates a rate limit was triggered.
	RateLimitHit = attribute.Key("rate_limit.hit")

	// GatewayTag is the routing mode tag (unified/routed).
	GatewayTag = attribute.Key("gateway.tag")
)
