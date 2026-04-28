package mcp

import (
	"encoding/json"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

// logReconnectStart emits the structured log warning that is common to all reconnect paths.
func (c *Connection) logReconnectStart() {
	logger.LogWarn("backend", "MCP session expired for %s, attempting to reconnect...", c.serverID)
}

// logReconnectResult emits the structured log entry that signals whether the reconnect
// succeeded or failed. It is the common success/failure telemetry shared by all reconnect paths.
func (c *Connection) logReconnectResult(err error) {
	if err != nil {
		logger.LogError("backend", "Session reconnect failed for %s: %v", c.serverID, err)
	} else {
		logger.LogInfo("backend", "Session successfully reconnected for %s", c.serverID)
	}
}

// logOutboundRPCRequest logs an outbound RPC request, optionally attaching agent DIFC tag snapshots.
// When shouldAttachTags is true, snapshot must be non-nil.
func logOutboundRPCRequest(serverID string, method string, payload []byte, shouldAttachTags bool, snapshot *AgentTagsSnapshot) {
	if shouldAttachTags {
		logger.LogRPCRequestWithAgentSnapshot(logger.RPCDirectionOutbound, serverID, method, payload, snapshot.Secrecy, snapshot.Integrity)
	} else {
		logger.LogRPCRequest(logger.RPCDirectionOutbound, serverID, method, payload)
	}
}

// logInboundRPCResponse logs an inbound RPC response, optionally attaching agent DIFC tag snapshots.
// When shouldAttachTags is true, snapshot must be non-nil.
func logInboundRPCResponse(serverID string, payload []byte, err error, shouldAttachTags bool, snapshot *AgentTagsSnapshot) {
	if shouldAttachTags {
		logger.LogRPCResponseWithAgentSnapshot(logger.RPCDirectionInbound, serverID, payload, err, snapshot.Secrecy, snapshot.Integrity)
	} else {
		logger.LogRPCResponse(logger.RPCDirectionInbound, serverID, payload, err)
	}
}

// logInboundRPCResponseFromResult attempts to marshal a response payload for logging,
// silently ignores marshal failures, logs the inbound response, and returns the
// original result and error unchanged.
func logInboundRPCResponseFromResult(serverID string, result *Response, err error, shouldAttachTags bool, snapshot *AgentTagsSnapshot) (*Response, error) {
	var responsePayload []byte
	if result != nil {
		responsePayload, _ = json.Marshal(result)
	}
	logInboundRPCResponse(serverID, responsePayload, err, shouldAttachTags, snapshot)
	return result, err
}
