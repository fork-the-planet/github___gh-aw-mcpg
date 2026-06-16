package mcptest_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/testutil/mcptest"
)

// TestCompleteWorkflow demonstrates a complete end-to-end test workflow
// This example shows how to:
// 1. Create a test server with tools and resources
// 2. Connect to it with a validator client
// 3. Explore its capabilities
// 4. Execute operations and validate results
func TestCompleteWorkflow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Step 1: Define a custom tool that performs meaningful work
	weatherTool := mcptest.ToolConfig{
		Name:        "get_weather",
		Description: "Get weather information for a city",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"city": map[string]interface{}{
					"type":        "string",
					"description": "Name of the city",
				},
				"units": map[string]interface{}{
					"type":        "string",
					"description": "Temperature units (celsius or fahrenheit)",
					"enum":        []string{"celsius", "fahrenheit"},
				},
			},
			"required": []string{"city"},
		},
		Handler: func(args map[string]interface{}) ([]sdk.Content, error) {
			city := args["city"].(string)
			units := "celsius"
			if u, ok := args["units"].(string); ok {
				units = u
			}

			// Simulate weather data
			temp := "22"
			if units == "fahrenheit" {
				temp = "72"
			}

			return []sdk.Content{
				&sdk.TextContent{
					Text: "Weather in " + city + ": " + temp + "° " + units,
				},
			}, nil
		},
	}

	// Step 2: Create a test server configuration
	serverConfig := &mcptest.ServerConfig{
		Name:    "weather-service",
		Version: "1.0.0",
		Tools:   []mcptest.ToolConfig{weatherTool},
		Resources: []mcptest.ResourceConfig{
			{
				URI:         "weather://cities",
				Name:        "Available Cities",
				Description: "List of cities with weather data",
				MimeType:    "text/plain",
				Content:     "New York, London, Tokyo, Paris, Sydney",
			},
		},
	}

	// Step 3: Set up test driver and create test server
	driver := mcptest.NewTestDriver()
	defer driver.Stop()

	require.NoError(t, driver.AddTestServer("weather", serverConfig), "Failed to add test server")

	// Step 4: Create transport and validator client
	transport, err := driver.CreateStdioTransport("weather")
	require.NoError(t, err, "Failed to create transport")

	validator, err := mcptest.NewValidatorClient(ctx, transport)
	require.NoError(t, err, "Failed to create validator client")
	defer validator.Close()

	// Step 5: Validate server information
	serverInfo := validator.GetServerInfo()
	require.NotNil(t, serverInfo, "Server info should not be nil")
	assert.Equal(t, "weather-service", serverInfo.Name)

	// Step 6: List and validate tools
	tools, err := validator.ListTools()
	require.NoError(t, err, "Failed to list tools")
	require.Len(t, tools, 1, "Expected exactly 1 tool")
	assert.Equal(t, "get_weather", tools[0].Name)
	assert.Equal(t, "Get weather information for a city", tools[0].Description)

	// Step 7: List and validate resources
	resources, err := validator.ListResources()
	require.NoError(t, err, "Failed to list resources")
	require.Len(t, resources, 1, "Expected exactly 1 resource")
	assert.Equal(t, "weather://cities", resources[0].URI)

	// Step 8: Call the weather tool with different parameters
	testCases := []struct {
		name          string
		args          map[string]interface{}
		expectedMatch string
	}{
		{
			name:          "Weather in default units",
			args:          map[string]interface{}{"city": "London"},
			expectedMatch: "Weather in London: 22° celsius",
		},
		{
			name:          "Weather in fahrenheit",
			args:          map[string]interface{}{"city": "Tokyo", "units": "fahrenheit"},
			expectedMatch: "Weather in Tokyo: 72° fahrenheit",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := validator.CallTool("get_weather", tc.args)
			require.NoError(t, err, "Failed to call tool")
			assert.False(t, result.IsError, "Tool should not return an error")
			require.Len(t, result.Content, 1, "Expected exactly 1 content item")
			textContent, ok := result.Content[0].(*sdk.TextContent)
			require.True(t, ok, "Expected *sdk.TextContent")
			assert.Equal(t, tc.expectedMatch, textContent.Text)
		})
	}

	// Step 9: Read resource content
	resourceResult, err := validator.ReadResource("weather://cities")
	require.NoError(t, err, "Failed to read resource")
	require.Len(t, resourceResult.Contents, 1, "Expected exactly 1 content item")
	assert.Equal(t, "New York, London, Tokyo, Paris, Sydney", resourceResult.Contents[0].Text)
}
