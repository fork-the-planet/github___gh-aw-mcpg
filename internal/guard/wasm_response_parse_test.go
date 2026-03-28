package guard

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/difc"
)

// sortTags sorts a slice of difc.Tag for stable comparison in assertions.
func sortTags(tags []difc.Tag) []difc.Tag {
	sorted := make([]difc.Tag, len(tags))
	copy(sorted, tags)
	sort.Slice(sorted, func(i, j int) bool {
		return string(sorted[i]) < string(sorted[j])
	})
	return sorted
}

// ─── parseResourceResponse ────────────────────────────────────────────────────

func TestParseResourceResponse(t *testing.T) {
	tests := []struct {
		name          string
		input         map[string]interface{}
		wantErr       bool
		errContains   string
		wantDesc      string
		wantSecrecy   []difc.Tag
		wantIntegrity []difc.Tag
		wantOperation difc.OperationType
	}{
		// ── Happy paths ────────────────────────────────────────────────────────

		{
			name: "full resource with description, secrecy, integrity, and read operation",
			input: map[string]interface{}{
				"resource": map[string]interface{}{
					"description": "GitHub repo data",
					"secrecy":     []interface{}{"private", "internal"},
					"integrity":   []interface{}{"approved"},
				},
				"operation": "read",
			},
			wantDesc:      "GitHub repo data",
			wantSecrecy:   []difc.Tag{"internal", "private"},
			wantIntegrity: []difc.Tag{"approved"},
			wantOperation: difc.OperationRead,
		},
		{
			name: "write operation",
			input: map[string]interface{}{
				"resource":  map[string]interface{}{},
				"operation": "write",
			},
			wantOperation: difc.OperationWrite,
		},
		{
			name: "read-write operation",
			input: map[string]interface{}{
				"resource":  map[string]interface{}{},
				"operation": "read-write",
			},
			wantOperation: difc.OperationReadWrite,
		},
		{
			name: "missing operation defaults to write (most restrictive)",
			input: map[string]interface{}{
				"resource": map[string]interface{}{},
			},
			wantOperation: difc.OperationWrite,
		},
		{
			name: "unknown operation string defaults to write",
			input: map[string]interface{}{
				"resource":  map[string]interface{}{},
				"operation": "unknown-op",
			},
			wantOperation: difc.OperationWrite,
		},
		{
			name: "empty secrecy array produces empty secrecy label",
			input: map[string]interface{}{
				"resource": map[string]interface{}{
					"secrecy": []interface{}{},
				},
				"operation": "read",
			},
			wantSecrecy:   []difc.Tag{},
			wantOperation: difc.OperationRead,
		},
		{
			name: "empty integrity array produces empty integrity label",
			input: map[string]interface{}{
				"resource": map[string]interface{}{
					"integrity": []interface{}{},
				},
				"operation": "read",
			},
			wantIntegrity: []difc.Tag{},
			wantOperation: difc.OperationRead,
		},
		{
			name: "missing secrecy key produces default empty secrecy label",
			input: map[string]interface{}{
				"resource":  map[string]interface{}{},
				"operation": "read",
			},
			wantSecrecy:   []difc.Tag{},
			wantOperation: difc.OperationRead,
		},
		{
			name: "missing integrity key produces default empty integrity label",
			input: map[string]interface{}{
				"resource":  map[string]interface{}{},
				"operation": "read",
			},
			wantIntegrity: []difc.Tag{},
			wantOperation: difc.OperationRead,
		},
		{
			name: "multiple secrecy and integrity tags",
			input: map[string]interface{}{
				"resource": map[string]interface{}{
					"secrecy":   []interface{}{"private", "internal", "confidential"},
					"integrity": []interface{}{"approved", "merged", "verified"},
				},
			},
			wantSecrecy:   []difc.Tag{"confidential", "internal", "private"},
			wantIntegrity: []difc.Tag{"approved", "merged", "verified"},
			wantOperation: difc.OperationWrite,
		},
		{
			name: "description field is optional",
			input: map[string]interface{}{
				"resource": map[string]interface{}{
					"secrecy": []interface{}{"private"},
				},
			},
			wantDesc:      "",
			wantSecrecy:   []difc.Tag{"private"},
			wantOperation: difc.OperationWrite,
		},
		{
			name: "non-string tags in secrecy array are skipped",
			input: map[string]interface{}{
				"resource": map[string]interface{}{
					"secrecy": []interface{}{"valid-tag", 42, true, nil, "another-tag"},
				},
			},
			wantSecrecy:   []difc.Tag{"another-tag", "valid-tag"},
			wantOperation: difc.OperationWrite,
		},
		{
			name: "non-string tags in integrity array are skipped",
			input: map[string]interface{}{
				"resource": map[string]interface{}{
					"integrity": []interface{}{"good-tag", 3.14, false, nil, "also-good"},
				},
			},
			wantIntegrity: []difc.Tag{"also-good", "good-tag"},
			wantOperation: difc.OperationWrite,
		},

		// ── Error cases ────────────────────────────────────────────────────────

		{
			name: "missing resource key returns error",
			input: map[string]interface{}{
				"operation": "read",
			},
			wantErr:     true,
			errContains: "invalid resource format in guard response",
		},
		{
			name: "resource value is not a map (string) returns error",
			input: map[string]interface{}{
				"resource": "not-a-map",
			},
			wantErr:     true,
			errContains: "invalid resource format in guard response",
		},
		{
			name: "resource value is not a map (array) returns error",
			input: map[string]interface{}{
				"resource": []interface{}{"a", "b"},
			},
			wantErr:     true,
			errContains: "invalid resource format in guard response",
		},
		{
			name: "resource value is not a map (number) returns error",
			input: map[string]interface{}{
				"resource": 42,
			},
			wantErr:     true,
			errContains: "invalid resource format in guard response",
		},
		{
			name: "resource value is nil returns error",
			input: map[string]interface{}{
				"resource": nil,
			},
			wantErr:     true,
			errContains: "invalid resource format in guard response",
		},
		{
			name:        "empty input map (no resource key) returns error",
			input:       map[string]interface{}{},
			wantErr:     true,
			errContains: "invalid resource format in guard response",
		},

		// ── Non-string secrecy/integrity values ────────────────────────────────

		{
			name: "secrecy value is not an array (string) defaults to empty label",
			input: map[string]interface{}{
				"resource": map[string]interface{}{
					"secrecy": "not-an-array",
				},
			},
			wantSecrecy:   []difc.Tag{},
			wantOperation: difc.OperationWrite,
		},
		{
			name: "integrity value is not an array (map) defaults to empty label",
			input: map[string]interface{}{
				"resource": map[string]interface{}{
					"integrity": map[string]interface{}{"tag": "value"},
				},
			},
			wantIntegrity: []difc.Tag{},
			wantOperation: difc.OperationWrite,
		},
		{
			name: "description field is not a string - ignored",
			input: map[string]interface{}{
				"resource": map[string]interface{}{
					"description": 999,
				},
			},
			wantDesc:      "",
			wantOperation: difc.OperationWrite,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, op, err := parseResourceResponse(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, resource)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resource)

			assert.Equal(t, tt.wantDesc, resource.Description)
			assert.Equal(t, tt.wantOperation, op)

			if tt.wantSecrecy != nil {
				got := sortTags(resource.Secrecy.Label.GetTags())
				assert.Equal(t, tt.wantSecrecy, got, "secrecy tags mismatch")
			}

			if tt.wantIntegrity != nil {
				got := sortTags(resource.Integrity.Label.GetTags())
				assert.Equal(t, tt.wantIntegrity, got, "integrity tags mismatch")
			}
		})
	}
}

func TestParseResourceResponse_OperationCoverage(t *testing.T) {
	operationCases := []struct {
		opStr    string
		wantOp   difc.OperationType
		wantName string
	}{
		{"read", difc.OperationRead, "read"},
		{"write", difc.OperationWrite, "write"},
		{"read-write", difc.OperationReadWrite, "read-write"},
		{"", difc.OperationWrite, "empty string defaults to write"},
		{"READ", difc.OperationWrite, "case-sensitive: uppercase READ defaults to write"},
		{"WRITE", difc.OperationWrite, "case-sensitive: uppercase WRITE defaults to write"},
	}

	for _, tc := range operationCases {
		t.Run(tc.wantName, func(t *testing.T) {
			input := map[string]interface{}{
				"resource":  map[string]interface{}{},
				"operation": tc.opStr,
			}
			_, op, err := parseResourceResponse(input)
			require.NoError(t, err)
			assert.Equal(t, tc.wantOp, op)
		})
	}
}

// ─── parseCollectionLabeledData ───────────────────────────────────────────────

func TestParseCollectionLabeledData(t *testing.T) {
	tests := []struct {
		name          string
		items         []interface{}
		wantItemCount int
		wantErr       bool
		checkItems    func(t *testing.T, collection *difc.CollectionLabeledData)
	}{
		// ── Empty / nil inputs ─────────────────────────────────────────────────

		{
			name:          "empty items slice returns empty collection",
			items:         []interface{}{},
			wantItemCount: 0,
		},
		{
			name:          "nil items slice returns empty collection",
			items:         nil,
			wantItemCount: 0,
		},

		// ── Non-map items silently skipped ────────────────────────────────────

		{
			name:          "string items are skipped",
			items:         []interface{}{"a", "b", "c"},
			wantItemCount: 0,
		},
		{
			name:          "number items are skipped",
			items:         []interface{}{1, 2.5, -3},
			wantItemCount: 0,
		},
		{
			name:          "boolean items are skipped",
			items:         []interface{}{true, false},
			wantItemCount: 0,
		},
		{
			name:          "nil items are skipped",
			items:         []interface{}{nil, nil},
			wantItemCount: 0,
		},
		{
			name:          "array items are skipped",
			items:         []interface{}{[]interface{}{"nested"}},
			wantItemCount: 0,
		},
		{
			name: "mixed non-map and map items: only maps are kept",
			items: []interface{}{
				"skip-me",
				map[string]interface{}{"data": "keep-me"},
				42,
				map[string]interface{}{"data": "also-keep-me"},
				nil,
			},
			wantItemCount: 2,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				assert.Equal(t, "keep-me", c.Items[0].Data)
				assert.Equal(t, "also-keep-me", c.Items[1].Data)
			},
		},

		// ── Items with no labels key ───────────────────────────────────────────

		{
			name: "item without labels key has nil Labels",
			items: []interface{}{
				map[string]interface{}{"data": "value"},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				assert.Equal(t, "value", c.Items[0].Data)
				assert.Nil(t, c.Items[0].Labels)
			},
		},
		{
			name: "item with non-map labels key has nil Labels",
			items: []interface{}{
				map[string]interface{}{"data": "x", "labels": "not-a-map"},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				assert.Nil(t, c.Items[0].Labels)
			},
		},

		// ── Items with labels ──────────────────────────────────────────────────

		{
			name: "item with empty labels map produces default labels",
			items: []interface{}{
				map[string]interface{}{
					"data":   "payload",
					"labels": map[string]interface{}{},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				item := c.Items[0]
				require.NotNil(t, item.Labels)
				assert.Empty(t, item.Labels.Secrecy.Label.GetTags())
				assert.Empty(t, item.Labels.Integrity.Label.GetTags())
				assert.Equal(t, "", item.Labels.Description)
			},
		},
		{
			name: "item with full labels: description, secrecy, and integrity",
			items: []interface{}{
				map[string]interface{}{
					"data": map[string]interface{}{"id": 1, "name": "repo"},
					"labels": map[string]interface{}{
						"description": "a private repository",
						"secrecy":     []interface{}{"private", "internal"},
						"integrity":   []interface{}{"approved"},
					},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				item := c.Items[0]
				require.NotNil(t, item.Labels)
				assert.Equal(t, "a private repository", item.Labels.Description)
				assert.Equal(t, []difc.Tag{"internal", "private"}, sortTags(item.Labels.Secrecy.Label.GetTags()))
				assert.Equal(t, []difc.Tag{"approved"}, item.Labels.Integrity.Label.GetTags())
			},
		},
		{
			name: "item with description only",
			items: []interface{}{
				map[string]interface{}{
					"data": "payload",
					"labels": map[string]interface{}{
						"description": "my resource",
					},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				item := c.Items[0]
				require.NotNil(t, item.Labels)
				assert.Equal(t, "my resource", item.Labels.Description)
				assert.Empty(t, item.Labels.Secrecy.Label.GetTags())
				assert.Empty(t, item.Labels.Integrity.Label.GetTags())
			},
		},
		{
			name: "item with secrecy only, integrity defaults to empty",
			items: []interface{}{
				map[string]interface{}{
					"data": "payload",
					"labels": map[string]interface{}{
						"secrecy": []interface{}{"secret"},
					},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				item := c.Items[0]
				require.NotNil(t, item.Labels)
				assert.Equal(t, []difc.Tag{"secret"}, item.Labels.Secrecy.Label.GetTags())
				assert.Empty(t, item.Labels.Integrity.Label.GetTags())
			},
		},
		{
			name: "item with integrity only, secrecy defaults to empty",
			items: []interface{}{
				map[string]interface{}{
					"data": "payload",
					"labels": map[string]interface{}{
						"integrity": []interface{}{"merged"},
					},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				item := c.Items[0]
				require.NotNil(t, item.Labels)
				assert.Empty(t, item.Labels.Secrecy.Label.GetTags())
				assert.Equal(t, []difc.Tag{"merged"}, item.Labels.Integrity.Label.GetTags())
			},
		},

		// ── Non-string tags are skipped ────────────────────────────────────────

		{
			name: "non-string secrecy tags are ignored",
			items: []interface{}{
				map[string]interface{}{
					"data": "x",
					"labels": map[string]interface{}{
						"secrecy": []interface{}{"valid", 123, true, nil, "also-valid"},
					},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				got := sortTags(c.Items[0].Labels.Secrecy.Label.GetTags())
				assert.Equal(t, []difc.Tag{"also-valid", "valid"}, got)
			},
		},
		{
			name: "non-string integrity tags are ignored",
			items: []interface{}{
				map[string]interface{}{
					"data": "x",
					"labels": map[string]interface{}{
						"integrity": []interface{}{42, "real-tag", 3.14},
					},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				got := c.Items[0].Labels.Integrity.Label.GetTags()
				assert.Equal(t, []difc.Tag{"real-tag"}, got)
			},
		},
		{
			name: "secrecy array with all non-string entries produces empty label",
			items: []interface{}{
				map[string]interface{}{
					"data": "x",
					"labels": map[string]interface{}{
						"secrecy": []interface{}{1, 2, 3},
					},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				assert.Empty(t, c.Items[0].Labels.Secrecy.Label.GetTags())
			},
		},

		// ── Non-array secrecy/integrity defaults to empty label ─────────────────

		{
			name: "secrecy is a string (not array) defaults to empty label",
			items: []interface{}{
				map[string]interface{}{
					"data": "x",
					"labels": map[string]interface{}{
						"secrecy": "private",
					},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				assert.Empty(t, c.Items[0].Labels.Secrecy.Label.GetTags())
			},
		},
		{
			name: "integrity is a map (not array) defaults to empty label",
			items: []interface{}{
				map[string]interface{}{
					"data": "x",
					"labels": map[string]interface{}{
						"integrity": map[string]interface{}{"tag": "val"},
					},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				assert.Empty(t, c.Items[0].Labels.Integrity.Label.GetTags())
			},
		},

		// ── Description field type safety ──────────────────────────────────────

		{
			name: "description is not a string - ignored",
			items: []interface{}{
				map[string]interface{}{
					"data": "x",
					"labels": map[string]interface{}{
						"description": 999,
					},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				assert.Equal(t, "", c.Items[0].Labels.Description)
			},
		},

		// ── Multiple items ─────────────────────────────────────────────────────

		{
			name: "multiple items with varied labels",
			items: []interface{}{
				map[string]interface{}{
					"data": "item-0-no-labels",
				},
				map[string]interface{}{
					"data": "item-1-with-labels",
					"labels": map[string]interface{}{
						"secrecy":   []interface{}{"private"},
						"integrity": []interface{}{"approved"},
					},
				},
				"not-a-map",
				map[string]interface{}{
					"data":   "item-2-empty-labels",
					"labels": map[string]interface{}{},
				},
			},
			wantItemCount: 3,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				// item 0: no labels
				assert.Equal(t, "item-0-no-labels", c.Items[0].Data)
				assert.Nil(t, c.Items[0].Labels)

				// item 1: full labels
				assert.Equal(t, "item-1-with-labels", c.Items[1].Data)
				require.NotNil(t, c.Items[1].Labels)
				assert.Equal(t, []difc.Tag{"private"}, c.Items[1].Labels.Secrecy.Label.GetTags())
				assert.Equal(t, []difc.Tag{"approved"}, c.Items[1].Labels.Integrity.Label.GetTags())

				// item 2: empty labels map
				assert.Equal(t, "item-2-empty-labels", c.Items[2].Data)
				require.NotNil(t, c.Items[2].Labels)
				assert.Empty(t, c.Items[2].Labels.Secrecy.Label.GetTags())
				assert.Empty(t, c.Items[2].Labels.Integrity.Label.GetTags())
			},
		},

		// ── Data field variety ─────────────────────────────────────────────────

		{
			name: "data field can be nil",
			items: []interface{}{
				map[string]interface{}{"data": nil},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				assert.Nil(t, c.Items[0].Data)
			},
		},
		{
			name: "data field can be a nested map",
			items: []interface{}{
				map[string]interface{}{
					"data": map[string]interface{}{"id": 42, "name": "test"},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				nested, ok := c.Items[0].Data.(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, 42, nested["id"])
			},
		},
		{
			name: "item map without data key stores nil data",
			items: []interface{}{
				map[string]interface{}{
					"labels": map[string]interface{}{
						"secrecy": []interface{}{"private"},
					},
				},
			},
			wantItemCount: 1,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				assert.Nil(t, c.Items[0].Data)
				require.NotNil(t, c.Items[0].Labels)
			},
		},

		// ── Collection integrity ───────────────────────────────────────────────

		{
			name: "collection preserves insertion order",
			items: func() []interface{} {
				result := make([]interface{}, 5)
				for i := 0; i < 5; i++ {
					result[i] = map[string]interface{}{"data": i}
				}
				return result
			}(),
			wantItemCount: 5,
			checkItems: func(t *testing.T, c *difc.CollectionLabeledData) {
				for i, item := range c.Items {
					assert.Equal(t, i, item.Data)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collection, err := parseCollectionLabeledData(tt.items)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, collection)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, collection)
			assert.Len(t, collection.Items, tt.wantItemCount)

			if tt.checkItems != nil {
				tt.checkItems(t, collection)
			}
		})
	}
}

// TestParseCollectionLabeledData_CollectionInterface verifies that the returned
// CollectionLabeledData satisfies the difc.LabeledData interface contract.
func TestParseCollectionLabeledData_CollectionInterface(t *testing.T) {
	items := []interface{}{
		map[string]interface{}{
			"data": "item-1",
			"labels": map[string]interface{}{
				"secrecy":   []interface{}{"private"},
				"integrity": []interface{}{"approved"},
			},
		},
		map[string]interface{}{
			"data": "item-2",
		},
	}

	collection, err := parseCollectionLabeledData(items)
	require.NoError(t, err)
	require.NotNil(t, collection)

	// Verify it satisfies LabeledData interface
	var _ difc.LabeledData = collection

	// Overall() should return aggregated labels from all items
	overall := collection.Overall()
	require.NotNil(t, overall)

	// ToResult() should return all items
	result, err := collection.ToResult()
	require.NoError(t, err)
	resultSlice, ok := result.([]interface{})
	require.True(t, ok)
	assert.Len(t, resultSlice, 2)
	assert.Equal(t, "item-1", resultSlice[0])
	assert.Equal(t, "item-2", resultSlice[1])
}

// TestParseResourceResponse_ReturnTypeSanity ensures the returned LabeledResource
// behaves correctly after being populated.
func TestParseResourceResponse_ReturnTypeSanity(t *testing.T) {
	input := map[string]interface{}{
		"resource": map[string]interface{}{
			"description": "test resource",
			"secrecy":     []interface{}{"tag-a"},
			"integrity":   []interface{}{"tag-b"},
		},
		"operation": "read",
	}

	resource, op, err := parseResourceResponse(input)
	require.NoError(t, err)
	require.NotNil(t, resource)

	assert.Equal(t, difc.OperationRead, op)

	// Verify tag lookup
	assert.True(t, resource.Secrecy.Label.Contains(difc.Tag("tag-a")))
	assert.False(t, resource.Secrecy.Label.Contains(difc.Tag("tag-b")))
	assert.True(t, resource.Integrity.Label.Contains(difc.Tag("tag-b")))
	assert.False(t, resource.Integrity.Label.Contains(difc.Tag("tag-a")))
}
