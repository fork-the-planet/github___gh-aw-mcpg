package jsonutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeepCloneJSON_Map(t *testing.T) {
	original := map[string]interface{}{
		"id": float64(1),
		"nested": map[string]interface{}{
			"value": "original",
		},
		"tags": []interface{}{"go", "test"},
	}

	cloned := DeepCloneJSON(original)
	clonedMap, ok := cloned.(map[string]interface{})
	require.True(t, ok)

	original["nested"].(map[string]interface{})["value"] = "mutated"
	original["tags"].([]interface{})[0] = "mutated-tag"

	assert.Equal(t, "original", clonedMap["nested"].(map[string]interface{})["value"])
	assert.Equal(t, "go", clonedMap["tags"].([]interface{})[0])
}

func TestDeepCloneJSON_Slice(t *testing.T) {
	original := []interface{}{
		map[string]interface{}{"id": float64(1)},
		"hello",
	}

	cloned := DeepCloneJSON(original)
	clonedSlice, ok := cloned.([]interface{})
	require.True(t, ok)
	assert.Len(t, clonedSlice, 2)

	original[0].(map[string]interface{})["id"] = float64(99)
	assert.Equal(t, float64(1), clonedSlice[0].(map[string]interface{})["id"])
}

func TestDeepCloneJSON_Primitives(t *testing.T) {
	assert.Equal(t, "hello", DeepCloneJSON("hello"))
	assert.Equal(t, float64(3.14), DeepCloneJSON(float64(3.14)))
	assert.Equal(t, true, DeepCloneJSON(true))
	assert.Nil(t, DeepCloneJSON(nil))
}
