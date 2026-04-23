package logger

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/logger/sanitize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitJSONLLogger(t *testing.T) {
	require := require.New(t)
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	// Test successful initialization
	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	// Verify log file was created
	logPath := filepath.Join(logDir, "test.jsonl")
	_, err = os.Stat(logPath)
	require.NoError(err, "Log file should exist at %s", logPath)
}

func TestJSONLLoggerClose(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")

	// Test closing
	err = CloseJSONLLogger()
	assert.NoError(err, "CloseJSONLLogger should not error")

	// Test closing again (should not error)
	err = CloseJSONLLogger()
	assert.NoError(err, "CloseJSONLLogger should not error on second call")
}

func TestLogRPCMessageJSONL(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	// Log a request
	requestPayload := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	LogRPCMessageJSONL(RPCDirectionOutbound, RPCMessageRequest, "github", "tools/list", requestPayload, nil)

	// Log a response
	responsePayload := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)
	LogRPCMessageJSONL(RPCDirectionInbound, RPCMessageResponse, "github", "", responsePayload, nil)

	// Close to flush
	CloseJSONLLogger()

	// Read and verify the log file
	logPath := filepath.Join(logDir, "test.jsonl")
	file, err := os.Open(logPath)
	require.NoError(err, "Failed to open log file")
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() {
		lineCount++
		line := scanner.Text()

		var entry JSONLRPCMessage
		err := json.Unmarshal([]byte(line), &entry)
		require.NoError(err, "Failed to parse JSONL line %d: %s", lineCount, line)

		// Verify common fields
		assert.NotEmpty(entry.Timestamp, "Line %d: missing timestamp", lineCount)
		assert.NotEmpty(entry.Direction, "Line %d: missing direction", lineCount)
		assert.NotEmpty(entry.Type, "Line %d: missing type", lineCount)
		assert.NotEmpty(entry.ServerID, "Line %d: missing server_id", lineCount)
		assert.NotNil(entry.Payload, "Line %d: missing payload", lineCount)

		// Verify line-specific fields
		switch lineCount {
		case 1:
			// First line should be a REQUEST
			assert.Equal("REQUEST", entry.Type, "Line 1: expected type REQUEST")
			assert.Equal("tools/list", entry.Method, "Line 1: expected method tools/list")
			assert.Equal("OUT", entry.Direction, "Line 1: expected direction OUT")
		case 2:
			// Second line should be a RESPONSE
			assert.Equal("RESPONSE", entry.Type, "Line 2: expected type RESPONSE")
			assert.Equal("IN", entry.Direction, "Line 2: expected direction IN")
		}
	}

	err = scanner.Err()
	require.NoError(err, "Error reading log file")

	assert.Equal(2, lineCount, "Expected 2 log entries")
}

func TestSanitizePayload(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectRedacted bool
		checkField     string
	}{
		{
			name:           "token in payload",
			input:          `{"token":"ghp_1234567890123456789012345678901234567890"}`,
			expectRedacted: true,
			checkField:     "token",
		},
		{
			name:           "nested token in params",
			input:          `{"params":{"auth":"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.sig"}}`,
			expectRedacted: true,
			checkField:     "params.auth",
		},
		{
			name:           "password field",
			input:          `{"password":"supersecret123"}`,
			expectRedacted: true,
			checkField:     "password",
		},
		{
			name:           "clean payload",
			input:          `{"method":"tools/list","id":1}`,
			expectRedacted: false,
			checkField:     "method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			result := sanitize.SanitizeJSON([]byte(tt.input))
			require.NotNil(result, "sanitize.SanitizeJSON returned nil")

			// The result is already a sanitized string
			sanitizedStr := string(result)

			if tt.expectRedacted {
				// Should contain [REDACTED]
				assert.Contains(sanitizedStr, "[REDACTED]", "Expected sanitized payload to contain [REDACTED]")

				// Should NOT contain the original secret patterns
				assert.NotContains(sanitizedStr, "ghp_", "Sanitized payload should not contain GitHub token")
				assert.NotContains(sanitizedStr, "Bearer eyJ", "Sanitized payload should not contain Bearer token")
				assert.NotContains(sanitizedStr, "supersecret", "Sanitized payload should not contain password")
			} else {
				// Should not contain [REDACTED] for clean payloads
				assert.NotContains(sanitizedStr, "[REDACTED]", "Clean payload should not be redacted")
			}
		})
	}
}

func TestSanitizePayloadWithNestedStructures(t *testing.T) {
	assert := assert.New(t)
	input := `{
		"params": {
			"credentials": {
				"apiKey": "test_fake_api_key_1234567890abcdefghij",
				"token": "ghp_1234567890123456789012345678901234567890"
			},
			"data": {
				"items": [
					{"name": "item1", "secret": "password123"},
					{"name": "item2", "value": "safe"}
				]
			}
		}
	}`

	result := sanitize.SanitizeJSON([]byte(input))

	// The result is already a sanitized string
	sanitizedStr := string(result)

	// Should redact all secrets at all levels
	assert.Contains(sanitizedStr, "[REDACTED]", "Expected [REDACTED] in sanitized output")

	// Should NOT contain original secrets
	assert.NotContains(sanitizedStr, "test_fake_api_key", "API key should be sanitized")
	assert.NotContains(sanitizedStr, "ghp_", "GitHub token should be sanitized")
	assert.NotContains(sanitizedStr, "password123", "Password should be sanitized")

	// Should preserve non-secret values
	assert.Contains(sanitizedStr, "item1", "Non-secret value 'item1' should be preserved")
	assert.Contains(sanitizedStr, "safe", "Non-secret value 'safe' should be preserved")
}

func TestLogRPCMessageJSONLWithError(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	// Log a response with error
	responsePayload := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid request"}}`)
	testErr := fmt.Errorf("backend connection failed")
	LogRPCMessageJSONL(RPCDirectionInbound, RPCMessageResponse, "github", "", responsePayload, testErr)

	// Close to flush
	CloseJSONLLogger()

	// Read and verify
	logPath := filepath.Join(logDir, "test.jsonl")
	content, err := os.ReadFile(logPath)
	require.NoError(err, "Failed to read log file")

	var entry JSONLRPCMessage
	err = json.Unmarshal(content, &entry)
	require.NoError(err, "Failed to parse JSONL")

	assert.Equal("backend connection failed", entry.Error, "Error field should match")
}

func TestLogRPCMessageJSONLWithInvalidJSON(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	// Log invalid JSON
	invalidPayload := []byte(`{invalid json}`)
	LogRPCMessageJSONL(RPCDirectionOutbound, RPCMessageRequest, "github", "test", invalidPayload, nil)

	// Close to flush
	CloseJSONLLogger()

	// Read and verify
	logPath := filepath.Join(logDir, "test.jsonl")
	content, err := os.ReadFile(logPath)
	require.NoError(err, "Failed to read log file")

	var entry JSONLRPCMessage
	err = json.Unmarshal(content, &entry)
	require.NoError(err, "Failed to parse JSONL")

	// The payload should be wrapped in a valid JSON object with an error marker
	var payloadObj map[string]interface{}
	err = json.Unmarshal(entry.Payload, &payloadObj)
	require.NoError(err, "Failed to parse payload")

	assert.Equal("invalid JSON", payloadObj["_error"], "Expected _error field in payload")
	assert.Contains(fmt.Sprintf("%v", payloadObj["_raw"]), "invalid", "Expected _raw field to contain original invalid JSON")
}

func TestJSONLLoggerNotInitialized(t *testing.T) {
	// Ensure no global logger is set
	CloseJSONLLogger()

	// Should not panic when logging without initialization
	requestPayload := []byte(`{"jsonrpc":"2.0","id":1,"method":"test"}`)
	LogRPCMessageJSONL(RPCDirectionOutbound, RPCMessageRequest, "github", "test", requestPayload, nil)
	// Test passes if no panic occurs
}

func TestMultipleMessagesInJSONL(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	// Log multiple messages
	messages := []struct {
		direction RPCMessageDirection
		msgType   RPCMessageType
		serverID  string
		method    string
		payload   string
	}{
		{RPCDirectionOutbound, RPCMessageRequest, "github", "tools/list", `{"jsonrpc":"2.0","method":"tools/list"}`},
		{RPCDirectionInbound, RPCMessageResponse, "github", "", `{"jsonrpc":"2.0","result":{}}`},
		{RPCDirectionOutbound, RPCMessageRequest, "backend", "tools/call", `{"jsonrpc":"2.0","method":"tools/call"}`},
		{RPCDirectionInbound, RPCMessageResponse, "backend", "", `{"jsonrpc":"2.0","result":{}}`},
	}

	for _, msg := range messages {
		LogRPCMessageJSONL(msg.direction, msg.msgType, msg.serverID, msg.method, []byte(msg.payload), nil)
	}

	// Close to flush
	CloseJSONLLogger()

	// Read and verify all lines
	logPath := filepath.Join(logDir, "test.jsonl")
	file, err := os.Open(logPath)
	require.NoError(err, "Failed to open log file")
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() {
		lineCount++
		line := scanner.Text()

		var entry JSONLRPCMessage
		err := json.Unmarshal([]byte(line), &entry)
		require.NoError(err, "Failed to parse JSONL line %d", lineCount)

		// Each line should be valid JSONL with required fields
		assert.NotEmpty(entry.Timestamp, "Line %d: missing timestamp", lineCount)
		assert.NotEmpty(entry.ServerID, "Line %d: missing server_id", lineCount)
	}

	assert.Equal(len(messages), lineCount, "Expected %d log entries", len(messages))
}

func TestSanitizePayloadCompactsJSON(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	// Test that multi-line JSON is compacted to a single line
	multilineJSON := `{
		"jsonrpc": "2.0",
		"method": "test",
		"params": {
			"nested": {
				"value": "test"
			}
		}
	}`

	result := sanitize.SanitizeJSON([]byte(multilineJSON))
	resultStr := string(result)

	// The result should not contain newlines
	assert.NotContains(resultStr, "\n", "Result should not contain newlines")

	// Should still be valid JSON
	var tmp interface{}
	err := json.Unmarshal(result, &tmp)
	require.NoError(err, "Result should be valid JSON")

	// Should contain the expected values
	assert.Contains(resultStr, "jsonrpc", "Result should contain 'jsonrpc'")
	assert.Contains(resultStr, "test", "Result should contain 'test'")
}

func TestInitJSONLLoggerWithInvalidPath(t *testing.T) {
	assert := assert.New(t)

	// Test initialization with an invalid directory path (permission denied scenario)
	// Using /proc/self as it's read-only and will fail to create subdirectories
	err := InitJSONLLogger("/proc/self/invalid", "test.jsonl")
	assert.Error(err, "InitJSONLLogger should fail with invalid directory path")
}

func TestLogRPCMessageJSONLDirectionTypes(t *testing.T) {
	require := require.New(t)
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	tests := []struct {
		name      string
		direction RPCMessageDirection
		msgType   RPCMessageType
		expected  map[string]string
	}{
		{
			name:      "outbound request",
			direction: RPCDirectionOutbound,
			msgType:   RPCMessageRequest,
			expected:  map[string]string{"direction": "OUT", "type": "REQUEST"},
		},
		{
			name:      "inbound request",
			direction: RPCDirectionInbound,
			msgType:   RPCMessageRequest,
			expected:  map[string]string{"direction": "IN", "type": "REQUEST"},
		},
		{
			name:      "outbound response",
			direction: RPCDirectionOutbound,
			msgType:   RPCMessageResponse,
			expected:  map[string]string{"direction": "OUT", "type": "RESPONSE"},
		},
		{
			name:      "inbound response",
			direction: RPCDirectionInbound,
			msgType:   RPCMessageResponse,
			expected:  map[string]string{"direction": "IN", "type": "RESPONSE"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := assert.New(t)
			testPayload := []byte(`{"jsonrpc":"2.0","id":1}`)

			// Clear previous log file
			logPath := filepath.Join(logDir, "test.jsonl")
			os.Remove(logPath)

			// Re-init logger for each subtest
			CloseJSONLLogger()
			err := InitJSONLLogger(logDir, "test.jsonl")
			require.NoError(err, "Re-init failed")

			LogRPCMessageJSONL(tt.direction, tt.msgType, "test-server", "test-method", testPayload, nil)
			CloseJSONLLogger()

			// Read and verify
			content, err := os.ReadFile(logPath)
			require.NoError(err, "Failed to read log file")

			var entry JSONLRPCMessage
			err = json.Unmarshal(content, &entry)
			require.NoError(err, "Failed to parse JSONL entry")

			a.Equal(tt.expected["direction"], entry.Direction, "Direction should match")
			a.Equal(tt.expected["type"], entry.Type, "Type should match")
		})
	}
}

func TestLogRPCMessageJSONLEmptyPayload(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	// Log with empty payload
	emptyPayload := []byte(`{}`)
	LogRPCMessageJSONL(RPCDirectionOutbound, RPCMessageRequest, "github", "test", emptyPayload, nil)

	CloseJSONLLogger()

	// Read and verify
	logPath := filepath.Join(logDir, "test.jsonl")
	content, err := os.ReadFile(logPath)
	require.NoError(err, "Failed to read log file")

	var entry JSONLRPCMessage
	err = json.Unmarshal(content, &entry)
	require.NoError(err, "Failed to parse JSONL")

	// Should still have a valid payload field
	assert.NotNil(entry.Payload, "Payload should not be nil even when empty")
	assert.NotEmpty(entry.Timestamp, "Timestamp should be present")
	assert.Equal("github", entry.ServerID, "ServerID should match")
}

func TestLogRPCMessageJSONLWithNilError(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	// Log with nil error (normal case)
	payload := []byte(`{"jsonrpc":"2.0","id":1}`)
	LogRPCMessageJSONL(RPCDirectionOutbound, RPCMessageRequest, "github", "test", payload, nil)

	CloseJSONLLogger()

	// Read and verify
	logPath := filepath.Join(logDir, "test.jsonl")
	content, err := os.ReadFile(logPath)
	require.NoError(err, "Failed to read log file")

	var entry JSONLRPCMessage
	err = json.Unmarshal(content, &entry)
	require.NoError(err, "Failed to parse JSONL")

	// Error field should be empty when nil error is passed
	assert.Empty(entry.Error, "Error field should be empty when no error")
}

// TestLogDifcFilteredItem_NilEntryDoesNotPanic verifies that calling
// LogDifcFilteredItem with a nil entry is safe.  The DIFC audit log path
// must never crash the gateway even when passed unexpected input.
func TestLogDifcFilteredItem_NilEntryDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		LogDifcFilteredItem(nil)
	}, "LogDifcFilteredItem(nil) must not panic")
}

// TestLogDifcFilteredItem_WritesAuditEntryToJSONL verifies that
// LogDifcFilteredItem correctly writes a DIFC_FILTERED entry to the JSONL
// audit log.  Audit trail continuity requires every filtered item to appear
// in rpc-messages.jsonl so privileged audit agents can inspect them.
func TestLogDifcFilteredItem_WritesAuditEntryToJSONL(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "rpc-messages.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	entry := &JSONLFilteredItem{
		FilteredItemLogEntry: FilteredItemLogEntry{
			ServerID:          "github",
			ToolName:          "list_issues",
			Description:       "issue:org/repo#42",
			Reason:            "agent lacks secrecy clearance for private:org/repo",
			SecrecyTags:       []string{"private:org/repo"},
			IntegrityTags:     []string{"approved:org/repo"},
			AuthorLogin:       "octocat",
			AuthorAssociation: "CONTRIBUTOR",
			HTMLURL:           "https://github.com/org/repo/issues/42",
			Number:            "42",
		},
	}
	LogDifcFilteredItem(entry)

	CloseJSONLLogger()

	logPath := filepath.Join(logDir, "rpc-messages.jsonl")
	content, err := os.ReadFile(logPath)
	require.NoError(err, "log file must exist after LogDifcFilteredItem")

	var logged JSONLFilteredItem
	err = json.Unmarshal(content, &logged)
	require.NoError(err, "JSONL entry must be valid JSON")

	assert.Equal("DIFC_FILTERED", logged.Type, "Type must be DIFC_FILTERED")
	assert.NotEmpty(logged.Timestamp, "Timestamp must be set for audit trail")
	assert.Equal("github", logged.ServerID)
	assert.Equal("list_issues", logged.ToolName)
	assert.Equal("issue:org/repo#42", logged.Description)
	assert.Equal("agent lacks secrecy clearance for private:org/repo", logged.Reason)
	assert.Equal([]string{"private:org/repo"}, logged.SecrecyTags)
	assert.Equal([]string{"approved:org/repo"}, logged.IntegrityTags)
	assert.Equal("octocat", logged.AuthorLogin)
	assert.Equal("42", logged.Number)
}

// TestLogDifcFilteredItem_MultipleEntriesAuditTrail verifies that multiple
// filtered items all appear in the JSONL audit log in order, without loss.
// This exercises the audit trail continuity recommendation from the integrity
// audit: every filtered item must be retained for retrospective analysis.
func TestLogDifcFilteredItem_MultipleEntriesAuditTrail(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "rpc-messages.jsonl")
	require.NoError(err)
	defer CloseJSONLLogger()

	entries := []*JSONLFilteredItem{
		{FilteredItemLogEntry: FilteredItemLogEntry{ServerID: "github", ToolName: "list_issues", Number: "1", Reason: "secrecy mismatch"}},
		{FilteredItemLogEntry: FilteredItemLogEntry{ServerID: "github", ToolName: "list_prs", Number: "2", Reason: "integrity too low"}},
		{FilteredItemLogEntry: FilteredItemLogEntry{ServerID: "github", ToolName: "list_commits", SHA: "abc123", Reason: "secrecy mismatch"}},
	}

	for _, e := range entries {
		LogDifcFilteredItem(e)
	}
	CloseJSONLLogger()

	logPath := filepath.Join(logDir, "rpc-messages.jsonl")
	file, err := os.Open(logPath)
	require.NoError(err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lines []JSONLFilteredItem
	for scanner.Scan() {
		var item JSONLFilteredItem
		err := json.Unmarshal([]byte(scanner.Text()), &item)
		require.NoError(err, "each line must be valid JSON")
		lines = append(lines, item)
	}
	require.NoError(scanner.Err())

	assert.Len(lines, 3, "all 3 filtered items must appear in the audit log")
	for i, line := range lines {
		assert.Equal("DIFC_FILTERED", line.Type, "entry[%d] must have Type=DIFC_FILTERED", i)
		assert.NotEmpty(line.Timestamp, "entry[%d] must have Timestamp", i)
		assert.NotEmpty(line.Reason, "entry[%d] must have Reason", i)
	}
}

// TestLogRPCMessageJSONLWithTags_AgentSecrecyTags verifies that agent secrecy tags
// are written into the JSONL entry when provided, and that integrity is omitted.
func TestLogRPCMessageJSONLWithTags_AgentSecrecyTags(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	payload := []byte(`{"jsonrpc":"2.0","id":1}`)
	secrecyTags := []string{"private:org/repo", "public"}

	LogRPCMessageJSONLWithTags(RPCDirectionInbound, RPCMessageResponse, "github", "tools/call", payload, nil, secrecyTags, nil)
	CloseJSONLLogger()

	logPath := filepath.Join(logDir, "test.jsonl")
	content, err := os.ReadFile(logPath)
	require.NoError(err, "Failed to read log file")

	var entry JSONLRPCMessage
	err = json.Unmarshal(content, &entry)
	require.NoError(err, "Failed to parse JSONL entry")

	assert.Equal(secrecyTags, entry.AgentSecrecy, "AgentSecrecy should match provided tags")
	assert.Empty(entry.AgentIntegrity, "AgentIntegrity should be absent when not provided")
}

// TestLogRPCMessageJSONLWithTags_AgentIntegrityTags verifies that agent integrity tags
// are written into the JSONL entry when provided, and that secrecy is omitted.
func TestLogRPCMessageJSONLWithTags_AgentIntegrityTags(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	payload := []byte(`{"jsonrpc":"2.0","id":1}`)
	integrityTags := []string{"approved:org/repo", "merged"}

	LogRPCMessageJSONLWithTags(RPCDirectionOutbound, RPCMessageRequest, "github", "tools/list", payload, nil, nil, integrityTags)
	CloseJSONLLogger()

	logPath := filepath.Join(logDir, "test.jsonl")
	content, err := os.ReadFile(logPath)
	require.NoError(err, "Failed to read log file")

	var entry JSONLRPCMessage
	err = json.Unmarshal(content, &entry)
	require.NoError(err, "Failed to parse JSONL entry")

	assert.Empty(entry.AgentSecrecy, "AgentSecrecy should be absent when not provided")
	assert.Equal(integrityTags, entry.AgentIntegrity, "AgentIntegrity should match provided tags")
}

// TestLogRPCMessageJSONLWithTags_BothTagTypes verifies that both agent secrecy and
// integrity tags are correctly written when both are provided in the same call.
func TestLogRPCMessageJSONLWithTags_BothTagTypes(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	payload := []byte(`{"jsonrpc":"2.0","id":2}`)
	secrecyTags := []string{"private:org/repo"}
	integrityTags := []string{"approved:org/repo", "merged:org/repo"}

	LogRPCMessageJSONLWithTags(RPCDirectionInbound, RPCMessageResponse, "github", "tools/call", payload, nil, secrecyTags, integrityTags)
	CloseJSONLLogger()

	logPath := filepath.Join(logDir, "test.jsonl")
	content, err := os.ReadFile(logPath)
	require.NoError(err, "Failed to read log file")

	var entry JSONLRPCMessage
	err = json.Unmarshal(content, &entry)
	require.NoError(err, "Failed to parse JSONL entry")

	assert.Equal(secrecyTags, entry.AgentSecrecy, "AgentSecrecy should match")
	assert.Equal(integrityTags, entry.AgentIntegrity, "AgentIntegrity should match")
	assert.Equal("github", entry.ServerID)
	assert.Equal("tools/call", entry.Method)
}

// TestLogRPCMessageJSONLWithTags_EmptyTagsOmitted verifies that empty (non-nil) tag
// slices are treated the same as nil — they must NOT appear in the JSON output due to
// the omitempty struct tag on AgentSecrecy and AgentIntegrity.
func TestLogRPCMessageJSONLWithTags_EmptyTagsOmitted(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	payload := []byte(`{"jsonrpc":"2.0","id":1}`)

	// Pass explicitly empty (non-nil) slices.
	LogRPCMessageJSONLWithTags(RPCDirectionOutbound, RPCMessageRequest, "github", "tools/list", payload, nil, []string{}, []string{})
	CloseJSONLLogger()

	logPath := filepath.Join(logDir, "test.jsonl")
	content, err := os.ReadFile(logPath)
	require.NoError(err, "Failed to read log file")

	var entry JSONLRPCMessage
	err = json.Unmarshal(content, &entry)
	require.NoError(err, "Failed to parse JSONL entry")

	// Empty slices must not be stored (len == 0 check in implementation).
	assert.Empty(entry.AgentSecrecy, "AgentSecrecy must be absent for empty slice input")
	assert.Empty(entry.AgentIntegrity, "AgentIntegrity must be absent for empty slice input")

	// Verify the raw JSON does not contain the tag fields at all.
	assert.NotContains(string(content), "agent_secrecy", "raw JSON must not contain agent_secrecy key for empty slice")
	assert.NotContains(string(content), "agent_integrity", "raw JSON must not contain agent_integrity key for empty slice")
}

// TestLogRPCMessageJSONLWithTags_TagsCopied verifies that the tags stored in the log
// entry are independent copies of the caller's slice. Mutating the original after the
// call must not alter the data that was written to disk.
func TestLogRPCMessageJSONLWithTags_TagsCopied(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitJSONLLogger(logDir, "test.jsonl")
	require.NoError(err, "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	payload := []byte(`{"jsonrpc":"2.0","id":1}`)
	secrecyTags := []string{"private:org/repo"}
	integrityTags := []string{"approved:org/repo"}

	LogRPCMessageJSONLWithTags(RPCDirectionInbound, RPCMessageResponse, "github", "tools/call", payload, nil, secrecyTags, integrityTags)
	CloseJSONLLogger()

	// Mutate the originals after the call.
	secrecyTags[0] = "MUTATED"
	integrityTags[0] = "MUTATED"

	logPath := filepath.Join(logDir, "test.jsonl")
	content, err := os.ReadFile(logPath)
	require.NoError(err, "Failed to read log file")

	var entry JSONLRPCMessage
	err = json.Unmarshal(content, &entry)
	require.NoError(err, "Failed to parse JSONL entry")

	// The logged values must reflect the originals at call time, not the mutation.
	assert.Equal([]string{"private:org/repo"}, entry.AgentSecrecy, "AgentSecrecy must be an independent copy")
	assert.Equal([]string{"approved:org/repo"}, entry.AgentIntegrity, "AgentIntegrity must be an independent copy")
}

// TestLogEntry_NilLogFile verifies that logEntry returns a descriptive error when
// the JSONLLogger has not been initialized (logFile == nil).
func TestLogEntry_NilLogFile(t *testing.T) {
	// Create a logger with nil logFile — simulates an uninitialized logger.
	jl := &JSONLLogger{}

	err := jl.logEntry(map[string]string{"key": "value"})

	require.Error(t, err, "logEntry with nil logFile should return an error")
	assert.Contains(t, err.Error(), "not initialized", "error message should indicate logger is not initialized")
}

// TestLogEntry_EncodeError verifies that logEntry returns a wrapped error when
// json.Encoder.Encode fails (e.g., for a value that cannot be marshaled to JSON).
func TestLogEntry_EncodeError(t *testing.T) {
	tmpDir := t.TempDir()
	f, err := os.CreateTemp(tmpDir, "encode-error-*.jsonl")
	require.NoError(t, err, "failed to create temp file")
	t.Cleanup(func() { f.Close() })

	jl := &JSONLLogger{
		logFile: f,
		encoder: json.NewEncoder(f),
	}

	// Channels cannot be marshaled to JSON; Encode must return an error.
	err = jl.logEntry(make(chan int))

	require.Error(t, err, "logEntry should return error for un-encodable type")
	assert.Contains(t, err.Error(), "failed to encode JSON", "error should be wrapped with context")
}

// TestLogEntry_SyncError verifies that logEntry returns a wrapped error when
// logFile.Sync() fails. This happens for file descriptors that do not support
// fsync, such as the write end of an OS pipe.
func TestLogEntry_SyncError(t *testing.T) {
	// os.Pipe returns a connected pair of Files. Calling Sync on the write end
	// returns EINVAL on Linux because pipes do not support fsync.
	r, w, err := os.Pipe()
	require.NoError(t, err, "failed to create OS pipe")
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})

	// Pre-flight: verify that Sync on the write end of a pipe actually fails on
	// this OS/filesystem. If Sync is a no-op or succeeds, skip so the suite
	// stays portable.
	if syncErr := w.Sync(); syncErr == nil {
		t.Skip("os.File.Sync on a pipe does not return an error on this platform")
	}

	jl := &JSONLLogger{
		logFile: w,
		encoder: json.NewEncoder(w),
	}

	// A simple JSON-encodable value: Encode should succeed, but Sync must fail.
	syncErr := jl.logEntry(map[string]string{"event": "test"})

	require.Error(t, syncErr, "logEntry should return an error when Sync fails")
	assert.Contains(t, syncErr.Error(), "failed to sync log file", "error should be wrapped with sync context")
}

// TestLogEntry_HappyPath verifies that logEntry succeeds for a valid logger and
// encodable entry, writing a single JSONL line to the underlying file.
func TestLogEntry_HappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.jsonl")
	f, err := os.Create(logPath)
	require.NoError(t, err, "failed to create log file")

	jl := &JSONLLogger{
		logFile: f,
		encoder: json.NewEncoder(f),
	}

	type testPayload struct {
		Event string `json:"event"`
		Count int    `json:"count"`
	}

	err = jl.logEntry(testPayload{Event: "test-event", Count: 42})
	require.NoError(t, err, "logEntry should succeed for valid logger and encodable entry")

	// Flush and verify the file content.
	require.NoError(t, f.Close(), "close should succeed")

	data, err := os.ReadFile(logPath)
	require.NoError(t, err, "failed to read log file")

	var got testPayload
	require.NoError(t, json.Unmarshal(data, &got), "written data should be valid JSON")
	assert.Equal(t, "test-event", got.Event)
	assert.Equal(t, 42, got.Count)
}

// TestLogEntry_ConcurrentAccess verifies that logEntry is safe to call from
// multiple goroutines at the same time — the mutex must prevent data races.
func TestLogEntry_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "concurrent.jsonl")
	f, err := os.Create(logPath)
	require.NoError(t, err, "failed to create log file")

	jl := &JSONLLogger{
		logFile: f,
		encoder: json.NewEncoder(f),
	}

	const numGoroutines = 20
	done := make(chan error, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		i := i
		go func() {
			done <- jl.logEntry(map[string]int{"n": i})
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		assert.NoError(t, <-done, "concurrent logEntry call should not error")
	}

	// Count lines written: each successful logEntry writes exactly one JSONL line.
	require.NoError(t, f.Close())
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	assert.Len(t, lines, numGoroutines, "each goroutine should write exactly one JSONL line")
}

// TestLogEntry_EncodeErrorMessage verifies that the error wrapping preserves
// the underlying JSON marshal error so callers can inspect it with errors.As / errors.Is.
func TestLogEntry_EncodeErrorMessage(t *testing.T) {
	tmpDir := t.TempDir()
	f, err := os.CreateTemp(tmpDir, "encode-err-*.jsonl")
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })

	jl := &JSONLLogger{logFile: f, encoder: json.NewEncoder(f)}

	err = jl.logEntry(make(chan int))

	require.Error(t, err)
	// The original json.UnsupportedTypeError must be unwrappable.
	var unsupported *json.UnsupportedTypeError
	assert.True(t, errors.As(err, &unsupported), "wrapped error should be a *json.UnsupportedTypeError")
}

// TestLogDifcFilteredItem_NilEntry verifies that LogDifcFilteredItem does not
// panic and returns silently when called with a nil entry.
func TestLogDifcFilteredItem_NilEntry(t *testing.T) {
	// This exercises the early-return guard at the top of LogDifcFilteredItem.
	// It must not panic even when no global JSONL logger is initialised.
	assert.NotPanics(t, func() {
		LogDifcFilteredItem(nil)
	}, "LogDifcFilteredItem(nil) must not panic")
}

// TestLogDifcFilteredItem_SetsTimestampAndType verifies that LogDifcFilteredItem
// populates the Timestamp and Type fields of the entry before writing it.
func TestLogDifcFilteredItem_SetsTimestampAndType(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	require.NoError(t, InitJSONLLogger(logDir, "difc.jsonl"), "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	entry := &JSONLFilteredItem{
		FilteredItemLogEntry: FilteredItemLogEntry{
			ServerID:    "github",
			ToolName:    "create_issue",
			Description: "Create a GitHub issue",
			Reason:      "secrecy constraint violated",
			SecrecyTags: []string{"private:org/repo"},
		},
	}

	LogDifcFilteredItem(entry)
	CloseJSONLLogger()

	logPath := filepath.Join(logDir, "difc.jsonl")
	data, err := os.ReadFile(logPath)
	require.NoError(t, err, "log file must be readable")

	var got JSONLFilteredItem
	require.NoError(t, json.Unmarshal(data, &got), "log entry must be valid JSON")

	assert.Equal(t, "DIFC_FILTERED", got.Type, "Type must always be DIFC_FILTERED")
	assert.NotEmpty(t, got.Timestamp, "Timestamp must be set by LogDifcFilteredItem")
	assert.Equal(t, "github", got.ServerID)
	assert.Equal(t, "create_issue", got.ToolName)
	assert.Equal(t, []string{"private:org/repo"}, got.SecrecyTags)
}

// TestLogDifcFilteredItem_NoLogger verifies that LogDifcFilteredItem does nothing
// and does not panic when no global JSONL logger has been initialised.
func TestLogDifcFilteredItem_NoLogger(t *testing.T) {
	// Ensure no global logger is active.
	CloseJSONLLogger()

	entry := &JSONLFilteredItem{
		FilteredItemLogEntry: FilteredItemLogEntry{
			ServerID: "test",
			ToolName: "some_tool",
		},
	}

	assert.NotPanics(t, func() {
		LogDifcFilteredItem(entry)
	}, "LogDifcFilteredItem must not panic when no logger is initialised")
}
