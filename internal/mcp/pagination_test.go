package mcp

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPaginateAllExported tests the exported PaginateAll cursor-based pagination helper.
func TestPaginateAllExported(t *testing.T) {
	t.Run("single page with no next cursor", func(t *testing.T) {
		fetch := func(cursor string) ([]string, string, error) {
			assert.Equal(t, "", cursor, "first call should use empty cursor")
			return []string{"a", "b", "c"}, "", nil
		}
		items, err := PaginateAll(10, fetch)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, items)
	})

	t.Run("empty first page returns nil", func(t *testing.T) {
		fetch := func(cursor string) ([]string, string, error) {
			return nil, "", nil
		}
		items, err := PaginateAll(10, fetch)
		require.NoError(t, err)
		assert.Nil(t, items)
	})

	t.Run("multiple pages accumulate all items", func(t *testing.T) {
		callCount := 0
		cursors := []string{"", "cursor1", "cursor2"}
		responses := []struct {
			items      []int
			nextCursor string
		}{
			{[]int{1, 2}, "cursor1"},
			{[]int{3, 4}, "cursor2"},
			{[]int{5}, ""},
		}

		fetch := func(cursor string) ([]int, string, error) {
			require.Less(t, callCount, len(cursors), "unexpected extra fetch call")
			assert.Equal(t, cursors[callCount], cursor)
			r := responses[callCount]
			callCount++
			return r.items, r.nextCursor, nil
		}

		items, err := PaginateAll(10, fetch)
		require.NoError(t, err)
		assert.Equal(t, []int{1, 2, 3, 4, 5}, items)
		assert.Equal(t, 3, callCount)
	})

	t.Run("fetch error on first call returns error immediately", func(t *testing.T) {
		fetchErr := errors.New("backend unavailable")
		fetch := func(cursor string) ([]string, string, error) {
			return nil, "", fetchErr
		}
		items, err := PaginateAll(10, fetch)
		require.Error(t, err)
		assert.ErrorIs(t, err, fetchErr)
		assert.Nil(t, items)
	})

	t.Run("fetch error on subsequent page returns error", func(t *testing.T) {
		callCount := 0
		fetch := func(cursor string) ([]string, string, error) {
			callCount++
			if callCount == 1 {
				return []string{"x"}, "cursor1", nil
			}
			return nil, "", errors.New("page 2 failed")
		}
		items, err := PaginateAll(10, fetch)
		require.Error(t, err)
		assert.ErrorContains(t, err, "page 2 failed")
		assert.Nil(t, items)
	})

	t.Run("maxPages zero disables cap", func(t *testing.T) {
		// 5 pages with maxPages=0 should succeed without hitting a cap
		callCount := 0
		fetch := func(cursor string) ([]int, string, error) {
			callCount++
			if callCount < 5 {
				return []int{callCount}, fmt.Sprintf("c%d", callCount), nil
			}
			return []int{callCount}, "", nil
		}
		items, err := PaginateAll(0, fetch)
		require.NoError(t, err)
		assert.Len(t, items, 5)
	})

	t.Run("maxPages negative disables cap", func(t *testing.T) {
		// 5 pages with maxPages=-1 should succeed without hitting a cap
		callCount := 0
		fetch := func(cursor string) ([]int, string, error) {
			callCount++
			if callCount < 5 {
				return []int{callCount}, fmt.Sprintf("c%d", callCount), nil
			}
			return []int{callCount}, "", nil
		}
		items, err := PaginateAll(-1, fetch)
		require.NoError(t, err)
		assert.Len(t, items, 5)
	})

	t.Run("maxPages exceeded returns error with page limit in message", func(t *testing.T) {
		const maxPages = 3
		fetch := func(cursor string) ([]string, string, error) {
			// Always return a next cursor to force pagination to continue
			return []string{"item"}, fmt.Sprintf("cursor-%s-next", cursor), nil
		}
		items, err := PaginateAll(maxPages, fetch)
		require.Error(t, err)
		assert.ErrorContains(t, err, "pagination exceeded")
		assert.ErrorContains(t, err, fmt.Sprintf("%d", maxPages))
		assert.Nil(t, items)
	})

	t.Run("cyclical cursor returns error with cursor value in message", func(t *testing.T) {
		callCount := 0
		fetch := func(cursor string) ([]string, string, error) {
			callCount++
			if callCount == 1 {
				return []string{"a"}, "loop-cursor", nil
			}
			// Return the same cursor to create a cycle
			return []string{"b"}, "loop-cursor", nil
		}
		items, err := PaginateAll(100, fetch)
		require.Error(t, err)
		assert.ErrorContains(t, err, "loop-cursor")
		assert.Nil(t, items)
	})

	t.Run("cyclical cursor error message includes cursor value", func(t *testing.T) {
		const cycleCursor = "my-repeating-cursor"
		callCount := 0
		fetch := func(cursor string) ([]string, string, error) {
			callCount++
			if callCount == 1 {
				return []string{"first"}, cycleCursor, nil
			}
			return []string{"second"}, cycleCursor, nil
		}
		_, err := PaginateAll(100, fetch)
		require.Error(t, err)
		assert.ErrorContains(t, err, cycleCursor)
		assert.ErrorContains(t, err, "cyclical cursor")
	})

	t.Run("maxPages=1 succeeds when single page has no next cursor", func(t *testing.T) {
		fetch := func(cursor string) ([]string, string, error) {
			return []string{"only"}, "", nil
		}
		items, err := PaginateAll(1, fetch)
		require.NoError(t, err)
		assert.Equal(t, []string{"only"}, items)
	})

	t.Run("maxPages=1 fails when first page returns next cursor", func(t *testing.T) {
		callCount := 0
		fetch := func(cursor string) ([]string, string, error) {
			callCount++
			return []string{"item"}, "next", nil
		}
		items, err := PaginateAll(1, fetch)
		require.Error(t, err)
		assert.ErrorContains(t, err, "pagination exceeded")
		// Only the first page should be fetched before hitting the cap on the next loop
		assert.Equal(t, 1, callCount)
		assert.Nil(t, items)
	})

	t.Run("exactly maxPages pages with final empty cursor succeeds", func(t *testing.T) {
		const maxPages = 3
		callCount := 0
		fetch := func(cursor string) ([]int, string, error) {
			callCount++
			if callCount < maxPages {
				return []int{callCount}, fmt.Sprintf("c%d", callCount), nil
			}
			return []int{callCount}, "", nil
		}
		items, err := PaginateAll(maxPages, fetch)
		require.NoError(t, err)
		assert.Len(t, items, maxPages)
		assert.Equal(t, maxPages, callCount)
	})

	t.Run("cursor passing is correct across pages", func(t *testing.T) {
		receivedCursors := make([]string, 0, 4)
		fetch := func(cursor string) ([]string, string, error) {
			receivedCursors = append(receivedCursors, cursor)
			switch cursor {
			case "":
				return []string{"a"}, "alpha", nil
			case "alpha":
				return []string{"b"}, "beta", nil
			case "beta":
				return []string{"c"}, "", nil
			default:
				return nil, "", fmt.Errorf("unexpected cursor %q", cursor)
			}
		}
		items, err := PaginateAll(10, fetch)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, items)
		assert.Equal(t, []string{"", "alpha", "beta"}, receivedCursors)
	})
}

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
		assert.NotNil(t, items)
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
		assert.ErrorContains(t, err, "exceeded")
		assert.ErrorContains(t, err, "page limit")
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

// TestCallParamMethod_MarshalParamsError tests that callParamMethod returns an error
// when rawParams cannot be marshalled (e.g. a channel value).
func TestCallParamMethod_MarshalParamsError(t *testing.T) {
	// Set a non-nil session so requireSDKSession() passes, allowing us to reach unmarshalParams.
	c := &Connection{session: &sdk.ClientSession{}}
	resp, err := callParamMethod(c, make(chan int), func(_ struct{}) (interface{}, error) {
		return nil, nil
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

// TestListMCPItems_PaginateError tests that listMCPItems propagates errors from paginateAll.
func TestListMCPItems_PaginateError(t *testing.T) {
	// Set a non-nil session so requireSDKSession() passes.
	c := &Connection{session: &sdk.ClientSession{}}
	fetchErr := errors.New("backend fetch failed")
	resp, err := listMCPItems(
		c,
		"Tools",
		func(_ string) (paginatedPage[string], error) {
			return paginatedPage[string]{}, fetchErr
		},
		func(items []string) []string { return items },
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, fetchErr)
	assert.Nil(t, resp)
}

// TestListSDKItems_NilSession verifies that listSDKItems checks session availability
// before calling the SDK list function.
func TestListSDKItems_NilSession(t *testing.T) {
	conn := newTestConnection(t)
	conn.session = nil
	listCalled := false
	type fakeItem struct {
		Name string
	}
	type fakeListResult struct {
		Items      []fakeItem
		NextCursor string
	}

	_, err := listSDKItems(
		conn,
		"tools",
		func(_ string) (fakeListResult, error) {
			listCalled = true
			return fakeListResult{}, nil
		},
		func(result fakeListResult) paginatedPage[fakeItem] {
			return paginatedPage[fakeItem](result)
		},
		func(items []fakeItem) []fakeItem { return items },
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "SDK session not available")
	assert.False(t, listCalled, "list should not be called when session is unavailable")
}

// TestCallParamMethod_FnError tests that callParamMethod propagates errors from fn.
func TestCallParamMethod_FnError(t *testing.T) {
	c := &Connection{session: &sdk.ClientSession{}}
	fnErr := errors.New("fn execution failed")
	resp, err := callParamMethod(c, nil, func(_ struct{}) (interface{}, error) {
		return nil, fnErr
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, fnErr)
	assert.Nil(t, resp)
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
