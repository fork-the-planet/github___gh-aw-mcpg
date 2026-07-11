package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/github/gh-aw-mcpg/internal/sanitize"
)

// JSONLLogger manages logging RPC messages to a JSONL file (one JSON object per line)
type JSONLLogger struct {
	lockable
	logFile  *os.File
	logDir   string
	fileName string
	encoder  *json.Encoder
}

var (
	globalJSONLLogger *JSONLLogger
	globalJSONLMu     sync.RWMutex
)

const (
	jsonTimestampLayout = "2006-01-02T15:04:05.000Z07:00"
	rpcMessageSchemaV2  = "rpc-message/v2"
	difcSchemaV2        = "difc-filtered/v2"
	difcEventSchemaV1   = "difc-event/v1"
)

// JSONLRPCMessage represents a single RPC message log entry in JSONL format
type JSONLRPCMessage struct {
	Timestamp      string          `json:"timestamp"`
	Event          string          `json:"event"`     // "rpc_request" or "rpc_response"
	Schema         string          `json:"_schema"`   // "rpc-message/v2"
	Direction      string          `json:"direction"` // "IN" or "OUT"
	ServerID       string          `json:"server_id"`
	Method         string          `json:"method,omitempty"`
	Error          string          `json:"error,omitempty"`
	AgentSecrecy   []string        `json:"agent_secrecy,omitempty"`
	AgentIntegrity []string        `json:"agent_integrity,omitempty"`
	Payload        json.RawMessage `json:"payload"` // Full sanitized payload as raw JSON
}

// jsonlLoggerFactory bundles the setup and error-handler for JSONLLogger.
var jsonlLoggerFactory = newLoggerFactory(
	func(file *os.File, logDir, fileName string) (*JSONLLogger, error) {
		jl := &JSONLLogger{
			logDir:   logDir,
			fileName: fileName,
			logFile:  file,
			encoder:  json.NewEncoder(file),
		}
		return jl, nil
	},
	func(err error, _ string, _ string) (*JSONLLogger, error) {
		// JSONLLogger has no fallback mode — return the error immediately.
		return strictLoggerOnInitError[*JSONLLogger](err)
	},
)

// InitJSONLLogger initializes the global JSONL logger
func InitJSONLLogger(logDir, fileName string) error {
	return initAndSetGlobalLoggerOnSuccess(&globalJSONLMu, &globalJSONLLogger, logDir, fileName, os.O_APPEND, jsonlLoggerFactory)
}

// Close closes the JSONL log file
func (jl *JSONLLogger) Close() error {
	return jl.withLock(func() error {
		return closeLogFile(jl.logFile, &jl.mu, "JSONL")
	})
}

// LogMessage logs an RPC message to the JSONL file
func (jl *JSONLLogger) LogMessage(entry *JSONLRPCMessage) error {
	return jl.logEntry(entry)
}

// logEntry writes any JSON-serializable value as a single JSONL line.
func (jl *JSONLLogger) logEntry(entry interface{}) error {
	jl.mu.Lock()
	defer jl.mu.Unlock()

	if jl.logFile == nil {
		return fmt.Errorf("JSONL logger not initialized")
	}

	if err := jl.encoder.Encode(entry); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	if err := jl.logFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync log file: %w", err)
	}

	return nil
}

// LogRPCMessageJSONL logs an RPC message to the global JSONL logger
func LogRPCMessageJSONL(direction RPCMessageDirection, messageType RPCMessageType, serverID, method string, payloadBytes []byte, err error) {
	LogRPCMessageJSONLWithTags(direction, messageType, serverID, method, payloadBytes, err, nil, nil)
}

// LogRPCMessageJSONLWithTags logs an RPC message to the global JSONL logger with optional agent tag snapshots.
// It uses the withGlobalLogger helper from global_helpers.go to handle mutex locking and nil-checking.
func LogRPCMessageJSONLWithTags(direction RPCMessageDirection, messageType RPCMessageType, serverID, method string, payloadBytes []byte, err error, agentSecrecy, agentIntegrity []string) {
	withGlobalLogger(&globalJSONLMu, &globalJSONLLogger, func(logger *JSONLLogger) {
		entry := &JSONLRPCMessage{
			Timestamp: time.Now().UTC().Format(jsonTimestampLayout),
			Event:     messageType.JSONLEvent(),
			Schema:    rpcMessageSchemaV2,
			Direction: string(direction),
			ServerID:  serverID,
			Method:    method,
			Payload:   sanitize.SanitizeJSON(payloadBytes),
		}

		if len(agentSecrecy) > 0 {
			entry.AgentSecrecy = append([]string(nil), agentSecrecy...)
		}
		if len(agentIntegrity) > 0 {
			entry.AgentIntegrity = append([]string(nil), agentIntegrity...)
		}

		if err != nil {
			entry.Error = err.Error()
		}

		// Best effort logging - don't fail if JSONL logging fails
		_ = logger.LogMessage(entry)
	})
}

// FilteredItemLogEntry holds the data fields for a DIFC-filtered item.
// It is used for both text log output ([DIFC-FILTERED] JSON lines) and as
// the embedded payload in JSONLFilteredItem for JSONL log output.
type FilteredItemLogEntry struct {
	ServerID          string   `json:"server_id"`
	ToolName          string   `json:"tool_name"`
	Description       string   `json:"description"`
	Reason            string   `json:"reason"`
	SecrecyTags       []string `json:"secrecy_tags"`
	IntegrityTags     []string `json:"integrity_tags"`
	AuthorAssociation string   `json:"author_association,omitempty"`
	AuthorLogin       string   `json:"author_login,omitempty"`
	HTMLURL           string   `json:"html_url,omitempty"`
	Number            string   `json:"number,omitempty"`
	SHA               string   `json:"sha,omitempty"`
}

// JSONLFilteredItem represents a DIFC-filtered item logged to the JSONL stream.
// These entries appear alongside RPC messages so filter events are visible
// in context with the request/response that triggered them.
// It embeds FilteredItemLogEntry so that adding a data field requires only a
// single edit rather than parallel edits in two structs.
type JSONLFilteredItem struct {
	Timestamp string `json:"timestamp"`
	Event     string `json:"event"`   // Always "difc_filtered"
	Schema    string `json:"_schema"` // "difc-filtered/v2"
	FilteredItemLogEntry
}

// JSONLUnrecognizedEndpointPassthrough records when the proxy forwards an
// unrecognized endpoint with empty DIFC labels.
type JSONLUnrecognizedEndpointPassthrough struct {
	Timestamp string `json:"timestamp"`
	Event     string `json:"event"`
	Schema    string `json:"_schema"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Action    string `json:"action"`
	Note      string `json:"note"`
}

// LogDifcFilteredItem writes a DIFC filter event to the JSONL log.
func LogDifcFilteredItem(entry *JSONLFilteredItem) {
	if entry == nil {
		// Best-effort logging: avoid panicking on nil input.
		return
	}

	entry.Timestamp = time.Now().UTC().Format(jsonTimestampLayout)
	entry.Event = "difc_filtered"
	entry.Schema = difcSchemaV2
	withGlobalLogger(&globalJSONLMu, &globalJSONLLogger, func(logger *JSONLLogger) {
		if logger == nil {
			return
		}
		_ = logger.logEntry(entry)
	})
}

// LogUnrecognizedEndpointPassthrough writes the proxy's unrecognized-endpoint
// passthrough event to the standard text log, markdown artifact log, and JSONL log.
func LogUnrecognizedEndpointPassthrough(method, path string) {
	entry := &JSONLUnrecognizedEndpointPassthrough{
		Timestamp: time.Now().UTC().Format(jsonTimestampLayout),
		Event:     "unrecognized_endpoint_passthrough",
		Schema:    difcEventSchemaV1,
		Method:    method,
		Path:      path,
		Action:    "passthrough_with_empty_labels",
		Note:      "Endpoint not in route table or metadata allowlist -- forwarded with no integrity and no secrecy labels",
	}

	if b, err := json.Marshal(entry); err == nil {
		LogWarnToMarkdown("proxy", "[UNRECOGNIZED-ENDPOINT] %s", string(b))
	} else {
		LogWarnToMarkdown("proxy", "failed to marshal unrecognized endpoint event for %s %s: %v", method, path, err)
	}

	withGlobalLogger(&globalJSONLMu, &globalJSONLLogger, func(logger *JSONLLogger) {
		if logger == nil {
			return
		}
		_ = logger.logEntry(entry)
	})
}
