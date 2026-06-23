package mcp

import (
	"fmt"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logPagination = logger.New("mcp:pagination")

// paginatedPage holds a single page of results from a paginated SDK list call.
type paginatedPage[T any] struct {
	Items      []T
	NextCursor string
}

// paginateAllMaxPages is the maximum number of pages that paginateAll will fetch.
// This guards against misbehaving or adversarial backends that return an unbounded
// sequence of pages, which would otherwise consume unbounded memory and time.
const paginateAllMaxPages = 100

// PaginateAll is the canonical cursor-based pagination algorithm shared across the
// codebase. It collects all items from a sequence of paginated fetch calls.
//
// fetch is called with a cursor string (empty string for the first call) and must
// return the items for that page, the cursor for the next page (empty when done),
// and any error. PaginateAll stops as soon as a page returns an empty next-cursor.
//
// maxPages caps the total number of fetch calls to prevent runaway loops. It must
// be a positive integer; a value of 0 or negative disables the cap (no page limit),
// which should only be used in tests or when the caller enforces its own limit.
// Returns an error if the cap is reached or if the same cursor is returned twice (cycle).
func PaginateAll[T any](maxPages int, fetch func(cursor string) ([]T, string, error)) ([]T, error) {
	logPagination.Printf("PaginateAll: starting pagination, maxPages=%d", maxPages)
	var all []T
	cursor := ""
	seenCursors := make(map[string]struct{})
	for pageCount := 0; ; pageCount++ {
		if maxPages > 0 && pageCount >= maxPages {
			logPagination.Printf("PaginateAll: page limit reached, maxPages=%d, totalItems=%d", maxPages, len(all))
			return nil, fmt.Errorf("pagination exceeded %d-page limit", maxPages)
		}
		items, nextCursor, err := fetch(cursor)
		if err != nil {
			return nil, err
		}
		logPagination.Printf("PaginateAll: fetched page=%d, items=%d, hasNext=%v", pageCount+1, len(items), nextCursor != "")
		all = append(all, items...)
		if nextCursor == "" {
			break
		}
		if _, seen := seenCursors[nextCursor]; seen {
			logPagination.Printf("PaginateAll: cyclical cursor detected, page=%d, cursor=%q", pageCount+1, nextCursor)
			return nil, fmt.Errorf("pagination detected cyclical cursor %q", nextCursor)
		}
		seenCursors[nextCursor] = struct{}{}
		cursor = nextCursor
	}
	logPagination.Printf("PaginateAll: completed, totalItems=%d, pages=%d", len(all), len(seenCursors)+1)
	return all, nil
}

// paginateAll collects all items across paginated SDK list calls.
// It returns an error if the backend returns more than paginateAllMaxPages pages,
// protecting against runaway backends.
// The canonical shared algorithm is PaginateAll; paginateAll adds server-specific
// logging and richer error context on top of it.
func paginateAll[T any](
	serverID string,
	itemKind string,
	fetch func(cursor string) (paginatedPage[T], error),
) ([]T, error) {
	first, err := fetch("")
	if err != nil {
		return nil, err
	}
	all := make([]T, len(first.Items), max(len(first.Items), 1))
	copy(all, first.Items)
	logConn.Printf("list%s: received page of %d %s from serverID=%s", itemKind, len(first.Items), itemKind, serverID)

	cursor := first.NextCursor
	seenCursors := make(map[string]struct{})
	for pageCount := 1; cursor != ""; pageCount++ {
		if pageCount >= paginateAllMaxPages {
			return nil, fmt.Errorf("list%s: backend serverID=%s returned more than %d pages; aborting to prevent unbounded memory growth", itemKind, serverID, paginateAllMaxPages)
		}
		if _, seen := seenCursors[cursor]; seen {
			return nil, fmt.Errorf("list%s: backend serverID=%s returned cyclical cursor %q", itemKind, serverID, cursor)
		}
		seenCursors[cursor] = struct{}{}
		page, err := fetch(cursor)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		logConn.Printf("list%s: received page of %d %s (total so far: %d) from serverID=%s", itemKind, len(page.Items), itemKind, len(all), serverID)
		cursor = page.NextCursor
	}
	logConn.Printf("list%s: received %d %s total from serverID=%s", itemKind, len(all), itemKind, serverID)
	return all, nil
}

// listMCPItems is a generic helper for the list* family of MCP operations.
// It handles session validation, logging, pagination, and response marshalling,
// eliminating the boilerplate that was previously duplicated across listTools,
// listResources, and listPrompts.
func listMCPItems[Item any, Result any](
	c *Connection,
	kind string,
	fetchPage func(cursor string) (paginatedPage[Item], error),
	buildResult func([]Item) Result,
) (*Response, error) {
	if err := c.requireSDKSession(); err != nil {
		return nil, err
	}
	logConn.Printf("list%s: requesting %s list from backend serverID=%s", kind, kind, c.serverID)
	items, err := paginateAll(c.serverID, kind, fetchPage)
	if err != nil {
		return nil, err
	}
	return marshalToResponse(buildResult(items))
}

// listSDKItems adapts cursor-based SDK list calls to listMCPItems.
// Item is the per-entry type (e.g. *sdk.Tool), SDKResult is the SDK list
// response type (e.g. *sdk.ListToolsResult), and Result is the final marshalled
// response wrapper. list executes a page request for a cursor, toPage extracts
// items and next cursor from SDKResult, and buildResult wraps the collected items
// for JSON-RPC response marshalling.
func listSDKItems[Item any, SDKResult any, Result any](
	c *Connection,
	kind string,
	list func(cursor string) (SDKResult, error),
	toPage func(SDKResult) paginatedPage[Item],
	buildResult func([]Item) Result,
) (*Response, error) {
	return listMCPItems(c, kind,
		func(cursor string) (paginatedPage[Item], error) {
			result, err := list(cursor)
			if err != nil {
				return paginatedPage[Item]{}, err
			}
			return toPage(result), nil
		},
		buildResult,
	)
}
