package proxy

import (
	"testing"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for branches in response_transform.go not yet covered by the existing
// response_transform_test.go or replace_nodes_array_test.go.

// ---------------------------------------------------------------------------
// rewrapSearchResponse — missing branches
// ---------------------------------------------------------------------------

// TestRewrapSearchResponse_NonMapOriginal verifies that when originalData is not
// a map (e.g. a slice or scalar), the function returns filteredItems unchanged
// without panicking.
func TestRewrapSearchResponse_NonMapOriginal(t *testing.T) {
	filtered := []interface{}{"a", "b"}

	tests := []struct {
		name     string
		original interface{}
	}{
		{"nil", nil},
		{"slice", []interface{}{"x"}},
		{"string", "not a map"},
		{"number", float64(42)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rewrapSearchResponse(tt.original, filtered)
			assert.Equal(t, filtered, result, "non-map original should return filteredItems unchanged")
		})
	}
}

// TestRewrapSearchResponse_NoTotalCount verifies that when the original map does
// not have a "total_count" field, filteredItems is returned unchanged.
func TestRewrapSearchResponse_NoTotalCount(t *testing.T) {
	original := map[string]interface{}{
		"items": []interface{}{"a", "b"},
		// deliberately no total_count
	}
	filtered := []interface{}{"a"}

	result := rewrapSearchResponse(original, filtered)

	assert.Equal(t, filtered, result, "map without total_count should return filteredItems unchanged")
}

// TestRewrapSearchResponse_ItemsKey verifies the standard search-items case where
// the original uses an "items" key and the filtered slice replaces it.
func TestRewrapSearchResponse_ItemsKey(t *testing.T) {
	original := map[string]interface{}{
		"total_count":        float64(3),
		"incomplete_results": true,
		"items":              []interface{}{"a", "b", "c"},
	}
	filtered := []interface{}{"a"}

	result := rewrapSearchResponse(original, filtered)

	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(1), m["total_count"])
	assert.False(t, m["incomplete_results"].(bool))
	assert.Equal(t, filtered, m["items"])
}

// ---------------------------------------------------------------------------
// unwrapSingleObject — missing branches
// ---------------------------------------------------------------------------

// TestUnwrapSingleObject_NonMapOriginal verifies that when originalData is not a
// map (e.g. a slice, string, or nil), filteredData is returned as-is.
func TestUnwrapSingleObject_NonMapOriginal(t *testing.T) {
	filtered := []interface{}{map[string]interface{}{"id": 1}}

	tests := []struct {
		name     string
		original interface{}
	}{
		{"nil", nil},
		{"slice", []interface{}{"x"}},
		{"string", "text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unwrapSingleObject(tt.original, filtered)
			assert.Equal(t, filtered, result)
		})
	}
}

// TestUnwrapSingleObject_LegacyWrappedTopLevelArray verifies the compatibility
// unwrap path for legacy singleton fallback output: [[original-array]] → [original-array].
func TestUnwrapSingleObject_LegacyWrappedTopLevelArray(t *testing.T) {
	original := []interface{}{
		map[string]interface{}{"id": float64(1), "body": "first"},
		map[string]interface{}{"id": float64(2), "body": "second"},
	}
	filtered := []interface{}{original}

	result := unwrapSingleObject(original, filtered)

	assert.Equal(t, original, result, "legacy wrapped array should be restored to top-level array")
}

// TestUnwrapSingleObject_ArrayNotLegacyWrapped verifies that arbitrary array
// responses are not unwrapped unless they exactly match the legacy wrapper shape.
func TestUnwrapSingleObject_ArrayNotLegacyWrapped(t *testing.T) {
	original := []interface{}{[]interface{}{float64(1), float64(2)}}
	filtered := []interface{}{[]interface{}{float64(1), float64(2)}}

	result := unwrapSingleObject(original, filtered)

	assert.Equal(t, filtered, result, "non-legacy array responses must be left unchanged")
}

// TestUnwrapSingleObject_SearchEnvelope verifies that a map containing
// "total_count" (search envelope) is NOT unwrapped — filteredData is returned as-is.
func TestUnwrapSingleObject_SearchEnvelope(t *testing.T) {
	original := map[string]interface{}{
		"total_count": float64(1),
		"items":       []interface{}{map[string]interface{}{"id": 1}},
	}
	filtered := []interface{}{map[string]interface{}{"id": 1}}

	result := unwrapSingleObject(original, filtered)

	assert.Equal(t, filtered, result, "search envelope should not be unwrapped")
}

// TestUnwrapSingleObject_GraphQLEnvelope verifies that a map containing a "data"
// key (GraphQL response) is NOT unwrapped — filteredData is returned as-is.
func TestUnwrapSingleObject_GraphQLEnvelope(t *testing.T) {
	original := map[string]interface{}{
		"data": map[string]interface{}{"repository": map[string]interface{}{}},
	}
	filtered := []interface{}{map[string]interface{}{"id": 1}}

	result := unwrapSingleObject(original, filtered)

	assert.Equal(t, filtered, result, "GraphQL envelope should not be unwrapped")
}

// TestUnwrapSingleObject_SingleElementUnwrapped verifies the happy path: when
// filteredData is a single-element []interface{}, the element is returned directly.
func TestUnwrapSingleObject_SingleElementUnwrapped(t *testing.T) {
	inner := map[string]interface{}{"id": float64(42), "name": "file.txt"}
	original := map[string]interface{}{"id": float64(42), "name": "file.txt"}
	filtered := []interface{}{inner}

	result := unwrapSingleObject(original, filtered)

	assert.Equal(t, inner, result, "single-element array should be unwrapped")
}

// TestUnwrapSingleObject_MultiElementNotUnwrapped verifies that a multi-element
// filtered array is returned as-is (no unwrapping).
func TestUnwrapSingleObject_MultiElementNotUnwrapped(t *testing.T) {
	original := map[string]interface{}{"id": float64(1)}
	filtered := []interface{}{
		map[string]interface{}{"id": float64(1)},
		map[string]interface{}{"id": float64(2)},
	}

	result := unwrapSingleObject(original, filtered)

	assert.Equal(t, filtered, result, "multi-element array should not be unwrapped")
}

// TestUnwrapSingleObject_EmptyArrayNotUnwrapped verifies that an empty slice is
// returned as-is.
func TestUnwrapSingleObject_EmptyArrayNotUnwrapped(t *testing.T) {
	original := map[string]interface{}{"id": float64(1)}
	filtered := []interface{}{}

	result := unwrapSingleObject(original, filtered)

	assert.Equal(t, filtered, result, "empty array should not be unwrapped")
}

// TestUnwrapSingleObject_NonSliceFilteredData verifies that when filteredData is
// not a slice (e.g. a string or nil), it is returned unchanged.
func TestUnwrapSingleObject_NonSliceFilteredData(t *testing.T) {
	original := map[string]interface{}{"id": float64(1)}

	assert.Equal(t, "hello", unwrapSingleObject(original, "hello"))
	assert.Nil(t, unwrapSingleObject(original, nil))
}

// ---------------------------------------------------------------------------
// rebuildGraphQLResponse — missing branches
// ---------------------------------------------------------------------------

// TestRebuildGraphQLResponse_NonMapOriginal verifies that when originalData is not
// a map (e.g. a string or nil), the function returns {"data": nil}.
func TestRebuildGraphQLResponse_NonMapOriginal(t *testing.T) {
	filtered := &difc.FilteredCollectionLabeledData{
		Accessible: []difc.LabeledItem{{Data: "item"}},
	}

	tests := []struct {
		name     string
		original interface{}
	}{
		{"nil", nil},
		{"string", "not a map"},
		{"number", float64(42)},
		{"slice", []interface{}{"x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rebuildGraphQLResponse(tt.original, filtered)
			m, ok := result.(map[string]interface{})
			require.True(t, ok, "result should be a map")
			assert.Nil(t, m["data"], "data should be nil for non-map original")
		})
	}
}

// TestRebuildGraphQLResponse_AllItemsFiltered verifies that when accessible count
// is zero (all items were filtered out), the function returns {"data": nil}.
func TestRebuildGraphQLResponse_AllItemsFiltered(t *testing.T) {
	original := map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"issues": map[string]interface{}{
					"nodes":      []interface{}{"issue1", "issue2"},
					"totalCount": float64(2),
				},
			},
		},
	}
	filtered := &difc.FilteredCollectionLabeledData{
		Accessible: []difc.LabeledItem{}, // no accessible items
		Filtered:   []difc.FilteredItemDetail{{Item: difc.LabeledItem{Data: "issue1"}}, {Item: difc.LabeledItem{Data: "issue2"}}},
		TotalCount: 2,
	}

	result := rebuildGraphQLResponse(original, filtered)

	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Nil(t, m["data"], "data should be nil when all items are filtered")
}

// TestRebuildGraphQLResponse_NoNodesEdgesInData verifies that when accessible
// items exist but the cloned "data" object has no nodes/edges array to replace,
// the function returns {"data": nil} to prevent leaking non-collection fields.
func TestRebuildGraphQLResponse_NoNodesEdgesInData(t *testing.T) {
	// The "data" field contains a scalar response (viewer { login }) — no nodes/edges.
	original := map[string]interface{}{
		"data": map[string]interface{}{
			"viewer": map[string]interface{}{
				"login": "octocat",
			},
		},
	}
	filtered := &difc.FilteredCollectionLabeledData{
		Accessible: []difc.LabeledItem{{Data: "octocat"}},
	}

	result := rebuildGraphQLResponse(original, filtered)

	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Nil(t, m["data"], "data should be nil when no nodes/edges array is found")
}

// TestRebuildGraphQLResponse_SuccessfulRebuild verifies the happy path: accessible
// items replace the nodes array and the response envelope is preserved.
func TestRebuildGraphQLResponse_SuccessfulRebuild(t *testing.T) {
	item1 := map[string]interface{}{"id": float64(1), "title": "Bug fix"}
	item2 := map[string]interface{}{"id": float64(2), "title": "Feature"}
	item3 := map[string]interface{}{"id": float64(3), "title": "Docs"}
	original := map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"issues": map[string]interface{}{
					"nodes":      []interface{}{item1, item2, item3},
					"totalCount": float64(3),
				},
			},
		},
	}
	// Only item1 is accessible.
	filtered := &difc.FilteredCollectionLabeledData{
		Accessible: []difc.LabeledItem{{Data: item1}},
		Filtered:   []difc.FilteredItemDetail{{Item: difc.LabeledItem{Data: item2}}, {Item: difc.LabeledItem{Data: item3}}},
		TotalCount: 3,
	}

	result := rebuildGraphQLResponse(original, filtered)

	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	data, ok := m["data"].(map[string]interface{})
	require.True(t, ok, "data should be a map")
	repo, ok := data["repository"].(map[string]interface{})
	require.True(t, ok, "repository should be a map")
	issues, ok := repo["issues"].(map[string]interface{})
	require.True(t, ok, "issues should be a map")
	nodes, ok := issues["nodes"].([]interface{})
	require.True(t, ok, "nodes should be a slice")
	assert.Len(t, nodes, 1, "only accessible items should be in nodes")
	assert.Equal(t, float64(1), issues["totalCount"], "totalCount should reflect filtered count")

	// The original must be unchanged (deep-clone was used).
	origNodes := original["data"].(map[string]interface{})["repository"].(map[string]interface{})["issues"].(map[string]interface{})["nodes"].([]interface{})
	assert.Len(t, origNodes, 3, "original nodes should be unchanged")
}

// TestRebuildGraphQLResponse_EdgesReplaced verifies that edges arrays are also
// replaced when the GraphQL response uses the Relay "edges" pattern.
func TestRebuildGraphQLResponse_EdgesReplaced(t *testing.T) {
	edge1 := map[string]interface{}{"node": map[string]interface{}{"id": float64(1)}}
	edge2 := map[string]interface{}{"node": map[string]interface{}{"id": float64(2)}}
	original := map[string]interface{}{
		"data": map[string]interface{}{
			"search": map[string]interface{}{
				"edges":      []interface{}{edge1, edge2},
				"totalCount": float64(2),
			},
		},
	}
	filtered := &difc.FilteredCollectionLabeledData{
		Accessible: []difc.LabeledItem{{Data: edge1}},
		Filtered:   []difc.FilteredItemDetail{{Item: difc.LabeledItem{Data: edge2}}},
		TotalCount: 2,
	}

	result := rebuildGraphQLResponse(original, filtered)

	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	data := m["data"].(map[string]interface{})
	search := data["search"].(map[string]interface{})
	edges, ok := search["edges"].([]interface{})
	require.True(t, ok)
	assert.Len(t, edges, 1)
	assert.Equal(t, float64(1), search["totalCount"])
}

// ---------------------------------------------------------------------------
// deepCloneJSON — map branch
// ---------------------------------------------------------------------------

// TestDeepCloneJSON_Map verifies that deepCloneJSON deep-copies a map so that
// mutations to the original map do not affect the clone.
func TestDeepCloneJSON_Map(t *testing.T) {
	original := map[string]interface{}{
		"id": float64(1),
		"nested": map[string]interface{}{
			"value": "original",
		},
		"tags": []interface{}{"go", "test"},
	}

	cloned := util.DeepCloneJSON(original)

	clonedMap, ok := cloned.(map[string]interface{})
	require.True(t, ok, "cloned value should be a map")

	// Mutate the nested map in the original.
	original["nested"].(map[string]interface{})["value"] = "mutated"
	original["tags"].([]interface{})[0] = "mutated-tag"

	// Clone should be unaffected.
	assert.Equal(t, "original", clonedMap["nested"].(map[string]interface{})["value"],
		"clone nested value should be unchanged after mutating original")
	assert.Equal(t, "go", clonedMap["tags"].([]interface{})[0],
		"clone tag should be unchanged after mutating original")
}

// TestDeepCloneJSON_NestedMapAndSlice verifies that deeply-nested structures
// are fully cloned (not just the top level).
func TestDeepCloneJSON_NestedMapAndSlice(t *testing.T) {
	original := map[string]interface{}{
		"level1": map[string]interface{}{
			"level2": []interface{}{
				map[string]interface{}{"x": float64(1)},
			},
		},
	}

	cloned := util.DeepCloneJSON(original)

	// Mutate deeply nested value.
	original["level1"].(map[string]interface{})["level2"].([]interface{})[0].(map[string]interface{})["x"] = float64(99)

	clonedVal := cloned.(map[string]interface{})["level1"].(map[string]interface{})["level2"].([]interface{})[0].(map[string]interface{})["x"]
	assert.Equal(t, float64(1), clonedVal, "deep clone should be independent of original mutations")
}
