package strutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeepCloneJSONMap(t *testing.T) {
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

func TestDeepCloneJSONPrimitives(t *testing.T) {
	assert.Equal(t, "hello", DeepCloneJSON("hello"))
	assert.Equal(t, float64(3.14), DeepCloneJSON(float64(3.14)))
	assert.Equal(t, true, DeepCloneJSON(true))
	assert.Nil(t, DeepCloneJSON(nil))
}
