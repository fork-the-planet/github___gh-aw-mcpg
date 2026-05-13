package mcp

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPaginateAll tests the paginateAll generic pagination helper.
func TestPaginateAllHelper(t *testing.T) {
	t.Run("single page with no next cursor", func(t *testing.T) {
		fetch := func(cursor string) (paginatedPage[string], error) {
			assert.Equal(t, "", cursor, "first call should use empty cursor")
			return paginatedPage[string]{
				Items:      []string{"a", "b", "c"},
				NextCursor: "",
			}, nil
		}

		items, err := paginateAll("server1", "Tools", fetch)

		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, items)
	})

	t.Run("empty first page", func(t *testing.T) {
		fetch := func(cursor string) (paginatedPage[string], error) {
			return paginatedPage[string]{Items: nil, NextCursor: ""}, nil
		}

		items, err := paginateAll("server1", "Tools", fetch)

		require.NoError(t, err)
		assert.Empty(t, items)
	})

	t.Run("multiple pages accumulate all items", func(t *testing.T) {
		pages := []paginatedPage[int]{
			{Items: []int{1, 2}, NextCursor: "cursor1"},
			{Items: []int{3, 4}, NextCursor: "cursor2"},
			{Items: []int{5}, NextCursor: ""},
		}
		callCount := 0

		fetch := func(cursor string) (paginatedPage[int], error) {
			page := pages[callCount]
			callCount++
			return page, nil
		}

		items, err := paginateAll("server1", "Resources", fetch)

		require.NoError(t, err)
		assert.Equal(t, []int{1, 2, 3, 4, 5}, items)
		assert.Equal(t, 3, callCount)
	})

	t.Run("two pages with last cursor empty", func(t *testing.T) {
		callCount := 0
		fetch := func(cursor string) (paginatedPage[string], error) {
			callCount++
			if cursor == "" {
				return paginatedPage[string]{Items: []string{"x"}, NextCursor: "next"}, nil
			}
			return paginatedPage[string]{Items: []string{"y", "z"}, NextCursor: ""}, nil
		}

		items, err := paginateAll("server2", "Prompts", fetch)

		require.NoError(t, err)
		assert.Equal(t, []string{"x", "y", "z"}, items)
		assert.Equal(t, 2, callCount)
	})

	t.Run("error on first fetch returns error", func(t *testing.T) {
		expectedErr := errors.New("backend unavailable")
		fetch := func(cursor string) (paginatedPage[string], error) {
			return paginatedPage[string]{}, expectedErr
		}

		items, err := paginateAll("server1", "Tools", fetch)

		require.Error(t, err)
		assert.ErrorIs(t, err, expectedErr)
		assert.Nil(t, items)
	})

	t.Run("error on subsequent page fetch returns error", func(t *testing.T) {
		callCount := 0
		fetch := func(cursor string) (paginatedPage[string], error) {
			callCount++
			if callCount == 1 {
				return paginatedPage[string]{Items: []string{"a"}, NextCursor: "cursor1"}, nil
			}
			return paginatedPage[string]{}, errors.New("page 2 failed")
		}

		items, err := paginateAll("server1", "Tools", fetch)

		require.Error(t, err)
		assert.ErrorContains(t, err, "page 2 failed")
		assert.Nil(t, items)
	})

	t.Run("cyclical cursor returns error", func(t *testing.T) {
		callCount := 0
		fetch := func(cursor string) (paginatedPage[string], error) {
			callCount++
			if callCount == 1 {
				return paginatedPage[string]{Items: []string{"a"}, NextCursor: "cursor1"}, nil
			}
			// Return the same cursor to create a cycle
			return paginatedPage[string]{Items: []string{"b"}, NextCursor: "cursor1"}, nil
		}

		items, err := paginateAll("server1", "Tools", fetch)

		require.Error(t, err)
		assert.ErrorContains(t, err, "cyclical cursor")
		assert.ErrorContains(t, err, "cursor1")
		assert.Nil(t, items)
	})

	t.Run("cyclical cursor error includes server ID", func(t *testing.T) {
		const serverID = "my-server"
		callCount := 0
		fetch := func(cursor string) (paginatedPage[string], error) {
			callCount++
			if callCount == 1 {
				return paginatedPage[string]{Items: []string{"a"}, NextCursor: "loop"}, nil
			}
			return paginatedPage[string]{Items: []string{"b"}, NextCursor: "loop"}, nil
		}

		_, err := paginateAll(serverID, "Tools", fetch)

		require.Error(t, err)
		assert.ErrorContains(t, err, serverID)
	})

	t.Run("max pages limit returns error", func(t *testing.T) {
		// Generate paginateAllMaxPages+1 pages to trigger the limit
		fetch := func(cursor string) (paginatedPage[string], error) {
			pageNum := 0
			if cursor != "" {
				if !strings.HasPrefix(cursor, "cursor") {
					return paginatedPage[string]{}, fmt.Errorf("invalid cursor format: %q", cursor)
				}
				parsed, err := strconv.Atoi(strings.TrimPrefix(cursor, "cursor"))
				if err != nil {
					return paginatedPage[string]{}, fmt.Errorf("invalid cursor format: %q: %w", cursor, err)
				}
				pageNum = parsed
			}
			nextCursor := fmt.Sprintf("cursor%d", pageNum+1)
			return paginatedPage[string]{
				Items:      []string{"item"},
				NextCursor: nextCursor,
			}, nil
		}

		items, err := paginateAll("server1", "Tools", fetch)

		require.Error(t, err)
		assert.ErrorContains(t, err, "more than")
		assert.ErrorContains(t, err, "pages")
		assert.Nil(t, items)
	})

	t.Run("max pages error includes server ID and item kind", func(t *testing.T) {
		const serverID = "my-backend"
		const itemKind = "Resources"
		callCount := 0
		fetch := func(cursor string) (paginatedPage[string], error) {
			callCount++
			return paginatedPage[string]{
				Items:      []string{"item"},
				NextCursor: fmt.Sprintf("cursor%d", callCount),
			}, nil
		}

		_, err := paginateAll(serverID, itemKind, fetch)

		require.Error(t, err)
		assert.ErrorContains(t, err, serverID)
		assert.ErrorContains(t, err, itemKind)
	})

	t.Run("exactly paginateAllMaxPages-1 pages succeeds", func(t *testing.T) {
		// paginateAllMaxPages is 100; use 99 pages (pageCount goes 1..98, checking >= 100)
		// Page 0 (empty cursor) + pages 1..98 = 99 pages total
		const totalPages = paginateAllMaxPages - 1
		callCount := 0
		fetch := func(cursor string) (paginatedPage[int], error) {
			callCount++
			if callCount < totalPages {
				return paginatedPage[int]{
					Items:      []int{callCount},
					NextCursor: fmt.Sprintf("c%d", callCount),
				}, nil
			}
			return paginatedPage[int]{Items: []int{callCount}, NextCursor: ""}, nil
		}

		items, err := paginateAll("server1", "Items", fetch)

		require.NoError(t, err)
		assert.Len(t, items, totalPages)
	})

	t.Run("different cursors on different pages are all valid", func(t *testing.T) {
		// First call uses empty cursor, then cursor "alpha", then "beta"
		fetch2 := func(cursor string) (paginatedPage[string], error) {
			switch cursor {
			case "":
				return paginatedPage[string]{Items: []string{"a"}, NextCursor: "alpha"}, nil
			case "alpha":
				return paginatedPage[string]{Items: []string{"b"}, NextCursor: "beta"}, nil
			case "beta":
				return paginatedPage[string]{Items: []string{"c"}, NextCursor: "gamma"}, nil
			case "gamma":
				return paginatedPage[string]{Items: []string{"d"}, NextCursor: ""}, nil
			default:
				return paginatedPage[string]{}, fmt.Errorf("unexpected cursor: %q", cursor)
			}
		}

		items, err := paginateAll("server1", "Tools", fetch2)

		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c", "d"}, items)
	})

	t.Run("works with struct item types", func(t *testing.T) {
		type toolItem struct {
			Name string
		}
		fetch := func(cursor string) (paginatedPage[toolItem], error) {
			if cursor == "" {
				return paginatedPage[toolItem]{
					Items:      []toolItem{{Name: "tool1"}, {Name: "tool2"}},
					NextCursor: "next",
				}, nil
			}
			return paginatedPage[toolItem]{
				Items:      []toolItem{{Name: "tool3"}},
				NextCursor: "",
			}, nil
		}

		items, err := paginateAll("server1", "Tools", fetch)

		require.NoError(t, err)
		require.Len(t, items, 3)
		assert.Equal(t, "tool1", items[0].Name)
		assert.Equal(t, "tool2", items[1].Name)
		assert.Equal(t, "tool3", items[2].Name)
	})

	t.Run("server ID and item kind are passed in error messages for max pages", func(t *testing.T) {
		const serverID = "critical-server"
		const itemKind = "Tools"
		pageNum := 0
		fetch := func(cursor string) (paginatedPage[string], error) {
			pageNum++
			return paginatedPage[string]{
				Items:      []string{"item"},
				NextCursor: fmt.Sprintf("cursor%d", pageNum),
			}, nil
		}

		_, err := paginateAll(serverID, itemKind, fetch)

		require.Error(t, err)
		assert.ErrorContains(t, err, serverID)
		assert.ErrorContains(t, err, itemKind)
		assert.ErrorContains(t, err, fmt.Sprintf("%d", paginateAllMaxPages))
	})
}

// TestPaginateAllMaxPagesConstant verifies the safety constant has expected value.
func TestPaginateAllMaxPagesConstant(t *testing.T) {
	assert.Equal(t, 100, paginateAllMaxPages,
		"paginateAllMaxPages should be 100 to guard against runaway backends")
}

// TestPaginateAll_ItemOrdering verifies item ordering is preserved across pages.
func TestPaginateAll_ItemOrdering(t *testing.T) {
	// Verify that items from later pages appear after items from earlier pages
	fetch := func(cursor string) (paginatedPage[int], error) {
		switch cursor {
		case "":
			return paginatedPage[int]{Items: []int{10, 20, 30}, NextCursor: "p2"}, nil
		case "p2":
			return paginatedPage[int]{Items: []int{40, 50}, NextCursor: "p3"}, nil
		case "p3":
			return paginatedPage[int]{Items: []int{60}, NextCursor: ""}, nil
		default:
			return paginatedPage[int]{}, fmt.Errorf("unexpected cursor %q", cursor)
		}
	}

	items, err := paginateAll("server1", "Numbers", fetch)

	require.NoError(t, err)
	assert.Equal(t, []int{10, 20, 30, 40, 50, 60}, items)
}
