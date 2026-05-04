package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarshalToResponse tests the marshalToResponse helper function
func TestMarshalToResponse(t *testing.T) {
	t.Run("successful marshal with simple struct", func(t *testing.T) {
		result := map[string]interface{}{
			"tools": []string{"tool1", "tool2"},
		}

		response, err := marshalToResponse(result)

		require.NoError(t, err, "Should successfully marshal simple struct")
		assert.NotNil(t, response, "Response should not be nil")
		assert.Equal(t, "2.0", response.JSONRPC, "JSONRPC version should be 2.0")
		assert.Equal(t, 1, response.ID, "ID should be 1")
		assert.NotNil(t, response.Result, "Result should not be nil")

		// Verify the marshaled result can be unmarshaled
		var unmarshaledResult map[string]interface{}
		err = json.Unmarshal(response.Result, &unmarshaledResult)
		require.NoError(t, err, "Should unmarshal result")

		// Check that tools field exists and has correct values (JSON unmarshaling converts []string to []interface{})
		tools, ok := unmarshaledResult["tools"].([]interface{})
		require.True(t, ok, "tools field should be an array")
		assert.Len(t, tools, 2, "Should have 2 tools")
		assert.Equal(t, "tool1", tools[0])
		assert.Equal(t, "tool2", tools[1])
	})

	t.Run("successful marshal with complex nested struct", func(t *testing.T) {
		result := map[string]interface{}{
			"tools": []map[string]interface{}{
				{
					"name":        "search",
					"description": "Search for items",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"query": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
			},
		}

		response, err := marshalToResponse(result)

		require.NoError(t, err, "Should successfully marshal complex struct")
		assert.NotNil(t, response, "Response should not be nil")

		// Verify the marshaled result structure
		var unmarshaledResult map[string]interface{}
		err = json.Unmarshal(response.Result, &unmarshaledResult)
		require.NoError(t, err, "Should unmarshal result")

		// Verify the structure is preserved
		tools, ok := unmarshaledResult["tools"].([]interface{})
		require.True(t, ok, "tools field should be an array")
		assert.Len(t, tools, 1, "Should have 1 tool")

		tool, ok := tools[0].(map[string]interface{})
		require.True(t, ok, "tool should be a map")
		assert.Equal(t, "search", tool["name"])
		assert.Equal(t, "Search for items", tool["description"])
		assert.NotNil(t, tool["inputSchema"])
	})

	t.Run("successful marshal with nil result", func(t *testing.T) {
		response, err := marshalToResponse(nil)

		require.NoError(t, err, "Should handle nil result")
		assert.NotNil(t, response, "Response should not be nil")
		assert.Equal(t, "2.0", response.JSONRPC)
		assert.Equal(t, 1, response.ID)
		// nil marshals to "null" in JSON
		assert.Equal(t, json.RawMessage("null"), response.Result)
	})

	t.Run("successful marshal with empty struct", func(t *testing.T) {
		result := map[string]interface{}{}

		response, err := marshalToResponse(result)

		require.NoError(t, err, "Should handle empty struct")
		assert.NotNil(t, response, "Response should not be nil")
		assert.Equal(t, "2.0", response.JSONRPC)
		assert.Equal(t, 1, response.ID)

		var unmarshaledResult map[string]interface{}
		err = json.Unmarshal(response.Result, &unmarshaledResult)
		require.NoError(t, err, "Should unmarshal empty result")
		assert.Empty(t, unmarshaledResult, "Unmarshaled result should be empty")
	})

	t.Run("fail on unmarshalable type", func(t *testing.T) {
		// Channels cannot be marshaled to JSON
		unmarshalableResult := make(chan int)

		response, err := marshalToResponse(unmarshalableResult)

		assert.Error(t, err, "Should fail to marshal channel type")
		assert.Nil(t, response, "Response should be nil on error")
		assert.ErrorContains(t, err, "failed to marshal result", "Error should mention marshal failure")
	})
}

// unmarshalableType is a type that cannot be marshaled to JSON
type unmarshalableType struct {
	Channel chan int
}

func TestMarshalToResponse_UnmarshalableStruct(t *testing.T) {
	result := unmarshalableType{
		Channel: make(chan int),
	}

	response, err := marshalToResponse(result)

	assert.Error(t, err, "Should fail to marshal struct with channel")
	assert.Nil(t, response, "Response should be nil on error")
	assert.ErrorContains(t, err, "failed to marshal result", "Error should mention marshal failure")
}

// TestMarshalToResponse_ResponseStructure validates the Response structure
func TestMarshalToResponse_ResponseStructure(t *testing.T) {
	result := map[string]string{"key": "value"}

	response, err := marshalToResponse(result)

	require.NoError(t, err)

	// Verify Response structure fields
	assert.IsType(t, &Response{}, response, "Should return Response pointer")
	assert.IsType(t, "", response.JSONRPC, "JSONRPC should be string")
	assert.IsType(t, 0, response.ID, "ID should be integer")
	assert.IsType(t, json.RawMessage{}, response.Result, "Result should be json.RawMessage")
	assert.Nil(t, response.Error, "Error should be nil for successful response")
}
