package sys

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper functions

// validateToolsListResponse is a test helper for validating tools/list responses
func validateToolsListResponse(t *testing.T, result interface{}, expectedToolCount int) []map[string]interface{} {
	t.Helper()

	require := require.New(t)
	assert := assert.New(t)

	resultMap, ok := result.(map[string]interface{})
	require.True(ok, "Result should be a map")

	tools, ok := resultMap["tools"].([]map[string]interface{})
	require.True(ok, "tools field should be an array of maps")
	assert.Equal(expectedToolCount, len(tools), "Tool count mismatch")

	return tools
}

// validateToolContent is a test helper for validating tool call response content
func validateToolContent(t *testing.T, result interface{}) string {
	t.Helper()

	require := require.New(t)
	assert := assert.New(t)

	resultMap, ok := result.(map[string]interface{})
	require.True(ok, "Result should be a map")

	content, ok := resultMap["content"].([]map[string]interface{})
	require.True(ok, "content field should be an array of maps")
	require.Equal(1, len(content), "Should have exactly 1 content item")

	contentItem := content[0]
	assert.Equal("text", contentItem["type"], "Content type should be text")

	text, ok := contentItem["text"].(string)
	require.True(ok, "text field should be a string")

	return text
}

// validateInputSchema is a test helper for validating tool input schemas
func validateInputSchema(t *testing.T, tool map[string]interface{}) {
	t.Helper()

	require := require.New(t)
	assert := assert.New(t)

	schema, ok := tool["inputSchema"].(map[string]interface{})
	require.True(ok, "inputSchema should be a map")
	assert.Equal("object", schema["type"], "inputSchema type should be object")
	assert.NotNil(schema["properties"], "inputSchema should have properties")
}

// Tests

func TestNewSysServer(t *testing.T) {
	tests := []struct {
		name      string
		serverIDs []string
		wantCount int
	}{
		{
			name:      "empty server list",
			serverIDs: []string{},
			wantCount: 0,
		},
		{
			name:      "single server",
			serverIDs: []string{"github"},
			wantCount: 1,
		},
		{
			name:      "multiple servers",
			serverIDs: []string{"github", "slack", "jira"},
			wantCount: 3,
		},
		{
			name:      "nil server list",
			serverIDs: nil,
			wantCount: 0,
		},
		{
			name:      "many servers",
			serverIDs: []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8", "s9", "s10"},
			wantCount: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			server := NewSysServer(tt.serverIDs)

			require.NotNil(server, "NewSysServer should never return nil")
			assert.Equal(tt.wantCount, len(server.serverIDs), "Server count mismatch")

			if len(tt.serverIDs) > 0 {
				assert.Equal(tt.serverIDs, server.serverIDs, "Server IDs should match")
			}
		})
	}
}

func TestNewSysServer_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name      string
		serverIDs []string
	}{
		{
			name:      "servers with hyphens",
			serverIDs: []string{"server-1", "server-2"},
		},
		{
			name:      "servers with underscores",
			serverIDs: []string{"server_1", "server_2"},
		},
		{
			name:      "servers with dots",
			serverIDs: []string{"server.1", "server.2"},
		},
		{
			name:      "mixed special characters",
			serverIDs: []string{"server-1", "server_2", "server.3"},
		},
		{
			name:      "unicode characters",
			serverIDs: []string{"服务器", "сервер", "🚀server"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			server := NewSysServer(tt.serverIDs)

			require.NotNil(server)
			assert.Equal(tt.serverIDs, server.serverIDs, "Should handle special characters")
		})
	}
}

func TestHandleRequest_ToolsList(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	server := NewSysServer([]string{"github", "slack"})

	result, err := server.HandleRequest("tools/list", nil)

	require.NoError(err, "tools/list should not return error")
	require.NotNil(result, "Result should not be nil")

	tools := validateToolsListResponse(t, result, 2)

	// Verify sys_init tool
	sysInitTool := tools[0]
	assert.Equal("sys_init", sysInitTool["name"], "First tool should be sys_init")
	assert.Contains(sysInitTool["description"], "Initialize", "Description should mention Initialize")
	validateInputSchema(t, sysInitTool)

	// Verify sys_list_servers tool
	listServersTool := tools[1]
	assert.Equal("sys_list_servers", listServersTool["name"], "Second tool should be sys_list_servers")
	assert.Contains(listServersTool["description"], "List all", "Description should mention List all")
	validateInputSchema(t, listServersTool)
}

func TestHandleRequest_ToolsCall_SysInit(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	serverIDs := []string{"github", "slack", "jira"}
	server := NewSysServer(serverIDs)

	params := json.RawMessage(`{
"name": "sys_init",
"arguments": {}
}`)

	result, err := server.HandleRequest("tools/call", params)

	require.NoError(err, "sys_init should not return error")
	require.NotNil(result, "Result should not be nil")

	text := validateToolContent(t, result)
	assert.Contains(text, "MCPG initialized", "Text should mention initialization")
	assert.Contains(text, "github", "Text should mention github server")
	assert.Contains(text, "slack", "Text should mention slack server")
	assert.Contains(text, "jira", "Text should mention jira server")
}

func TestHandleRequest_ToolsCall_ListServers(t *testing.T) {
	tests := []struct {
		name      string
		serverIDs []string
		wantCount int
	}{
		{
			name:      "empty servers",
			serverIDs: []string{},
			wantCount: 0,
		},
		{
			name:      "single server",
			serverIDs: []string{"github"},
			wantCount: 1,
		},
		{
			name:      "multiple servers",
			serverIDs: []string{"github", "slack", "jira"},
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			server := NewSysServer(tt.serverIDs)

			params := json.RawMessage(`{
"name": "sys_list_servers",
"arguments": {}
}`)

			result, err := server.HandleRequest("tools/call", params)

			require.NoError(err, "sys_list_servers should not return error")
			require.NotNil(result, "Result should not be nil")

			text := validateToolContent(t, result)
			assert.Contains(text, "Configured MCP Servers", "Text should mention configured servers")

			// Verify each server ID is listed with correct numbering
			for i, id := range tt.serverIDs {
				assert.Contains(text, id, "Text should contain server ID: %s", id)
				// Verify numbering format: "1. github"
				expectedLine := (i + 1)
				assert.Contains(text, id, "Text should contain numbered server: %d. %s", expectedLine, id)
			}
		})
	}
}

func TestHandleRequest_ToolsCall_InvalidJSON(t *testing.T) {
	assert := assert.New(t)

	server := NewSysServer([]string{"github"})

	tests := []struct {
		name   string
		params json.RawMessage
	}{
		{
			name:   "invalid JSON",
			params: json.RawMessage(`{invalid json`),
		},
		{
			name:   "missing name field",
			params: json.RawMessage(`{"arguments": {}}`),
		},
		{
			name:   "null params",
			params: json.RawMessage(`null`),
		},
		{
			name:   "empty object",
			params: json.RawMessage(`{}`),
		},
		{
			name:   "array instead of object",
			params: json.RawMessage(`[]`),
		},
		{
			name:   "string instead of object",
			params: json.RawMessage(`"invalid"`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := server.HandleRequest("tools/call", tt.params)

			assert.Error(err, "Should return error for invalid params")
			assert.Nil(result, "Result should be nil on error")
			assert.Contains(err.Error(), "invalid params", "Error should mention invalid params")
		})
	}
}

func TestHandleRequest_ToolsCall_UnknownTool(t *testing.T) {
	assert := assert.New(t)

	server := NewSysServer([]string{"github"})

	tests := []struct {
		name     string
		toolName string
	}{
		{
			name:     "unknown tool",
			toolName: "unknown_tool",
		},
		{
			name:     "misspelled tool",
			toolName: "sys_initialize",
		},
		{
			name:     "case sensitive tool name",
			toolName: "SYS_INIT",
		},
		{
			name:     "empty tool name",
			toolName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := json.RawMessage(`{
"name": "` + tt.toolName + `",
"arguments": {}
}`)

			result, err := server.HandleRequest("tools/call", params)

			if tt.toolName == "" {
				assert.Error(err, "Empty tool name should return error")
				assert.Contains(err.Error(), "invalid params", "Error should mention invalid params for empty name")
			} else {
				assert.Error(err, "Should return error for unknown tool")
				assert.Contains(err.Error(), "unknown tool", "Error should mention unknown tool")
			}
			assert.Nil(result, "Result should be nil on error")
		})
	}
}

func TestHandleRequest_UnsupportedMethod(t *testing.T) {
	assert := assert.New(t)

	server := NewSysServer([]string{"github"})

	tests := []struct {
		name   string
		method string
	}{
		{
			name:   "resources/list",
			method: "resources/list",
		},
		{
			name:   "prompts/list",
			method: "prompts/list",
		},
		{
			name:   "empty method",
			method: "",
		},
		{
			name:   "invalid method",
			method: "invalid/method",
		},
		{
			name:   "case sensitive method",
			method: "Tools/List",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := server.HandleRequest(tt.method, nil)

			assert.Error(err, "Should return error for unsupported method")
			assert.Nil(result, "Result should be nil on error")
			assert.Contains(err.Error(), "unsupported method", "Error should mention unsupported method")
			assert.Contains(err.Error(), tt.method, "Error should include the method name")
		})
	}
}

func TestHandleRequest_ToolsCall_WithArguments(t *testing.T) {
	require := require.New(t)

	server := NewSysServer([]string{"github"})

	// Test that arguments are accepted even if not used
	params := json.RawMessage(`{
"name": "sys_init",
"arguments": {
"unused": "value",
"another": 123,
"nested": {"key": "value"}
}
}`)

	result, err := server.HandleRequest("tools/call", params)

	require.NoError(err, "Should not error with extra arguments")
	require.NotNil(result, "Result should not be nil")
}

func TestListTools_ResponseStructure(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	server := NewSysServer([]string{"github"})

	result, err := server.listTools()

	require.NoError(err, "listTools should not error")
	require.NotNil(result, "Result should not be nil")

	tools := validateToolsListResponse(t, result, 2)

	// Verify each tool has required fields
	for i, tool := range tools {
		assert.NotEmpty(tool["name"], "Tool %d should have name", i)
		assert.NotEmpty(tool["description"], "Tool %d should have description", i)
		validateInputSchema(t, tool)
	}
}

func TestCallTool_AllTools(t *testing.T) {
	serverIDs := []string{"github", "slack"}
	server := NewSysServer(serverIDs)

	tests := []struct {
		name         string
		toolName     string
		args         map[string]interface{}
		expectError  bool
		validateFunc func(t *testing.T, result interface{})
	}{
		{
			name:        "sys_init with empty args",
			toolName:    "sys_init",
			args:        map[string]interface{}{},
			expectError: false,
			validateFunc: func(t *testing.T, result interface{}) {
				text := validateToolContent(t, result)
				assert.Contains(t, text, "MCPG initialized")
				assert.Contains(t, text, "github")
				assert.Contains(t, text, "slack")
			},
		},
		{
			name:        "sys_init with ignored args",
			toolName:    "sys_init",
			args:        map[string]interface{}{"ignored": "value"},
			expectError: false,
			validateFunc: func(t *testing.T, result interface{}) {
				assert.NotNil(t, result)
			},
		},
		{
			name:        "sys_list_servers with empty args",
			toolName:    "sys_list_servers",
			args:        map[string]interface{}{},
			expectError: false,
			validateFunc: func(t *testing.T, result interface{}) {
				text := validateToolContent(t, result)
				assert.Contains(t, text, "Configured MCP Servers")
				assert.Contains(t, text, "1. github")
				assert.Contains(t, text, "2. slack")
			},
		},
		{
			name:        "sys_list_servers with nil args",
			toolName:    "sys_list_servers",
			args:        nil,
			expectError: false,
			validateFunc: func(t *testing.T, result interface{}) {
				assert.NotNil(t, result)
			},
		},
		{
			name:        "unknown tool",
			toolName:    "nonexistent",
			args:        map[string]interface{}{},
			expectError: true,
			validateFunc: func(t *testing.T, result interface{}) {
				assert.Nil(t, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			result, err := server.callTool(tt.toolName, tt.args)

			if tt.expectError {
				assert.Error(err)
				assert.Contains(err.Error(), "unknown tool")
			} else {
				require.NoError(err)
			}

			if tt.validateFunc != nil {
				tt.validateFunc(t, result)
			}
		})
	}
}

func TestSysInit_ServerListFormatting(t *testing.T) {
	tests := []struct {
		name      string
		serverIDs []string
	}{
		{
			name:      "empty servers",
			serverIDs: []string{},
		},
		{
			name:      "single server",
			serverIDs: []string{"github"},
		},
		{
			name:      "many servers",
			serverIDs: []string{"github", "slack", "jira", "notion", "linear"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			server := NewSysServer(tt.serverIDs)
			result, err := server.sysInit()

			require.NoError(err)
			require.NotNil(result)

			text := validateToolContent(t, result)
			assert.Contains(text, "MCPG initialized")
			assert.Contains(text, "Available servers")
		})
	}
}

func TestListServers_Formatting(t *testing.T) {
	tests := []struct {
		name      string
		serverIDs []string
		expected  []string
	}{
		{
			name:      "empty list",
			serverIDs: []string{},
			expected:  []string{"Configured MCP Servers:"},
		},
		{
			name:      "single server",
			serverIDs: []string{"github"},
			expected:  []string{"Configured MCP Servers:", "1. github"},
		},
		{
			name:      "multiple servers",
			serverIDs: []string{"github", "slack", "jira"},
			expected:  []string{"Configured MCP Servers:", "1. github", "2. slack", "3. jira"},
		},
		{
			name:      "servers with special characters",
			serverIDs: []string{"server-1", "server_2", "server.3"},
			expected:  []string{"1. server-1", "2. server_2", "3. server.3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			server := NewSysServer(tt.serverIDs)
			result, err := server.listServers()

			require.NoError(err)
			require.NotNil(result)

			text := validateToolContent(t, result)

			// Verify all expected strings are present
			for _, expectedStr := range tt.expected {
				assert.Contains(text, expectedStr, "Output should contain: %s", expectedStr)
			}
		})
	}
}

func TestHandleRequest_NilParams(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	server := NewSysServer([]string{"github"})

	// tools/list with nil params should work
	result, err := server.HandleRequest("tools/list", nil)
	require.NoError(err)
	assert.NotNil(result)

	// tools/call with nil params should fail
	result, err = server.HandleRequest("tools/call", nil)
	assert.Error(err)
	assert.Nil(result)
}

func TestHandleRequest_EmptyParams(t *testing.T) {
	assert := assert.New(t)

	server := NewSysServer([]string{"github"})

	// tools/call with empty JSON object should fail (missing name field)
	params := json.RawMessage(`{}`)
	result, err := server.HandleRequest("tools/call", params)

	assert.Error(err)
	assert.Nil(result)
}

func TestSysServer_MultipleSequentialCalls(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	server := NewSysServer([]string{"github", "slack"})

	// Call tools/list multiple times
	for i := 0; i < 3; i++ {
		result, err := server.HandleRequest("tools/list", nil)
		require.NoError(err, "Call %d should not error", i)
		assert.NotNil(result, "Call %d should return result", i)
	}

	// Call sys_init multiple times
	params := json.RawMessage(`{"name": "sys_init", "arguments": {}}`)
	for i := 0; i < 3; i++ {
		result, err := server.HandleRequest("tools/call", params)
		require.NoError(err, "Call %d should not error", i)
		assert.NotNil(result, "Call %d should return result", i)
	}

	// Call sys_list_servers multiple times
	params = json.RawMessage(`{"name": "sys_list_servers", "arguments": {}}`)
	for i := 0; i < 3; i++ {
		result, err := server.HandleRequest("tools/call", params)
		require.NoError(err, "Call %d should not error", i)
		assert.NotNil(result, "Call %d should return result", i)
	}
}

// New test: Concurrent access to SysServer
func TestSysServer_ConcurrentAccess(t *testing.T) {
	require := require.New(t)

	server := NewSysServer([]string{"github", "slack", "jira"})

	var wg sync.WaitGroup
	numGoroutines := 10

	// Test concurrent tools/list calls
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := server.HandleRequest("tools/list", nil)
			require.NoError(err)
			require.NotNil(result)
		}()
	}

	// Test concurrent sys_init calls
	params := json.RawMessage(`{"name": "sys_init", "arguments": {}}`)
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := server.HandleRequest("tools/call", params)
			require.NoError(err)
			require.NotNil(result)
		}()
	}

	wg.Wait()
}

// New test: Large server list
func TestSysServer_LargeServerList(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	// Create a large server list
	serverIDs := make([]string, 100)
	for i := 0; i < 100; i++ {
		serverIDs[i] = "server" + string(rune('a'+i%26))
	}

	server := NewSysServer(serverIDs)
	require.NotNil(server)

	// Test listServers with large list
	result, err := server.listServers()
	require.NoError(err)
	require.NotNil(result)

	text := validateToolContent(t, result)
	assert.Contains(text, "Configured MCP Servers")
	assert.Contains(text, "1. server")
	assert.Contains(text, "100. server")
}

// New test: Empty string in server IDs
func TestSysServer_EmptyStringServerID(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	serverIDs := []string{"github", "", "slack"}
	server := NewSysServer(serverIDs)
	require.NotNil(server)

	result, err := server.listServers()
	require.NoError(err)
	require.NotNil(result)

	text := validateToolContent(t, result)
	assert.Contains(text, "github")
	assert.Contains(text, "slack")
	// Empty string will still be included in the list
	assert.Contains(text, "2. \n")
}
