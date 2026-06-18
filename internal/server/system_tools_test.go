package server

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validateToolContent(t *testing.T, result interface{}) string {
	t.Helper()

	require := require.New(t)
	assert := assert.New(t)

	resultMap, ok := result.(map[string]interface{})
	require.True(ok, "Result should be a map")

	content, ok := resultMap["content"].([]map[string]interface{})
	require.True(ok, "content field should be an array of maps")
	require.Len(content, 1, "Should have exactly 1 content item")

	contentItem := content[0]
	assert.Equal("text", contentItem["type"], "Content type should be text")

	text, ok := contentItem["text"].(string)
	require.True(ok, "text field should be a string")
	return text
}

func TestNewSysServer(t *testing.T) {
	tests := []struct {
		name      string
		serverIDs []string
		wantCount int
	}{
		{name: "empty server list", serverIDs: []string{}, wantCount: 0},
		{name: "single server", serverIDs: []string{"github"}, wantCount: 1},
		{name: "multiple servers", serverIDs: []string{"github", "slack", "jira"}, wantCount: 3},
		{name: "nil server list", serverIDs: nil, wantCount: 0},
		{name: "many servers", serverIDs: []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8", "s9", "s10"}, wantCount: 10},
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

func TestSysInit(t *testing.T) {
	tests := []struct {
		name      string
		serverIDs []string
	}{
		{name: "empty servers", serverIDs: []string{}},
		{name: "single server", serverIDs: []string{"github"}},
		{name: "many servers", serverIDs: []string{"github", "slack", "jira", "notion", "linear"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			server := NewSysServer(tt.serverIDs)
			result, err := server.SysInit()

			require.NoError(err)
			require.NotNil(result)

			text := validateToolContent(t, result)
			assert.Contains(text, "MCPG initialized")
			assert.Contains(text, "Available servers")
			for _, id := range tt.serverIDs {
				assert.Contains(text, id)
			}
		})
	}
}

func TestListServers(t *testing.T) {
	tests := []struct {
		name      string
		serverIDs []string
		expected  []string
	}{
		{name: "empty list", serverIDs: []string{}, expected: []string{"Configured MCP Servers:"}},
		{name: "single server", serverIDs: []string{"github"}, expected: []string{"Configured MCP Servers:", "1. github"}},
		{name: "multiple servers", serverIDs: []string{"github", "slack", "jira"}, expected: []string{"Configured MCP Servers:", "1. github", "2. slack", "3. jira"}},
		{name: "servers with special characters", serverIDs: []string{"server-1", "server_2", "server.3"}, expected: []string{"1. server-1", "2. server_2", "3. server.3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			server := NewSysServer(tt.serverIDs)
			result, err := server.ListServers()

			require.NoError(err)
			require.NotNil(result)

			text := validateToolContent(t, result)
			for _, expectedStr := range tt.expected {
				assert.Contains(text, expectedStr, "Output should contain: %s", expectedStr)
			}
		})
	}
}

func TestListServers_EmptyStringServerID(t *testing.T) {
	server := NewSysServer([]string{"github", "", "slack"})

	result, err := server.ListServers()
	require.NoError(t, err)
	require.NotNil(t, result)

	text := validateToolContent(t, result)
	assert.Contains(t, text, "github")
	assert.Contains(t, text, "slack")
	assert.Contains(t, text, "2. \n")
}

func TestSysServer_ConcurrentAccess(t *testing.T) {
	server := NewSysServer([]string{"github", "slack", "jira"})

	var wg sync.WaitGroup
	const numGoroutines = 10

	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := server.SysInit()
			assert.NoError(t, err)
			assert.NotNil(t, result)
		}()
	}

	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := server.ListServers()
			assert.NoError(t, err)
			assert.NotNil(t, result)
		}()
	}

	wg.Wait()
}
