package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/github/gh-aw-mcpg/internal/logger/sanitize"
)

// JSONLLogger manages logging RPC messages to a JSONL file (one JSON object per line)
type JSONLLogger struct {
	logFile  *os.File
	mu       sync.Mutex
	logDir   string
	fileName string
	encoder  *json.Encoder
}

var (
	globalJSONLLogger *JSONLLogger
	globalJSONLMu     sync.RWMutex
)

// JSONLRPCMessage represents a single RPC message log entry in JSONL format
type JSONLRPCMessage struct {
	Timestamp      string          `json:"timestamp"`
	Direction      string          `json:"direction"` // "IN" or "OUT"
	Type           string          `json:"type"`      // "REQUEST" or "RESPONSE"
	ServerID       string          `json:"server_id"`
	Method         string          `json:"method,omitempty"`
	Error          string          `json:"error,omitempty"`
	AgentSecrecy   []string        `json:"agent_secrecy,omitempty"`
	AgentIntegrity []string        `json:"agent_integrity,omitempty"`
	Payload        json.RawMessage `json:"payload"` // Full sanitized payload as raw JSON
}

// InitJSONLLogger initializes the global JSONL logger
func InitJSONLLogger(logDir, fileName string) error {
	logger, err := initLogger(
		logDir, fileName, os.O_APPEND,
		// Setup function: configure the logger after file is opened
		func(file *os.File, logDir, fileName string) (*JSONLLogger, error) {
			jl := &JSONLLogger{
				logDir:   logDir,
				fileName: fileName,
				logFile:  file,
				encoder:  json.NewEncoder(file),
			}
			return jl, nil
		},
		// Error handler: return error immediately (no fallback)
		func(err error, logDir, fileName string) (*JSONLLogger, error) {
			return nil, err
		},
	)

	// Only initialize global logger if successful (no error)
	// Unlike FileLogger/MarkdownLogger which return fallback loggers,
	// JSONLLogger has no fallback mode, so we should not initialize
	// the global logger when initialization fails
	if err == nil {
		initGlobalJSONLLogger(logger)
	}
	return err
}

// Close closes the JSONL log file
func (jl *JSONLLogger) Close() error {
	jl.mu.Lock()
	defer jl.mu.Unlock()

	return closeLogFile(jl.logFile, &jl.mu, "JSONL")
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

// CloseJSONLLogger closes the global JSONL logger
func CloseJSONLLogger() error {
	return closeGlobalJSONLLogger()
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
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Direction: string(direction),
			Type:      string(messageType),
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

// JSONLFilteredItem represents a DIFC-filtered item logged to the JSONL stream.
// These entries appear alongside RPC messages so filter events are visible
// in context with the request/response that triggered them.
type JSONLFilteredItem struct {
	Timestamp         string   `json:"timestamp"`
	Type              string   `json:"type"` // Always "DIFC_FILTERED"
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

// LogDifcFilteredItem writes a DIFC filter event to the JSONL log.
func LogDifcFilteredItem(entry *JSONLFilteredItem) {
	if entry == nil {
		// Best-effort logging: avoid panicking on nil input.
		return
	}

	entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	entry.Type = "DIFC_FILTERED"
	withGlobalLogger(&globalJSONLMu, &globalJSONLLogger, func(logger *JSONLLogger) {
		if logger == nil {
			return
		}
		_ = logger.logEntry(entry)
	})
}
