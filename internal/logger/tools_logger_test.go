package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitToolsLogger(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Create a temp directory for testing
	tmpDir := t.TempDir()

	// Initialize the tools logger
	err := InitToolsLogger(tmpDir, "tools.json")
	require.NoError(err, "InitToolsLogger failed")

	// Verify the global logger was initialized
	globalToolsMu.RLock()
	assert.NotNil(globalToolsLogger, "Global tools logger should be initialized")
	globalToolsMu.RUnlock()

	// Clean up
	err = CloseToolsLogger()
	assert.NoError(err, "CloseToolsLogger failed")
}

func TestToolsLoggerLogTools(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Create a temp directory for testing
	tmpDir := t.TempDir()

	// Initialize the tools logger
	err := InitToolsLogger(tmpDir, "tools.json")
	require.NoError(err, "InitToolsLogger failed")

	// Log some tools for a server
	tools := []ToolInfo{
		{Name: "tool1", Description: "First tool"},
		{Name: "tool2", Description: "Second tool"},
	}
	LogToolsForServer("server1", tools)

	// Read the tools.json file
	toolsPath := filepath.Join(tmpDir, "tools.json")
	data, err := os.ReadFile(toolsPath)
	require.NoError(err, "Failed to read tools.json")

	// Parse the JSON
	var toolsData ToolsData
	err = json.Unmarshal(data, &toolsData)
	require.NoError(err, "Failed to parse tools.json")

	// Verify the structure
	assert.Contains(toolsData.Servers, "server1", "Server should be in the map")
	assert.Len(toolsData.Servers["server1"], 2, "Should have 2 tools")
	assert.Equal("tool1", toolsData.Servers["server1"][0].Name)
	assert.Equal("First tool", toolsData.Servers["server1"][0].Description)
	assert.Equal("tool2", toolsData.Servers["server1"][1].Name)
	assert.Equal("Second tool", toolsData.Servers["server1"][1].Description)

	// Clean up
	err = CloseToolsLogger()
	assert.NoError(err, "CloseToolsLogger failed")
}

func TestToolsLoggerMultipleServers(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Create a temp directory for testing
	tmpDir := t.TempDir()

	// Initialize the tools logger
	err := InitToolsLogger(tmpDir, "tools.json")
	require.NoError(err, "InitToolsLogger failed")

	// Log tools for multiple servers
	tools1 := []ToolInfo{
		{Name: "tool1", Description: "Server 1 tool 1"},
		{Name: "tool2", Description: "Server 1 tool 2"},
	}
	LogToolsForServer("server1", tools1)

	tools2 := []ToolInfo{
		{Name: "tool3", Description: "Server 2 tool 1"},
	}
	LogToolsForServer("server2", tools2)

	// Read the tools.json file
	toolsPath := filepath.Join(tmpDir, "tools.json")
	data, err := os.ReadFile(toolsPath)
	require.NoError(err, "Failed to read tools.json")

	// Parse the JSON
	var toolsData ToolsData
	err = json.Unmarshal(data, &toolsData)
	require.NoError(err, "Failed to parse tools.json")

	// Verify both servers are present
	assert.Contains(toolsData.Servers, "server1", "Server1 should be in the map")
	assert.Contains(toolsData.Servers, "server2", "Server2 should be in the map")
	assert.Len(toolsData.Servers["server1"], 2, "Server1 should have 2 tools")
	assert.Len(toolsData.Servers["server2"], 1, "Server2 should have 1 tool")

	// Clean up
	err = CloseToolsLogger()
	assert.NoError(err, "CloseToolsLogger failed")
}

func TestToolsLoggerUpdate(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Create a temp directory for testing
	tmpDir := t.TempDir()

	// Initialize the tools logger
	err := InitToolsLogger(tmpDir, "tools.json")
	require.NoError(err, "InitToolsLogger failed")

	// Log initial tools
	tools1 := []ToolInfo{
		{Name: "tool1", Description: "Original tool"},
	}
	LogToolsForServer("server1", tools1)

	// Update with new tools
	tools2 := []ToolInfo{
		{Name: "tool2", Description: "Updated tool"},
		{Name: "tool3", Description: "Another tool"},
	}
	LogToolsForServer("server1", tools2)

	// Read the tools.json file
	toolsPath := filepath.Join(tmpDir, "tools.json")
	data, err := os.ReadFile(toolsPath)
	require.NoError(err, "Failed to read tools.json")

	// Parse the JSON
	var toolsData ToolsData
	err = json.Unmarshal(data, &toolsData)
	require.NoError(err, "Failed to parse tools.json")

	// Verify the tools were updated (not appended)
	assert.Len(toolsData.Servers["server1"], 2, "Should have 2 tools (updated, not appended)")
	assert.Equal("tool2", toolsData.Servers["server1"][0].Name)
	assert.Equal("tool3", toolsData.Servers["server1"][1].Name)

	// Clean up
	err = CloseToolsLogger()
	assert.NoError(err, "CloseToolsLogger failed")
}

func TestToolsLoggerEmptyTools(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Create a temp directory for testing
	tmpDir := t.TempDir()

	// Initialize the tools logger
	err := InitToolsLogger(tmpDir, "tools.json")
	require.NoError(err, "InitToolsLogger failed")

	// Log empty tools array
	tools := []ToolInfo{}
	LogToolsForServer("server1", tools)

	// Read the tools.json file
	toolsPath := filepath.Join(tmpDir, "tools.json")
	data, err := os.ReadFile(toolsPath)
	require.NoError(err, "Failed to read tools.json")

	// Parse the JSON
	var toolsData ToolsData
	err = json.Unmarshal(data, &toolsData)
	require.NoError(err, "Failed to parse tools.json")

	// Verify empty array is stored
	assert.Contains(toolsData.Servers, "server1", "Server should be in the map")
	assert.Empty(toolsData.Servers["server1"], "Should have 0 tools")

	// Clean up
	err = CloseToolsLogger()
	assert.NoError(err, "CloseToolsLogger failed")
}

func TestToolsLoggerFallback(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Try to initialize with an invalid directory
	err := InitToolsLogger("/nonexistent/invalid/path", "tools.json")
	// Should not error even if directory creation fails (fallback mode)
	require.NoError(err, "InitToolsLogger should not fail on fallback")

	// Logging should not cause errors in fallback mode
	tools := []ToolInfo{
		{Name: "tool1", Description: "Test tool"},
	}
	LogToolsForServer("server1", tools)

	// Clean up
	err = CloseToolsLogger()
	assert.NoError(err, "CloseToolsLogger failed")
}

func TestToolsLoggerJSONFormat(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Create a temp directory for testing
	tmpDir := t.TempDir()

	// Initialize the tools logger
	err := InitToolsLogger(tmpDir, "tools.json")
	require.NoError(err, "InitToolsLogger failed")

	// Log tools with special characters
	tools := []ToolInfo{
		{Name: "tool-with-dashes", Description: "Description with \"quotes\" and newlines\ntest"},
		{Name: "tool_with_underscores", Description: "Description with 'single quotes'"},
	}
	LogToolsForServer("server-1", tools)

	// Read the tools.json file
	toolsPath := filepath.Join(tmpDir, "tools.json")
	data, err := os.ReadFile(toolsPath)
	require.NoError(err, "Failed to read tools.json")

	// Verify it's valid JSON
	var toolsData ToolsData
	err = json.Unmarshal(data, &toolsData)
	require.NoError(err, "Should be valid JSON")

	// Verify special characters were preserved
	assert.Equal("tool-with-dashes", toolsData.Servers["server-1"][0].Name)
	assert.Contains(toolsData.Servers["server-1"][0].Description, "\"quotes\"")
	assert.Contains(toolsData.Servers["server-1"][0].Description, "\n")

	// Clean up
	err = CloseToolsLogger()
	assert.NoError(err, "CloseToolsLogger failed")
}
