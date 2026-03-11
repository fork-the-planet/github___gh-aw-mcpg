package logger

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatRPCMessage(t *testing.T) {
	tests := []struct {
		name string
		info *RPCMessageInfo
		want []string // Strings that should be present in output
	}{
		{
			name: "outbound request",
			info: &RPCMessageInfo{
				Direction:   RPCDirectionOutbound,
				MessageType: RPCMessageRequest,
				ServerID:    "github",
				Method:      "tools/list",
				PayloadSize: 50,
				Payload:     `{"jsonrpc":"2.0","method":"tools/list"}`,
			},
			want: []string{"github→tools/list", "50b", `{"jsonrpc":"2.0","method":"tools/list"}`},
		},
		{
			name: "inbound response with error",
			info: &RPCMessageInfo{
				Direction:   RPCDirectionInbound,
				MessageType: RPCMessageResponse,
				ServerID:    "github",
				PayloadSize: 100,
				Payload:     `{"jsonrpc":"2.0","error":{"code":-32600}}`,
				Error:       "Invalid request",
			},
			want: []string{"github←resp", "100b", "err:Invalid request"},
		},
		{
			name: "client request",
			info: &RPCMessageInfo{
				Direction:   RPCDirectionInbound,
				MessageType: RPCMessageRequest,
				ServerID:    "client",
				Method:      "tools/call",
				PayloadSize: 200,
				Payload:     `{"method":"tools/call","params":{}}`,
			},
			want: []string{"client←tools/call", "200b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatRPCMessage(tt.info)

			for _, expected := range tt.want {
				assert.Contains(t, result, expected, "formatRPCMessage result should contain %q", expected)
			}
		})
	}
}

func TestFormatRPCMessageMarkdown(t *testing.T) {
	tests := []struct {
		name    string
		info    *RPCMessageInfo
		want    []string // Strings that should be present in output
		notWant []string // Strings that should NOT be present in output
	}{
		{
			name: "outbound request",
			info: &RPCMessageInfo{
				Direction:   RPCDirectionOutbound,
				MessageType: RPCMessageRequest,
				ServerID:    "github",
				Method:      "tools/list",
				PayloadSize: 50,
				Payload:     `{"jsonrpc":"2.0","method":"tools/list","params":{}}`,
			},
			want:    []string{"**github**→`tools/list`", "```json", `"params"`, "{}"},
			notWant: []string{`"jsonrpc"`, `"method"`},
		},
		{
			name: "inbound response",
			info: &RPCMessageInfo{
				Direction:   RPCDirectionInbound,
				MessageType: RPCMessageResponse,
				ServerID:    "github",
				PayloadSize: 100,
				Payload:     `{"result":{}}`,
			},
			want:    []string{"**github**←`resp`", "```json", `"result"`},
			notWant: []string{`"jsonrpc"`, `"method"`},
		},
		{
			name: "response with error",
			info: &RPCMessageInfo{
				Direction:   RPCDirectionInbound,
				MessageType: RPCMessageResponse,
				ServerID:    "github",
				PayloadSize: 100,
				Error:       "Connection timeout",
			},
			want:    []string{"**github**←`resp`", "⚠️`Connection timeout`"},
			notWant: []string{},
		},
		{
			name: "invalid JSON payload uses inline backticks",
			info: &RPCMessageInfo{
				Direction:   RPCDirectionOutbound,
				MessageType: RPCMessageRequest,
				ServerID:    "github",
				Method:      "tools/call",
				PayloadSize: 30,
				Payload:     `{invalid json syntax}`,
			},
			want:    []string{"**github**→`tools/call`", "`{invalid json syntax}`"},
			notWant: []string{"```json"}, // Should NOT use code blocks for invalid JSON
		},
		{
			name: "request with only params null after field removal",
			info: &RPCMessageInfo{
				Direction:   RPCDirectionOutbound,
				MessageType: RPCMessageRequest,
				ServerID:    "github",
				Method:      "tools/list",
				PayloadSize: 50,
				Payload:     `{"jsonrpc":"2.0","method":"tools/list","params":null}`,
			},
			want:    []string{"**github**→`tools/list`"},
			notWant: []string{"```json", `"params"`}, // Should NOT show JSON block when only params: null
		},
		{
			name: "request with empty object after field removal",
			info: &RPCMessageInfo{
				Direction:   RPCDirectionOutbound,
				MessageType: RPCMessageRequest,
				ServerID:    "github",
				Method:      "tools/list",
				PayloadSize: 50,
				Payload:     `{"jsonrpc":"2.0","method":"tools/list"}`,
			},
			want:    []string{"**github**→`tools/list`"},
			notWant: []string{"```json"}, // Should NOT show JSON block when empty
		},
		{
			name: "tools/call with tool name",
			info: &RPCMessageInfo{
				Direction:   RPCDirectionOutbound,
				MessageType: RPCMessageRequest,
				ServerID:    "github",
				Method:      "tools/call",
				PayloadSize: 100,
				Payload:     `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"search_code","arguments":{"query":"test"}}}`,
			},
			want:    []string{"**github**→`tools/call` `search_code`", "```json", `"arguments"`},
			notWant: []string{`"jsonrpc"`, `"method"`},
		},
		{
			name: "tools/call without tool name in params",
			info: &RPCMessageInfo{
				Direction:   RPCDirectionOutbound,
				MessageType: RPCMessageRequest,
				ServerID:    "github",
				Method:      "tools/call",
				PayloadSize: 50,
				Payload:     `{"jsonrpc":"2.0","method":"tools/call","params":{}}`,
			},
			want:    []string{"**github**→`tools/call`", "```json", `"params"`},
			notWant: []string{`"jsonrpc"`, `"method"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatRPCMessageMarkdown(tt.info)

			for _, expected := range tt.want {
				assert.Contains(t, result, expected, "formatRPCMessageMarkdown result should contain %q", expected)
			}

			for _, notExpected := range tt.notWant {
				assert.NotContains(t, result, notExpected, "formatRPCMessageMarkdown result should NOT contain %q", notExpected)
			}
		})
	}
}

func TestFormatJSONWithoutFields(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		fieldsToRemove []string
		wantContains   []string
		wantNotContain []string
		wantValid      bool
		wantEmpty      bool
	}{
		{
			name:           "remove jsonrpc and method",
			input:          `{"jsonrpc":"2.0","method":"tools/call","params":{"arg":"value"},"id":1}`,
			fieldsToRemove: []string{"jsonrpc", "method"},
			wantContains:   []string{`"params"`, `"arg"`, `"value"`, `"id"`},
			wantNotContain: []string{`"jsonrpc"`, `"method"`},
			wantValid:      true,
			wantEmpty:      false,
		},
		{
			name:           "compact single line format",
			input:          `{"a":"b","c":{"d":"e"}}`,
			fieldsToRemove: []string{},
			wantContains:   []string{`"a":"b"`, `"c":`, `"d":"e"`},
			wantNotContain: []string{"\n", "  "},
			wantValid:      true,
			wantEmpty:      false,
		},
		{
			name:           "invalid JSON returns as-is with false",
			input:          `{invalid json}`,
			fieldsToRemove: []string{"jsonrpc"},
			wantContains:   []string{`{invalid json}`},
			wantNotContain: []string{},
			wantValid:      false,
			wantEmpty:      false,
		},
		{
			name:           "empty object",
			input:          `{}`,
			fieldsToRemove: []string{"jsonrpc"},
			wantContains:   []string{`{}`},
			wantNotContain: []string{},
			wantValid:      true,
			wantEmpty:      true,
		},
		{
			name:           "only params null after removal",
			input:          `{"jsonrpc":"2.0","method":"tools/list","params":null}`,
			fieldsToRemove: []string{"jsonrpc", "method"},
			wantContains:   []string{`"params"`, `null`},
			wantNotContain: []string{},
			wantValid:      true,
			wantEmpty:      true,
		},
		{
			name:           "params with value is not empty",
			input:          `{"jsonrpc":"2.0","method":"tools/list","params":{"key":"value"}}`,
			fieldsToRemove: []string{"jsonrpc", "method"},
			wantContains:   []string{`"params"`},
			wantNotContain: []string{},
			wantValid:      true,
			wantEmpty:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, isValid, isEmpty := formatJSONWithoutFields(tt.input, tt.fieldsToRemove)

			assert.Equal(t, tt.wantValid, isValid, "isValid mismatch")
			assert.Equal(t, tt.wantEmpty, isEmpty, "isEmpty mismatch")

			for _, want := range tt.wantContains {
				assert.Contains(t, result, want, "result should contain %q", want)
			}

			for _, notWant := range tt.wantNotContain {
				assert.NotContains(t, result, notWant, "result should NOT contain %q", notWant)
			}
		})
	}
}

func TestLogRPCRequest(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	require.NoError(t, InitFileLogger(logDir, "test.log"), "InitFileLogger failed")
	defer CloseGlobalLogger()

	require.NoError(t, InitMarkdownLogger(logDir, "test.md"), "InitMarkdownLogger failed")
	defer CloseMarkdownLogger()

	// Log an RPC request
	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	LogRPCRequest(RPCDirectionOutbound, "github", "tools/list", payload)

	// Close loggers to flush
	CloseGlobalLogger()
	CloseMarkdownLogger()

	// Check text log
	textLog := filepath.Join(logDir, "test.log")
	textContent, err := os.ReadFile(textLog)
	require.NoError(t, err, "Failed to read text log")

	textStr := string(textContent)
	assert.Contains(t, textStr, "github→tools/list")
	assert.Contains(t, textStr, "58b")

	// Check markdown log
	mdLog := filepath.Join(logDir, "test.md")
	mdContent, err := os.ReadFile(mdLog)
	require.NoError(t, err, "Failed to read markdown log")

	assert.Contains(t, string(mdContent), "**github**→`tools/list`")
}

func TestLogRPCResponse(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	require.NoError(t, InitFileLogger(logDir, "test.log"), "InitFileLogger failed")
	defer CloseGlobalLogger()

	require.NoError(t, InitMarkdownLogger(logDir, "test.md"), "InitMarkdownLogger failed")
	defer CloseMarkdownLogger()

	// Log an RPC response with error
	payload := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid request"}}`)
	err := errors.New("backend connection failed")
	LogRPCResponse(RPCDirectionInbound, "github", payload, err)

	// Close loggers to flush
	CloseGlobalLogger()
	CloseMarkdownLogger()

	// Check text log
	textLog := filepath.Join(logDir, "test.log")
	textContent, readErr := os.ReadFile(textLog)
	require.NoError(t, readErr, "Failed to read text log")

	textStr := string(textContent)
	assert.Contains(t, textStr, "github←resp")
	assert.Contains(t, textStr, "err:backend connection failed")

	// Check markdown log
	mdLog := filepath.Join(logDir, "test.md")
	mdContent, readErr := os.ReadFile(mdLog)
	require.NoError(t, readErr, "Failed to read markdown log")

	mdStr := string(mdContent)
	assert.Contains(t, mdStr, "**github**←`resp`")
	assert.Contains(t, mdStr, "⚠️`backend connection failed`")
}

func TestLogRPCRequestWithSecrets(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	require.NoError(t, InitFileLogger(logDir, "test.log"), "InitFileLogger failed")
	defer CloseGlobalLogger()

	require.NoError(t, InitMarkdownLogger(logDir, "test.md"), "InitMarkdownLogger failed")
	defer CloseMarkdownLogger()

	// Log an RPC request with a secret
	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"authenticate","params":{"token":"ghp_1234567890123456789012345678901234567890"}}`)
	LogRPCRequest(RPCDirectionInbound, "client", "authenticate", payload)

	// Close loggers to flush
	CloseGlobalLogger()
	CloseMarkdownLogger()

	const secret = "ghp_1234567890123456789012345678901234567890"

	// Check text log - should NOT contain the actual token
	textLog := filepath.Join(logDir, "test.log")
	textContent, err := os.ReadFile(textLog)
	require.NoError(t, err, "Failed to read text log")

	textStr := string(textContent)
	assert.NotContains(t, textStr, secret, "Text log should not contain the raw secret")
	assert.Contains(t, textStr, "[REDACTED]", "Text log should contain [REDACTED] marker")

	// Check markdown log - should NOT contain the actual token
	mdLog := filepath.Join(logDir, "test.md")
	mdContent, err := os.ReadFile(mdLog)
	require.NoError(t, err, "Failed to read markdown log")

	mdStr := string(mdContent)
	assert.NotContains(t, mdStr, secret, "Markdown log should not contain the raw secret")
	assert.Contains(t, mdStr, "[REDACTED]", "Markdown log should contain [REDACTED] marker")
}

func TestLogRPCRequestPayloadTruncation(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	require.NoError(t, InitFileLogger(logDir, "test.log"), "InitFileLogger failed")
	defer CloseGlobalLogger()

	require.NoError(t, InitMarkdownLogger(logDir, "test.md"), "InitMarkdownLogger failed")
	defer CloseMarkdownLogger()

	// Create a large payload (> 10KB for text, > 512 chars for markdown)
	largeData := strings.Repeat("x", 12*1024) // 12KB of x's
	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"test","params":{"data":"` + largeData + `"}}`)
	LogRPCRequest(RPCDirectionOutbound, "backend", "test", payload)

	// Close loggers to flush
	CloseGlobalLogger()
	CloseMarkdownLogger()

	// Check text log - payload should be truncated at 10KB
	textLog := filepath.Join(logDir, "test.log")
	textContent, err := os.ReadFile(textLog)
	require.NoError(t, err, "Failed to read text log")

	textStr := string(textContent)
	assert.Contains(t, textStr, "...", "Text log should show truncation marker")
	assert.Equal(t, 0, strings.Count(textStr, strings.Repeat("x", 11*1024)),
		"Text log should not contain more than 10KB of data after truncation")

	// Check markdown log - should be truncated at 512 chars
	mdLog := filepath.Join(logDir, "test.md")
	mdContent, err := os.ReadFile(mdLog)
	require.NoError(t, err, "Failed to read markdown log")

	mdStr := string(mdContent)
	assert.Contains(t, mdStr, "...", "Markdown log should show truncation marker")
	assert.Equal(t, 0, strings.Count(mdStr, strings.Repeat("x", 600)),
		"Markdown log should not contain more than 512 chars of data after truncation")
}

func TestLogRPCRequestWithAgentSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	require.NoError(t, InitFileLogger(logDir, "test.log"), "InitFileLogger failed")
	defer CloseGlobalLogger()

	require.NoError(t, InitJSONLLogger(logDir, "test.jsonl"), "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	agentSecrecy := []string{"private", "confidential"}
	agentIntegrity := []string{"trusted"}

	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{}}`)
	LogRPCRequestWithAgentSnapshot(RPCDirectionOutbound, "github", "tools/call", payload, agentSecrecy, agentIntegrity)

	CloseGlobalLogger()
	CloseJSONLLogger()

	// Verify agent tags are recorded in the JSONL log
	jsonlPath := filepath.Join(logDir, "test.jsonl")
	content, err := os.ReadFile(jsonlPath)
	require.NoError(t, err, "Failed to read JSONL log")

	var entry JSONLRPCMessage
	require.NoError(t, json.Unmarshal(content, &entry), "Failed to parse JSONL entry")

	assert.Equal(t, "REQUEST", entry.Type)
	assert.Equal(t, "OUT", entry.Direction)
	assert.Equal(t, "github", entry.ServerID)
	assert.Equal(t, "tools/call", entry.Method)
	assert.ElementsMatch(t, agentSecrecy, entry.AgentSecrecy, "AgentSecrecy tags should be recorded")
	assert.ElementsMatch(t, agentIntegrity, entry.AgentIntegrity, "AgentIntegrity tags should be recorded")
}

func TestLogRPCResponseWithAgentSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	require.NoError(t, InitFileLogger(logDir, "test.log"), "InitFileLogger failed")
	defer CloseGlobalLogger()

	require.NoError(t, InitJSONLLogger(logDir, "test.jsonl"), "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	agentSecrecy := []string{"public"}
	agentIntegrity := []string{"approved", "merged"}

	payload := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)
	LogRPCResponseWithAgentSnapshot(RPCDirectionInbound, "github", payload, nil, agentSecrecy, agentIntegrity)

	CloseGlobalLogger()
	CloseJSONLLogger()

	// Verify agent tags are recorded in the JSONL log
	jsonlPath := filepath.Join(logDir, "test.jsonl")
	content, err := os.ReadFile(jsonlPath)
	require.NoError(t, err, "Failed to read JSONL log")

	var entry JSONLRPCMessage
	require.NoError(t, json.Unmarshal(content, &entry), "Failed to parse JSONL entry")

	assert.Equal(t, "RESPONSE", entry.Type)
	assert.Equal(t, "IN", entry.Direction)
	assert.Equal(t, "github", entry.ServerID)
	assert.ElementsMatch(t, agentSecrecy, entry.AgentSecrecy, "AgentSecrecy tags should be recorded")
	assert.ElementsMatch(t, agentIntegrity, entry.AgentIntegrity, "AgentIntegrity tags should be recorded")
}

func TestLogRPCRequestWithAgentSnapshot_EmptyTags(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	require.NoError(t, InitFileLogger(logDir, "test.log"), "InitFileLogger failed")
	defer CloseGlobalLogger()

	require.NoError(t, InitJSONLLogger(logDir, "test.jsonl"), "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	LogRPCRequestWithAgentSnapshot(RPCDirectionOutbound, "github", "tools/list", payload, nil, nil)

	CloseGlobalLogger()
	CloseJSONLLogger()

	jsonlPath := filepath.Join(logDir, "test.jsonl")
	content, err := os.ReadFile(jsonlPath)
	require.NoError(t, err, "Failed to read JSONL log")

	var entry JSONLRPCMessage
	require.NoError(t, json.Unmarshal(content, &entry), "Failed to parse JSONL entry")

	// nil slices should not appear as fields in JSON
	assert.Nil(t, entry.AgentSecrecy, "AgentSecrecy should be nil when empty tags passed")
	assert.Nil(t, entry.AgentIntegrity, "AgentIntegrity should be nil when empty tags passed")
}

func TestLogRPCMessage(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	require.NoError(t, InitFileLogger(logDir, "test.log"), "InitFileLogger failed")
	defer CloseGlobalLogger()

	require.NoError(t, InitMarkdownLogger(logDir, "test.md"), "InitMarkdownLogger failed")
	defer CloseMarkdownLogger()

	info := &RPCMessageInfo{
		Direction:   RPCDirectionOutbound,
		MessageType: RPCMessageRequest,
		ServerID:    "custom-server",
		Method:      "custom/method",
		PayloadSize: 42,
		Payload:     `{"key":"value"}`,
	}
	LogRPCMessage(info)

	CloseGlobalLogger()
	CloseMarkdownLogger()

	// Verify text log
	textContent, err := os.ReadFile(filepath.Join(logDir, "test.log"))
	require.NoError(t, err)
	assert.Contains(t, string(textContent), "custom-server→custom/method")

	// Verify markdown log
	mdContent, err := os.ReadFile(filepath.Join(logDir, "test.md"))
	require.NoError(t, err)
	assert.Contains(t, string(mdContent), "**custom-server**→`custom/method`")
}

func TestLogRPCResponse_NoError(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	require.NoError(t, InitFileLogger(logDir, "test.log"), "InitFileLogger failed")
	defer CloseGlobalLogger()

	require.NoError(t, InitJSONLLogger(logDir, "test.jsonl"), "InitJSONLLogger failed")
	defer CloseJSONLLogger()

	payload := []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	LogRPCResponse(RPCDirectionInbound, "backend", payload, nil)

	CloseGlobalLogger()
	CloseJSONLLogger()

	// Verify JSONL entry has no error field when nil error is passed
	jsonlContent, err := os.ReadFile(filepath.Join(logDir, "test.jsonl"))
	require.NoError(t, err)

	var jsonlLines []string
	scanner := bufio.NewScanner(strings.NewReader(string(jsonlContent)))
	for scanner.Scan() {
		jsonlLines = append(jsonlLines, scanner.Text())
	}
	require.Len(t, jsonlLines, 1, "Expected exactly 1 JSONL entry")

	var entry JSONLRPCMessage
	require.NoError(t, json.Unmarshal([]byte(jsonlLines[0]), &entry))
	assert.Empty(t, entry.Error, "Error field should be empty when no error")
	assert.Equal(t, "RESPONSE", entry.Type)
	assert.Equal(t, "backend", entry.ServerID)
}
