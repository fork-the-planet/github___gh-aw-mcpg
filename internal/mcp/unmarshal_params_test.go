package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnmarshalParams tests the unmarshalParams helper function
func TestUnmarshalParams(t *testing.T) {
	t.Run("successful unmarshal with map params", func(t *testing.T) {
		params := map[string]interface{}{
			"name": "test-tool",
			"arguments": map[string]interface{}{
				"query": "search term",
			},
		}

		var target CallToolParams
		err := unmarshalParams(params, &target)

		require.NoError(t, err, "Should successfully unmarshal map params")
		assert.Equal(t, "test-tool", target.Name)
		assert.NotNil(t, target.Arguments)
		assert.Equal(t, "search term", target.Arguments["query"])
	})

	t.Run("successful unmarshal with struct params", func(t *testing.T) {
		type testParams struct {
			URI string `json:"uri"`
		}

		params := map[string]interface{}{
			"uri": "file:///test.txt",
		}

		var target testParams
		err := unmarshalParams(params, &target)

		require.NoError(t, err, "Should successfully unmarshal to struct")
		assert.Equal(t, "file:///test.txt", target.URI)
	})

	t.Run("successful unmarshal with nested struct", func(t *testing.T) {
		type nestedParams struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}

		params := map[string]interface{}{
			"name": "test-prompt",
			"arguments": map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
		}

		var target nestedParams
		err := unmarshalParams(params, &target)

		require.NoError(t, err, "Should successfully unmarshal nested struct")
		assert.Equal(t, "test-prompt", target.Name)
		assert.NotNil(t, target.Arguments)
		assert.Equal(t, "value1", target.Arguments["key1"])
		assert.Equal(t, "value2", target.Arguments["key2"])
	})

	t.Run("successful unmarshal with nil arguments", func(t *testing.T) {
		params := map[string]interface{}{
			"name":      "test-tool",
			"arguments": nil,
		}

		var target CallToolParams
		err := unmarshalParams(params, &target)

		require.NoError(t, err, "Should handle nil arguments")
		assert.Equal(t, "test-tool", target.Name)
		assert.Nil(t, target.Arguments, "Arguments should be nil when input is nil")
	})

	t.Run("successful unmarshal with empty params", func(t *testing.T) {
		params := map[string]interface{}{}

		var target struct {
			URI string `json:"uri"`
		}
		err := unmarshalParams(params, &target)

		require.NoError(t, err, "Should handle empty params")
		assert.Equal(t, "", target.URI, "URI should be empty string when not provided")
	})

	t.Run("fail on unmarshalable input type", func(t *testing.T) {
		// Channels cannot be marshaled to JSON
		params := make(chan int)

		var target CallToolParams
		err := unmarshalParams(params, &target)

		assert.Error(t, err, "Should fail to marshal channel type")
		assert.Contains(t, err.Error(), "failed to marshal params", "Error should mention marshal failure")
	})

	t.Run("fail on invalid JSON structure", func(t *testing.T) {
		// Params with type that can't convert to target struct
		params := map[string]interface{}{
			"name": 12345, // Should be string
		}

		var target struct {
			Name []string `json:"name"` // Expecting array, getting number
		}
		err := unmarshalParams(params, &target)

		assert.Error(t, err, "Should fail on type mismatch")
		assert.Contains(t, err.Error(), "invalid params", "Error should mention invalid params")
	})

	t.Run("fail with non-pointer target", func(t *testing.T) {
		params := map[string]interface{}{
			"uri": "file:///test.txt",
		}

		// Non-pointer target should cause unmarshal to fail
		var target struct {
			URI string `json:"uri"`
		}
		err := unmarshalParams(params, target) // Note: not &target

		assert.Error(t, err, "Should fail when target is not a pointer")
		assert.Contains(t, err.Error(), "invalid params", "Error should mention invalid params")
	})

	t.Run("successful unmarshal preserves JSON types", func(t *testing.T) {
		params := map[string]interface{}{
			"string_val":  "test",
			"int_val":     42,
			"float_val":   3.14,
			"bool_val":    true,
			"array_val":   []interface{}{"a", "b", "c"},
			"object_val":  map[string]interface{}{"nested": "value"},
			"null_val":    nil,
		}

		var target map[string]interface{}
		err := unmarshalParams(params, &target)

		require.NoError(t, err, "Should successfully preserve JSON types")
		assert.Equal(t, "test", target["string_val"])
		// JSON unmarshal converts numbers to float64 by default
		assert.Equal(t, float64(42), target["int_val"])
		assert.Equal(t, 3.14, target["float_val"])
		assert.Equal(t, true, target["bool_val"])
		assert.Len(t, target["array_val"], 3)
		assert.NotNil(t, target["object_val"])
		assert.Nil(t, target["null_val"])
	})

	t.Run("successful unmarshal with JSON tags", func(t *testing.T) {
		type taggedParams struct {
			FieldOne   string `json:"field_one"`
			FieldTwo   int    `json:"field_two"`
			FieldThree bool   `json:"field_three,omitempty"`
		}

		params := map[string]interface{}{
			"field_one": "test",
			"field_two": 42,
		}

		var target taggedParams
		err := unmarshalParams(params, &target)

		require.NoError(t, err, "Should handle JSON tags")
		assert.Equal(t, "test", target.FieldOne)
		assert.Equal(t, 42, target.FieldTwo)
		assert.False(t, target.FieldThree, "Omitted field should be zero value")
	})

	t.Run("successful unmarshal with json.RawMessage input", func(t *testing.T) {
		// Simulate params coming from json.RawMessage (common in MCP protocol)
		jsonStr := `{"name":"test-tool","arguments":{"key":"value"}}`
		var rawParams interface{}
		err := json.Unmarshal([]byte(jsonStr), &rawParams)
		require.NoError(t, err)

		var target CallToolParams
		err = unmarshalParams(rawParams, &target)

		require.NoError(t, err, "Should handle json.RawMessage input")
		assert.Equal(t, "test-tool", target.Name)
		assert.Equal(t, "value", target.Arguments["key"])
	})
}

// TestUnmarshalParams_ErrorMessages tests that error messages are informative
func TestUnmarshalParams_ErrorMessages(t *testing.T) {
	t.Run("marshal error has descriptive message", func(t *testing.T) {
		params := make(chan int) // Unmarshalable type
		var target CallToolParams

		err := unmarshalParams(params, &target)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to marshal params")
		// Error should wrap the original JSON error
		assert.Contains(t, err.Error(), "json")
	})

	t.Run("unmarshal error has descriptive message", func(t *testing.T) {
		params := map[string]interface{}{
			"name": []int{1, 2, 3}, // Should be string
		}
		var target struct {
			Name string `json:"name"`
		}

		err := unmarshalParams(params, &target)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid params")
	})
}

// TestUnmarshalParams_Integration tests integration with actual MCP types
func TestUnmarshalParams_Integration(t *testing.T) {
	t.Run("CallToolParams integration", func(t *testing.T) {
		params := map[string]interface{}{
			"name": "github_search_code",
			"arguments": map[string]interface{}{
				"query":  "test",
				"repo":   "github/gh-aw-mcpg",
			},
		}

		var target CallToolParams
		err := unmarshalParams(params, &target)

		require.NoError(t, err)
		assert.Equal(t, "github_search_code", target.Name)
		assert.Equal(t, "test", target.Arguments["query"])
		assert.Equal(t, "github/gh-aw-mcpg", target.Arguments["repo"])
	})

	t.Run("readResource params integration", func(t *testing.T) {
		params := map[string]interface{}{
			"uri": "file:///home/user/config.toml",
		}

		var target struct {
			URI string `json:"uri"`
		}
		err := unmarshalParams(params, &target)

		require.NoError(t, err)
		assert.Equal(t, "file:///home/user/config.toml", target.URI)
	})

	t.Run("getPrompt params integration", func(t *testing.T) {
		params := map[string]interface{}{
			"name": "commit-message",
			"arguments": map[string]interface{}{
				"type":  "feat",
				"scope": "api",
			},
		}

		var target struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		err := unmarshalParams(params, &target)

		require.NoError(t, err)
		assert.Equal(t, "commit-message", target.Name)
		assert.Equal(t, "feat", target.Arguments["type"])
		assert.Equal(t, "api", target.Arguments["scope"])
	})
}
