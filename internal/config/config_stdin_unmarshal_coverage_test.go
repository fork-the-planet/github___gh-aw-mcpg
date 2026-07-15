package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStdinServerConfig_UnmarshalJSON_ErrorPaths tests the error branches of
// StdinServerConfig.UnmarshalJSON that are not covered by existing tests.
func TestStdinServerConfig_UnmarshalJSON_ErrorPaths(t *testing.T) {
	t.Run("invalid JSON returns error", func(t *testing.T) {
		var server StdinServerConfig
		err := server.UnmarshalJSON([]byte(`not valid json`))
		require.Error(t, err)
	})

	t.Run("connect_timeout with non-integer value returns error", func(t *testing.T) {
		// The legacy snake_case alias connect_timeout must be a valid integer.
		// Passing a non-integer triggers the assignLegacyIntAlias error path.
		data := []byte(`{
			"type": "http",
			"url": "https://example.com/mcp",
			"connect_timeout": "not-a-number"
		}`)

		var server StdinServerConfig
		err := server.UnmarshalJSON(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "connect_timeout")
	})

	t.Run("tool_timeout with non-integer value returns error", func(t *testing.T) {
		// The legacy snake_case alias tool_timeout must be a valid integer.
		// Passing a non-integer triggers the assignLegacyIntAlias error path.
		data := []byte(`{
			"type": "http",
			"url": "https://example.com/mcp",
			"tool_timeout": "not-a-number"
		}`)

		var server StdinServerConfig
		err := server.UnmarshalJSON(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tool_timeout")
	})

	t.Run("connect_timeout as float returns error", func(t *testing.T) {
		// Floats are not valid integers for connect_timeout.
		data := []byte(`{
			"type": "http",
			"url": "https://example.com/mcp",
			"connect_timeout": 3.14
		}`)

		var server StdinServerConfig
		err := server.UnmarshalJSON(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "connect_timeout")
	})

	t.Run("tool_timeout as float returns error", func(t *testing.T) {
		// Floats are not valid integers for tool_timeout.
		data := []byte(`{
			"type": "http",
			"url": "https://example.com/mcp",
			"tool_timeout": 1.5
		}`)

		var server StdinServerConfig
		err := server.UnmarshalJSON(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tool_timeout")
	})

	t.Run("connect_timeout as null is treated as zero", func(t *testing.T) {
		// json.Unmarshal decodes null into an int as 0, so the field gets a pointer to 0.
		data := []byte(`{
			"type": "http",
			"url": "https://example.com/mcp",
			"connect_timeout": null
		}`)

		var server StdinServerConfig
		err := server.UnmarshalJSON(data)
		require.NoError(t, err)
		require.NotNil(t, server.ConnectTimeout, "null connect_timeout decodes to a pointer to 0")
		assert.Equal(t, 0, *server.ConnectTimeout)
	})

	t.Run("both legacy timeout fields present simultaneously", func(t *testing.T) {
		// When both snake_case and camelCase are present the camelCase value wins
		// because it is decoded first by the embedded alias struct.
		data := []byte(`{
			"type": "http",
			"url": "https://example.com/mcp",
			"connectTimeout": 60,
			"toolTimeout": 300,
			"connect_timeout": 30,
			"tool_timeout": 120
		}`)

		var server StdinServerConfig
		err := server.UnmarshalJSON(data)
		require.NoError(t, err)
		require.NotNil(t, server.ConnectTimeout)
		require.NotNil(t, server.ToolTimeout)
		// camelCase fields are decoded by the embedded alias; snake_case aliases
		// are only applied when the primary pointer is nil.
		assert.Equal(t, 60, *server.ConnectTimeout)
		assert.Equal(t, 300, *server.ToolTimeout)
	})

	t.Run("only tool_timeout snake_case sets toolTimeoutFieldName", func(t *testing.T) {
		// When toolTimeout (camelCase) is absent and tool_timeout (snake_case) is
		// present, toolTimeoutFieldName should be set to "tool_timeout".
		data := []byte(`{
			"type": "http",
			"url": "https://example.com/mcp",
			"tool_timeout": 90
		}`)

		var server StdinServerConfig
		err := server.UnmarshalJSON(data)
		require.NoError(t, err)
		assert.Equal(t, "tool_timeout", server.toolTimeoutField())
	})

	t.Run("toolTimeout camelCase keeps default toolTimeoutFieldName", func(t *testing.T) {
		// When toolTimeout (camelCase) is present, toolTimeoutFieldName stays as
		// "toolTimeout" (the default set in UnmarshalJSON).
		data := []byte(`{
			"type": "http",
			"url": "https://example.com/mcp",
			"toolTimeout": 90
		}`)

		var server StdinServerConfig
		err := server.UnmarshalJSON(data)
		require.NoError(t, err)
		assert.Equal(t, "toolTimeout", server.toolTimeoutField())
	})

	t.Run("unknown fields are collected in AdditionalProperties", func(t *testing.T) {
		data := []byte(`{
			"type": "stdio",
			"container": "ghcr.io/example/server:latest",
			"customField": "customValue",
			"anotherExtra": 42
		}`)

		var server StdinServerConfig
		err := server.UnmarshalJSON(data)
		require.NoError(t, err)
		assert.Equal(t, "customValue", server.AdditionalProperties["customField"])
		// Numbers are stored as json.Number (not float64) to preserve precision for
		// large integers such as 9007199254740993 that cannot be represented by float64.
		assert.Equal(t, json.Number("42"), server.AdditionalProperties["anotherExtra"])
		// Known fields must not appear in AdditionalProperties.
		_, typeExists := server.AdditionalProperties["type"]
		assert.False(t, typeExists, "known field 'type' should not appear in AdditionalProperties")
	})
}
