package strutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetStringFromMap(t *testing.T) {
	t.Parallel()

	t.Run("returns string value when present", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"owner": "octo"}
		assert.Equal(t, "octo", GetStringFromMap(m, "owner"))
	})

	t.Run("returns empty string for missing key", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"owner": "octo"}
		assert.Equal(t, "", GetStringFromMap(m, "repo"))
	})

	t.Run("returns empty string for non-string value", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"number": float64(1)}
		assert.Equal(t, "", GetStringFromMap(m, "number"))
	})

	t.Run("returns empty string for nil map", func(t *testing.T) {
		t.Parallel()
		var m map[string]interface{}
		assert.Equal(t, "", GetStringFromMap(m, "owner"))
	})
}

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
