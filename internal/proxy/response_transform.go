package proxy

import (
	"reflect"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/util"
)

var logTransform = logger.ForFile()

// rewrapSearchResponse re-wraps filtered items into the original search response
// envelope. GitHub search endpoints return {"total_count": N, "items": [...]};
// ToResult() returns a bare array, so we rebuild the wrapper.
func rewrapSearchResponse(originalData interface{}, filteredItems interface{}) interface{} {
	original, ok := originalData.(map[string]interface{})
	if !ok {
		logTransform.Print("rewrapSearchResponse: originalData is not a map, returning filteredItems unchanged")
		return filteredItems
	}
	// Detect search response wrapper (has total_count + items/repositories)
	if _, hasTotalCount := original["total_count"]; !hasTotalCount {
		logTransform.Print("rewrapSearchResponse: no total_count field, not a search response, returning filteredItems unchanged")
		return filteredItems
	}
	items, ok := filteredItems.([]interface{})
	if !ok {
		return filteredItems
	}
	logTransform.Printf("rewrapSearchResponse: rebuilding search wrapper with %d filtered items", len(items))
	// Rebuild the search wrapper with filtered items
	result := make(map[string]interface{})
	for k, v := range original {
		result[k] = v
	}
	// Replace items key — search can use "items", "repositories", etc.
	for _, key := range []string{"items", "repositories"} {
		if _, ok := original[key]; ok {
			result[key] = items
			break
		}
	}
	result["total_count"] = float64(len(items))
	result["incomplete_results"] = false
	return result
}

// unwrapSingleObject preserves the original response shape for single-object endpoints.
// When the guard wraps a single object in a collection, ToResult() returns [obj].
// This unwraps it back to obj when the original response was a single object
// (e.g., get_file_contents, get_commit, issue_read).
func unwrapSingleObject(originalData interface{}, filteredData interface{}) interface{} {
	// Guard compatibility: older singleton fallback could wrap a top-level array
	// as a single collection item, producing [[...]]. If the wrapped value is
	// exactly the original array payload, restore the original top-level shape.
	if originalArray, isArray := originalData.([]interface{}); isArray {
		if arr, ok := filteredData.([]interface{}); ok && len(arr) == 1 {
			if wrapped, ok := arr[0].([]interface{}); ok &&
				len(wrapped) == len(originalArray) {
				if len(wrapped) == 0 {
					logTransform.Print("unwrapSingleObject: restoring wrapped empty array to original top-level shape")
					return wrapped
				}
				if reflect.DeepEqual(wrapped, originalArray) {
					logTransform.Printf("unwrapSingleObject: restoring wrapped array to original top-level shape, len=%d", len(wrapped))
					return wrapped
				}
			}
		}
		return filteredData
	}

	original, isMap := originalData.(map[string]interface{})
	if !isMap {
		return filteredData
	}
	// Don't unwrap search envelopes (handled by rewrapSearchResponse)
	if _, hasTotalCount := original["total_count"]; hasTotalCount {
		logTransform.Print("unwrapSingleObject: skipping search envelope (has total_count field)")
		return filteredData
	}
	// Don't unwrap GraphQL responses (handled separately)
	if _, hasData := original["data"]; hasData {
		logTransform.Print("unwrapSingleObject: skipping GraphQL response envelope (has data field)")
		return filteredData
	}
	// If filtered result is a single-element array, unwrap to match original shape
	if arr, ok := filteredData.([]interface{}); ok && len(arr) == 1 {
		logTransform.Print("unwrapSingleObject: unwrapping single-element filtered array to match original object shape")
		return arr[0]
	}
	return filteredData
}

// rebuildGraphQLResponse reconstructs a GraphQL response with only accessible
// items, preserving the {"data": {...}} envelope that clients expect.
func rebuildGraphQLResponse(originalData interface{}, filtered *difc.FilteredCollectionLabeledData) interface{} {
	original, ok := originalData.(map[string]interface{})
	if !ok {
		logTransform.Print("rebuildGraphQLResponse: originalData is not a map, returning null data")
		return map[string]interface{}{"data": nil}
	}
	if _, ok := original["data"]; !ok {
		logTransform.Print("rebuildGraphQLResponse: no data field in original response, returning null data")
		return map[string]interface{}{"data": nil}
	}

	// If all items were filtered out, return {"data": null} to avoid leaking
	// the original response through non-collection fields (e.g., viewer).
	if filtered.GetAccessibleCount() == 0 {
		logTransform.Print("rebuildGraphQLResponse: all items filtered out, returning null data")
		return map[string]interface{}{"data": nil}
	}

	logTransform.Printf("rebuildGraphQLResponse: rebuilding with %d accessible items", filtered.GetAccessibleCount())

	// Deep-clone the original data structure
	cloned := util.DeepCloneJSON(original)

	// Build accessible items set
	accessibleItems := make([]interface{}, 0, len(filtered.Accessible))
	for _, item := range filtered.Accessible {
		accessibleItems = append(accessibleItems, item.Data)
	}

	// Walk the cloned structure and replace nodes/edges arrays.
	// If no nodes/edges found, return {"data": null} to prevent leaking
	// non-collection data (e.g., viewer { login }).
	if clonedMap, ok := cloned.(map[string]interface{}); ok {
		if clonedData, ok := clonedMap["data"]; ok {
			if !replaceNodesArray(clonedData, accessibleItems) {
				logTransform.Print("rebuildGraphQLResponse: no nodes/edges array found in cloned data, returning null data")
				return map[string]interface{}{"data": nil}
			}
		}
	}

	return cloned
}

// replaceNodesArray walks a JSON tree and replaces the first "nodes" or "edges"
// array with the given items, and updates any adjacent "totalCount".
func replaceNodesArray(v interface{}, items []interface{}) bool {
	obj, ok := v.(map[string]interface{})
	if !ok {
		return false
	}
	for _, key := range []string{"nodes", "edges"} {
		if _, ok := obj[key]; ok {
			logTransform.Printf("replaceNodesArray: replacing %q array with %d accessible item(s)", key, len(items))
			obj[key] = items
			if _, ok := obj["totalCount"]; ok {
				obj["totalCount"] = float64(len(items))
			}
			return true
		}
	}
	// Recurse into child objects
	for _, child := range obj {
		if replaceNodesArray(child, items) {
			return true
		}
	}
	return false
}
