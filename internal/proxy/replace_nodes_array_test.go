package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestReplaceNodesArray provides direct, comprehensive coverage of replaceNodesArray.
//
// The existing TestRebuildGraphQLResponse exercises this helper indirectly but only
// covers the "nodes" key on a singly-nested structure.  These tests add direct
// coverage for every branch:
//
//   - non-map input (early return false)
//   - empty map (no matching key → false)
//   - "nodes" key present, with and without an adjacent "totalCount"
//   - "edges" key present, with and without an adjacent "totalCount"
//   - "nodes" takes priority over "edges" when both are present in the same object
//   - recursion: nodes found one level deeper
//   - recursion: nodes found multiple levels deep
//   - recursion: non-map children are skipped
//   - recursion returns false when no match exists anywhere in the tree
//   - replacement with an empty items slice
//   - replacement with a nil items slice
func TestReplaceNodesArray(t *testing.T) {
	items2 := []interface{}{"x", "y"}

	t.Run("non-map input returns false", func(t *testing.T) {
		assert.False(t, replaceNodesArray("not a map", items2))
		assert.False(t, replaceNodesArray(42, items2))
		assert.False(t, replaceNodesArray([]interface{}{"a"}, items2))
		assert.False(t, replaceNodesArray(nil, items2))
	})

	t.Run("empty map returns false", func(t *testing.T) {
		obj := map[string]interface{}{}
		assert.False(t, replaceNodesArray(obj, items2))
	})

	t.Run("nodes key replaced, returns true", func(t *testing.T) {
		obj := map[string]interface{}{
			"nodes": []interface{}{"a", "b", "c"},
		}
		result := replaceNodesArray(obj, items2)
		assert.True(t, result)
		assert.Equal(t, items2, obj["nodes"])
	})

	t.Run("nodes replaced and totalCount updated", func(t *testing.T) {
		obj := map[string]interface{}{
			"nodes":      []interface{}{"a", "b", "c"},
			"totalCount": float64(3),
		}
		result := replaceNodesArray(obj, items2)
		assert.True(t, result)
		assert.Equal(t, items2, obj["nodes"])
		assert.Equal(t, float64(2), obj["totalCount"])
	})

	t.Run("nodes replaced but no totalCount key — field not created", func(t *testing.T) {
		obj := map[string]interface{}{
			"nodes": []interface{}{"a", "b", "c"},
		}
		result := replaceNodesArray(obj, items2)
		assert.True(t, result)
		assert.Equal(t, items2, obj["nodes"])
		_, hasTotalCount := obj["totalCount"]
		assert.False(t, hasTotalCount, "totalCount should not be created when absent")
	})

	t.Run("edges key replaced, returns true", func(t *testing.T) {
		obj := map[string]interface{}{
			"edges": []interface{}{"e1", "e2", "e3"},
		}
		result := replaceNodesArray(obj, items2)
		assert.True(t, result)
		assert.Equal(t, items2, obj["edges"])
	})

	t.Run("edges replaced and totalCount updated", func(t *testing.T) {
		obj := map[string]interface{}{
			"edges":      []interface{}{"e1", "e2"},
			"totalCount": float64(2),
		}
		result := replaceNodesArray(obj, items2)
		assert.True(t, result)
		assert.Equal(t, items2, obj["edges"])
		assert.Equal(t, float64(2), obj["totalCount"])
	})

	t.Run("edges replaced but no totalCount key — field not created", func(t *testing.T) {
		obj := map[string]interface{}{
			"edges": []interface{}{"e1", "e2"},
		}
		result := replaceNodesArray(obj, items2)
		assert.True(t, result)
		assert.Equal(t, items2, obj["edges"])
		_, hasTotalCount := obj["totalCount"]
		assert.False(t, hasTotalCount, "totalCount should not be created when absent")
	})

	t.Run("nodes takes priority over edges in same object", func(t *testing.T) {
		// The loop checks "nodes" before "edges", so "nodes" wins.
		obj := map[string]interface{}{
			"nodes": []interface{}{"n1"},
			"edges": []interface{}{"e1"},
		}
		result := replaceNodesArray(obj, items2)
		assert.True(t, result)
		assert.Equal(t, items2, obj["nodes"])
		// edges must be unchanged
		assert.Equal(t, []interface{}{"e1"}, obj["edges"])
	})

	t.Run("no matching key at top level returns false", func(t *testing.T) {
		obj := map[string]interface{}{
			"id":    "abc",
			"title": "issue title",
		}
		assert.False(t, replaceNodesArray(obj, items2))
	})

	t.Run("recursion: nodes found one level deep", func(t *testing.T) {
		inner := map[string]interface{}{
			"nodes":      []interface{}{"a", "b"},
			"totalCount": float64(2),
		}
		outer := map[string]interface{}{
			"issues": inner,
		}
		result := replaceNodesArray(outer, items2)
		assert.True(t, result)
		assert.Equal(t, items2, inner["nodes"])
		assert.Equal(t, float64(2), inner["totalCount"])
	})

	t.Run("recursion: nodes found two levels deep", func(t *testing.T) {
		deepest := map[string]interface{}{
			"nodes": []interface{}{"a", "b", "c"},
		}
		middle := map[string]interface{}{
			"issues": deepest,
		}
		top := map[string]interface{}{
			"repository": middle,
		}
		result := replaceNodesArray(top, items2)
		assert.True(t, result)
		assert.Equal(t, items2, deepest["nodes"])
	})

	t.Run("recursion: non-map children are skipped", func(t *testing.T) {
		// The top-level object has string/numeric values plus a child map with nodes.
		child := map[string]interface{}{
			"nodes": []interface{}{"a"},
		}
		obj := map[string]interface{}{
			"name":  "octocat",
			"count": float64(5),
			"list":  []interface{}{"x", "y"},
			"child": child,
		}
		result := replaceNodesArray(obj, items2)
		assert.True(t, result)
		assert.Equal(t, items2, child["nodes"])
	})

	t.Run("recursion: returns false when no match exists at any depth", func(t *testing.T) {
		inner := map[string]interface{}{
			"login": "octocat",
		}
		outer := map[string]interface{}{
			"viewer": inner,
		}
		assert.False(t, replaceNodesArray(outer, items2))
	})

	t.Run("replacement with empty items slice sets nodes to empty slice", func(t *testing.T) {
		empty := []interface{}{}
		obj := map[string]interface{}{
			"nodes":      []interface{}{"a", "b"},
			"totalCount": float64(2),
		}
		result := replaceNodesArray(obj, empty)
		assert.True(t, result)
		assert.Equal(t, empty, obj["nodes"])
		assert.Equal(t, float64(0), obj["totalCount"])
	})

	t.Run("replacement with nil items sets nodes to nil", func(t *testing.T) {
		obj := map[string]interface{}{
			"nodes": []interface{}{"a"},
		}
		result := replaceNodesArray(obj, nil)
		assert.True(t, result)
		assert.Nil(t, obj["nodes"])
	})

	t.Run("edges found when sibling keys exist before edges in iteration", func(t *testing.T) {
		// The object only has "edges" (no "nodes"), plus unrelated keys.
		obj := map[string]interface{}{
			"pageInfo": map[string]interface{}{"hasNextPage": true},
			"edges":    []interface{}{"e1", "e2", "e3"},
		}
		result := replaceNodesArray(obj, items2)
		assert.True(t, result)
		assert.Equal(t, items2, obj["edges"])
	})

	t.Run("first match stops recursion (early return)", func(t *testing.T) {
		// Two sibling children both have nodes; only the first one encountered
		// should be replaced (function returns true on first match).
		child1 := map[string]interface{}{"nodes": []interface{}{"a"}}
		child2 := map[string]interface{}{"nodes": []interface{}{"b"}}
		obj := map[string]interface{}{
			"alpha": child1,
			"beta":  child2,
		}
		result := replaceNodesArray(obj, items2)
		assert.True(t, result)
		// Exactly one of the children was updated; the total count of replaced
		// nodes across both children must equal len(items2).
		child1Nodes := child1["nodes"].([]interface{})
		child2Nodes := child2["nodes"].([]interface{})
		replaced := 0
		if len(child1Nodes) == len(items2) {
			replaced++
		}
		if len(child2Nodes) == len(items2) {
			replaced++
		}
		assert.Equal(t, 1, replaced, "exactly one child should have been updated")
	})
}
