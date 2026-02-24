// Package logger provides structured logging for the MCP Gateway.
//
// This file contains RPC message logging coordination, managing the flow of messages
// across multiple output formats (text, markdown, JSONL).
//
// File Organization:
//
// - rpc_logger.go (this file): Coordination of RPC logging across formats
// - rpc_formatter.go: Text and markdown formatting functions
// - rpc_helpers.go: Utility functions for payload processing
//
// The package supports logging RPC messages in three formats:
//
// 1. Text logs: Compact single-line format for grep-friendly searching
// 2. Markdown logs: Human-readable format with syntax highlighting
// 3. JSONL logs: Machine-readable format for structured analysis
//
// Example:
//
//	logger.LogRPCRequest(logger.RPCDirectionOutbound, "github", "tools/list", payload)
//	logger.LogRPCResponse(logger.RPCDirectionInbound, "github", responsePayload, nil)
package logger

// RPCMessageType represents the direction of an RPC message
type RPCMessageType string

const (
	// RPCMessageRequest represents an outbound request or inbound client request
	RPCMessageRequest RPCMessageType = "REQUEST"
	// RPCMessageResponse represents an inbound response from backend or outbound response to client
	RPCMessageResponse RPCMessageType = "RESPONSE"
)

// RPCMessageDirection represents whether the message is inbound or outbound
type RPCMessageDirection string

const (
	// RPCDirectionInbound represents messages coming into the gateway
	RPCDirectionInbound RPCMessageDirection = "IN"
	// RPCDirectionOutbound represents messages going out from the gateway
	RPCDirectionOutbound RPCMessageDirection = "OUT"
)

const (
	// MaxPayloadPreviewLengthText is the maximum number of characters to include in text log preview (10KB)
	MaxPayloadPreviewLengthText = 10 * 1024 // 10KB
	// MaxPayloadPreviewLengthMarkdown is the maximum number of characters to include in markdown log preview
	MaxPayloadPreviewLengthMarkdown = 512
)

// RPCMessageInfo contains information about an RPC message for logging
type RPCMessageInfo struct {
	Direction   RPCMessageDirection // IN or OUT
	MessageType RPCMessageType      // REQUEST or RESPONSE
	ServerID    string              // Backend server ID or "client" for client messages
	Method      string              // RPC method name (for requests)
	PayloadSize int                 // Size of the payload in bytes
	Payload     string              // First N characters of payload (sanitized)
	Error       string              // Error message if any (for responses)
}

// logRPCMessageToAll is a helper that logs RPC messages to text, markdown, and JSONL logs
func logRPCMessageToAll(direction RPCMessageDirection, messageType RPCMessageType, serverID, method string, payload []byte, err error, agentSecrecy, agentIntegrity []string) {
	// Create info for text log (with larger payload preview)
	infoText := &RPCMessageInfo{
		Direction:   direction,
		MessageType: messageType,
		ServerID:    serverID,
		Method:      method,
		PayloadSize: len(payload),
		Payload:     truncateAndSanitize(string(payload), MaxPayloadPreviewLengthText),
	}

	if err != nil {
		infoText.Error = err.Error()
	}

	// Log to text file
	LogDebug("rpc", "%s", formatRPCMessage(infoText))

	// Create info for markdown log (with shorter payload preview)
	infoMarkdown := &RPCMessageInfo{
		Direction:   direction,
		MessageType: messageType,
		ServerID:    serverID,
		Method:      method,
		PayloadSize: len(payload),
		Payload:     truncateAndSanitize(string(payload), MaxPayloadPreviewLengthMarkdown),
	}

	if err != nil {
		infoMarkdown.Error = err.Error()
	}

	// Log to markdown file
	globalMarkdownMu.RLock()
	defer globalMarkdownMu.RUnlock()

	if globalMarkdownLogger != nil {
		globalMarkdownLogger.Log(LogLevelDebug, "rpc", "%s", formatRPCMessageMarkdown(infoMarkdown))
	}

	// Log to JSONL file (full payload, sanitized)
	LogRPCMessageJSONLWithTags(direction, messageType, serverID, method, payload, err, agentSecrecy, agentIntegrity)
}

// LogRPCRequest logs an RPC request message to text, markdown, and JSONL logs
func LogRPCRequest(direction RPCMessageDirection, serverID, method string, payload []byte) {
	logRPCMessageToAll(direction, RPCMessageRequest, serverID, method, payload, nil, nil, nil)
}

// LogRPCResponse logs an RPC response message to text, markdown, and JSONL logs
func LogRPCResponse(direction RPCMessageDirection, serverID string, payload []byte, err error) {
	logRPCMessageToAll(direction, RPCMessageResponse, serverID, "", payload, err, nil, nil)
}

// LogRPCRequestWithAgentSnapshot logs an RPC request and includes agent DIFC tag snapshots in JSONL output.
func LogRPCRequestWithAgentSnapshot(direction RPCMessageDirection, serverID, method string, payload []byte, agentSecrecy, agentIntegrity []string) {
	logRPCMessageToAll(direction, RPCMessageRequest, serverID, method, payload, nil, agentSecrecy, agentIntegrity)
}

// LogRPCResponseWithAgentSnapshot logs an RPC response and includes agent DIFC tag snapshots in JSONL output.
func LogRPCResponseWithAgentSnapshot(direction RPCMessageDirection, serverID string, payload []byte, err error, agentSecrecy, agentIntegrity []string) {
	logRPCMessageToAll(direction, RPCMessageResponse, serverID, "", payload, err, agentSecrecy, agentIntegrity)
}

// LogRPCMessage logs a generic RPC message with custom info
func LogRPCMessage(info *RPCMessageInfo) {
	// Log to text file
	LogDebug("rpc", "%s", formatRPCMessage(info))

	// Log to markdown file
	globalMarkdownMu.RLock()
	defer globalMarkdownMu.RUnlock()

	if globalMarkdownLogger != nil {
		globalMarkdownLogger.Log(LogLevelDebug, "rpc", "%s", formatRPCMessageMarkdown(info))
	}
}
