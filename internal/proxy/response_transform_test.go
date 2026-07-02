package proxy

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/util"
	"github.com/stretchr/testify/assert"
)

// These tests complement existing response-transform helper tests in proxy_test.go
// by exercising defensive and less-common branches in response_transform.go.
//
// TestRewrapSearchResponse_Repositories verifies that rewrapSearchResponse uses the
// "repositories" key when the original search envelope uses that field name.
func TestRewrapSearchResponse_Repositories(t *testing.T) {
	original := map[string]interface{}{
		"total_count":        float64(5),
		"incomplete_results": false,
		"repositories":       []interface{}{"r1", "r2", "r3", "r4", "r5"},
	}
	filtered := []interface{}{"r1", "r2"}

	result := rewrapSearchResponse(original, filtered)

	m, ok := result.(map[string]interface{})
	assert.True(t, ok, "result should be a map")
	assert.Equal(t, float64(2), m["total_count"], "total_count should reflect filtered count")
	incompleteResults, ok := m["incomplete_results"].(bool)
	assert.True(t, ok, "incomplete_results should be a bool")
	assert.False(t, incompleteResults)
	repos, ok := m["repositories"].([]interface{})
	assert.True(t, ok, "repositories key should be present")
	assert.Len(t, repos, 2)
	// "items" key must not be introduced
	_, hasItems := m["items"]
	assert.False(t, hasItems, "items key should not be introduced when original used repositories")
}

// TestRewrapSearchResponse_FilteredItemsNotSlice verifies that rewrapSearchResponse
// returns filteredItems unchanged when filteredItems is not a []interface{}.
// This exercises the type-assertion guard added after the total_count check.
func TestRewrapSearchResponse_FilteredItemsNotSlice(t *testing.T) {
	original := map[string]interface{}{
		"total_count": float64(3),
		"items":       []interface{}{"a", "b", "c"},
	}
	// filteredItems is a string, not a []interface{} — type assertion must fail gracefully.
	filtered := "not a slice"

	result := rewrapSearchResponse(original, filtered)

	assert.Equal(t, filtered, result, "non-slice filteredItems should be returned unchanged")
}

// TestRewrapSearchResponse_NeitherItemsNorRepositories verifies that
// rewrapSearchResponse still rebuilds the envelope (with total_count and
// incomplete_results) even when the original contains neither an "items" nor a
// "repositories" key.  The function iterates the known keys and silently skips
// if none match; the caller receives a map with updated metadata but no
// items/repositories field.
func TestRewrapSearchResponse_NeitherItemsNorRepositories(t *testing.T) {
	original := map[string]interface{}{
		"total_count": float64(2),
		// intentionally no "items" or "repositories" key
		"other_field": "value",
	}
	filtered := []interface{}{"x", "y"}

	result := rewrapSearchResponse(original, filtered)

	m, ok := result.(map[string]interface{})
	assert.True(t, ok, "result should be a map")
	assert.Equal(t, float64(2), m["total_count"], "total_count updated to filtered length")
	incompleteResults, ok := m["incomplete_results"].(bool)
	assert.True(t, ok, "incomplete_results should be a bool")
	assert.False(t, incompleteResults)
	assert.Equal(t, "value", m["other_field"], "unrelated fields should be preserved")
	_, hasItems := m["items"]
	assert.False(t, hasItems, "items key should not be created when absent from original")
	_, hasRepos := m["repositories"]
	assert.False(t, hasRepos, "repositories key should not be created when absent from original")
}

// TestRebuildGraphQLResponse_NoDataField verifies that rebuildGraphQLResponse
// returns {"data": nil} when the original map exists but has no "data" key.
// This exercises the guard added after the non-map early-return path.
func TestRebuildGraphQLResponse_NoDataField(t *testing.T) {
	original := map[string]interface{}{
		"errors": []interface{}{
			map[string]interface{}{"message": "some error"},
		},
	}
	filtered := &difc.FilteredCollectionLabeledData{
		Accessible: []difc.LabeledItem{{Data: "item"}},
	}

	result := rebuildGraphQLResponse(original, filtered)

	m, ok := result.(map[string]interface{})
	assert.True(t, ok, "result should be a map")
	assert.Nil(t, m["data"], "data should be nil when original has no data field")
}

// TestDeepCloneJSON_Slice verifies that deepCloneJSON deep-copies a top-level
// []interface{} so that mutations to the original slice do not affect the clone.
func TestDeepCloneJSON_Slice(t *testing.T) {
	original := []interface{}{
		map[string]interface{}{"id": float64(1)},
		"hello",
		float64(42),
	}

	cloned := util.DeepCloneJSON(original)

	clonedSlice, ok := cloned.([]interface{})
	assert.True(t, ok, "cloned value should be a []interface{}")
	assert.Len(t, clonedSlice, 3)

	// Mutate the original slice element (the nested map).
	original[0].(map[string]interface{})["id"] = float64(99)

	// Clone should be unaffected.
	assert.Equal(t, float64(1), clonedSlice[0].(map[string]interface{})["id"],
		"clone should not be affected by mutation of original")
}

// TestDeepCloneJSON_Primitive verifies that deepCloneJSON returns primitive values
// (string, number, bool, nil) unchanged — they are immutable so no copy is needed.
func TestDeepCloneJSON_Primitive(t *testing.T) {
	assert.Equal(t, "hello", util.DeepCloneJSON("hello"))
	assert.Equal(t, float64(3.14), util.DeepCloneJSON(float64(3.14)))
	clonedBool, ok := util.DeepCloneJSON(true).(bool)
	assert.True(t, ok, "cloned bool should remain a bool")
	assert.True(t, clonedBool)
	assert.Nil(t, util.DeepCloneJSON(nil))
}

// TestUnwrapSingleObject_NilFilteredData verifies that unwrapSingleObject handles
// a nil filteredData value gracefully — it should be returned as-is since it is
// not a single-element []interface{}.
func TestUnwrapSingleObject_NilFilteredData(t *testing.T) {
	original := map[string]interface{}{"name": "file.txt"}
	result := unwrapSingleObject(original, nil)
	assert.Nil(t, result)
}
