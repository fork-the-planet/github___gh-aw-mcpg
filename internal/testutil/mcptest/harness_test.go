package mcptest_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/testutil/mcptest"
)

// TestBasicServerWithOneTool tests a basic MCP server with a single tool
func TestBasicServerWithOneTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config := mcptest.DefaultServerConfig().
		WithTool(mcptest.SimpleEchoTool("test_echo"))

	driver := mcptest.NewTestDriver()
	defer driver.Stop()

	err := driver.AddTestServer("test", config)
	require.NoError(t, err, "Failed to add test server")

	transport, err := driver.CreateStdioTransport("test")
	require.NoError(t, err, "Failed to create transport")

	validator, err := mcptest.NewValidatorClient(ctx, transport)
	require.NoError(t, err, "Failed to create validator client")
	defer validator.Close()

	// Validate tools
	tools, err := validator.ListTools()
	require.NoError(t, err, "Failed to list tools")
	require.Len(t, tools, 1, "Expected exactly 1 tool")
	assert.Equal(t, "test_echo", tools[0].Name)

	// Test tool execution
	result, err := validator.CallTool("test_echo", map[string]interface{}{
		"message": "Hello, World!",
	})
	require.NoError(t, err, "Failed to call tool")
	assert.False(t, result.IsError)
	require.Len(t, result.Content, 1, "Expected exactly 1 content item")
	textContent, ok := result.Content[0].(*sdk.TextContent)
	require.True(t, ok, "Expected *sdk.TextContent")
	assert.Equal(t, "Echo: Hello, World!", textContent.Text)
}

// TestServerWithMultipleTools tests a server with multiple tools
func TestServerWithMultipleTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create test server with multiple tools
	config := mcptest.DefaultServerConfig().
		WithTool(mcptest.SimpleEchoTool("echo1")).
		WithTool(mcptest.SimpleEchoTool("echo2")).
		WithTool(mcptest.ToolConfig{
			Name:        "add",
			Description: "Adds two numbers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{"type": "number"},
					"b": map[string]interface{}{"type": "number"},
				},
				"required": []string{"a", "b"},
			},
			Handler: func(args map[string]interface{}) ([]sdk.Content, error) {
				a, _ := args["a"].(float64)
				b, _ := args["b"].(float64)
				sum := a + b
				return []sdk.Content{
					&sdk.TextContent{
						Text: fmt.Sprintf("%g", sum),
					},
				}, nil
			},
		})

	driver := mcptest.NewTestDriver()
	defer driver.Stop()

	err := driver.AddTestServer("test", config)
	require.NoError(t, err, "Failed to add test server")

	transport, err := driver.CreateStdioTransport("test")
	require.NoError(t, err, "Failed to create transport")

	validator, err := mcptest.NewValidatorClient(ctx, transport)
	require.NoError(t, err, "Failed to create validator")
	defer validator.Close()

	// Validate: Should have 3 tools
	tools, err := validator.ListTools()
	require.NoError(t, err, "Failed to list tools")
	require.Len(t, tools, 3, "Expected exactly 3 tools")

	// Verify all expected tools are present
	toolNames := make([]string, len(tools))
	for i, tool := range tools {
		toolNames[i] = tool.Name
	}
	assert.ElementsMatch(t, []string{"echo1", "echo2", "add"}, toolNames)

	// Test the add tool
	result, err := validator.CallTool("add", map[string]interface{}{
		"a": 5.0,
		"b": 3.0,
	})
	require.NoError(t, err, "Failed to call add tool")
	assert.False(t, result.IsError)
	require.Len(t, result.Content, 1, "Expected exactly 1 content item")
	textContent, ok := result.Content[0].(*sdk.TextContent)
	require.True(t, ok, "Expected *sdk.TextContent")
	assert.Equal(t, "8", textContent.Text)
}

// TestServerWithResources tests a server with resources
func TestServerWithResources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create test server with resources
	config := mcptest.DefaultServerConfig().
		WithResource(mcptest.ResourceConfig{
			URI:         "test://doc1",
			Name:        "Document 1",
			Description: "A test document",
			MimeType:    "text/plain",
			Content:     "This is test content",
		})

	driver := mcptest.NewTestDriver()
	defer driver.Stop()

	err := driver.AddTestServer("test", config)
	require.NoError(t, err, "Failed to add test server")

	transport, err := driver.CreateStdioTransport("test")
	require.NoError(t, err, "Failed to create transport")

	validator, err := mcptest.NewValidatorClient(ctx, transport)
	require.NoError(t, err, "Failed to create validator")
	defer validator.Close()

	// Test: List resources
	resources, err := validator.ListResources()
	require.NoError(t, err, "Failed to list resources")
	require.Len(t, resources, 1, "Expected exactly 1 resource")
	assert.Equal(t, "test://doc1", resources[0].URI)

	// Test: Read resource
	readResult, err := validator.ReadResource("test://doc1")
	require.NoError(t, err, "Failed to read resource")
	require.Len(t, readResult.Contents, 1, "Expected exactly 1 content item")
	assert.Equal(t, "test://doc1", readResult.Contents[0].URI)
	assert.Equal(t, "This is test content", readResult.Contents[0].Text)
}

// TestServerInfo validates server metadata
func TestServerInfo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create test server with custom name and version
	config := &mcptest.ServerConfig{
		Name:    "custom-test-server",
		Version: "2.5.0",
		Tools:   []mcptest.ToolConfig{},
	}

	driver := mcptest.NewTestDriver()
	defer driver.Stop()

	err := driver.AddTestServer("test", config)
	require.NoError(t, err, "Failed to add test server")

	transport, err := driver.CreateStdioTransport("test")
	require.NoError(t, err, "Failed to create transport")

	validator, err := mcptest.NewValidatorClient(ctx, transport)
	require.NoError(t, err, "Failed to create validator")
	defer validator.Close()

	// Test: Get server info
	serverInfo := validator.GetServerInfo()
	require.NotNil(t, serverInfo, "Server info is nil")
	assert.Equal(t, "custom-test-server", serverInfo.Name)
	assert.Equal(t, "2.5.0", serverInfo.Version)
}
