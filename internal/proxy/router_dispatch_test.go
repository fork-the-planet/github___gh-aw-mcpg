package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRouteMatchKey validates the dispatch-key extraction helper used by
// the optimised MatchRoute to narrow down candidate routes before regexp matching.
func TestRouteMatchKey(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		// /repos paths — key is the sub-resource segment after owner/repo
		{"/repos/owner/repo/issues/1/comments", "issues"},
		{"/repos/owner/repo/issues/1/labels", "issues"},
		{"/repos/owner/repo/issues", "issues"},
		{"/repos/owner/repo/pulls/1", "pulls"},
		{"/repos/owner/repo/pulls/1/files", "pulls"},
		{"/repos/owner/repo/actions/workflows", "actions"},
		{"/repos/owner/repo/actions/runs/123/artifacts", "actions"},
		{"/repos/owner/repo/commits/abc123", "commits"},
		{"/repos/owner/repo/contents/path/to/file.go", "contents"},
		{"/repos/owner/repo/labels/bug", "labels"},
		{"/repos/owner/repo/labels", "labels"},
		{"/repos/owner/repo/branches", "branches"},
		{"/repos/owner/repo/releases/latest", "releases"},
		{"/repos/owner/repo/discussions", "discussions"},
		{"/repos/owner/repo/git/trees/abc", "git"},
		{"/repos/owner/repo/environments/prod/secrets", "environments"},
		// /repos/owner/repo with no sub-resource → catch-all key ""
		{"/repos/owner/repo", ""},
		// Top-level paths
		{"/search/code", "search"},
		{"/search/issues", "search"},
		{"/search/repositories", "search"},
		{"/notifications", "notifications"},
		{"/user", "user"},
		{"/user/keys", "user"},
		{"/orgs/myorg/actions/secrets", "orgs"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, routeMatchKey(tt.path))
		})
	}
}

// TestBuildRouteDispatch validates that every route is reachable via its dispatch
// key: all route indices are present in exactly one bucket, and no index appears
// in more than one non-empty bucket.
func TestBuildRouteDispatch(t *testing.T) {
	d := buildRouteDispatch(routes)

	// Every route index must appear in exactly one bucket
	seen := make(map[int]string) // index → key
	for key, indices := range d {
		for _, idx := range indices {
			prev, already := seen[idx]
			require.False(t, already,
				"route index %d appears in both key %q and %q", idx, prev, key)
			seen[idx] = key
		}
	}

	for i := range routes {
		_, ok := seen[i]
		assert.True(t, ok, "route index %d is not reachable via any dispatch key", i)
	}
}

// TestRouteMatchKeyDispatchCorrectness verifies that MatchRoute still returns
// the correct result for a representative set of paths after the dispatch
// optimisation is applied.
func TestRouteMatchKeyDispatchCorrectness(t *testing.T) {
	tests := []struct {
		path     string
		wantTool string
	}{
		{"/repos/o/r/issues/1/comments", "issue_read"},
		{"/repos/o/r/issues", "list_issues"},
		{"/repos/o/r/pulls/1", "pull_request_read"},
		{"/repos/o/r/pulls", "list_pull_requests"},
		{"/repos/o/r/actions/workflows", "actions_list"},
		{"/repos/o/r/actions/runs/123/artifacts", "actions_list"},
		{"/repos/o/r/commits", "list_commits"},
		{"/repos/o/r/branches", "list_branches"},
		{"/repos/o/r/releases/latest", "get_latest_release"},
		{"/repos/o/r/labels", "list_labels"},
		{"/repos/o/r/discussions", "list_discussions"},
		{"/repos/o/r/git/trees/abc", "get_file_contents"},
		{"/search/code", "search_code"},
		{"/search/issues", "search_issues"},
		{"/search/repositories", "search_repositories"},
		{"/user", "get_me"},
		{"/notifications", "list_notifications"},
		{"/repos/o/r", "get_file_contents"},                 // generic catch-all
		{"/repos/o/r/contents/foo.go", "get_file_contents"}, // contents → get_file_contents
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			m := MatchRoute(tt.path)
			require.NotNil(t, m, "expected non-nil match for %q", tt.path)
			assert.Equal(t, tt.wantTool, m.ToolName)
		})
	}
}

// BenchmarkMatchRoute_Issues benchmarks dispatch overhead on an issue path.
// This path matched the first route before dispatching and still does afterward,
// so it serves as a control for the overhead added to an existing fast path.
func BenchmarkMatchRoute_Issues(b *testing.B) {
	path := "/repos/github/gh-aw-mcpg/issues/123/comments"
	for b.Loop() {
		MatchRoute(path)
	}
}

// BenchmarkMatchRoute_Actions benchmarks MatchRoute on an Actions path — the
// largest bucket with 14 routes. Before optimisation, all preceding routes were
// tried first. After optimisation only the 14 "actions" routes are tried.
func BenchmarkMatchRoute_Actions(b *testing.B) {
	path := "/repos/github/gh-aw-mcpg/actions/runs/12345/artifacts"
	for b.Loop() {
		MatchRoute(path)
	}
}

// BenchmarkMatchRoute_Search benchmarks MatchRoute on a search path.
func BenchmarkMatchRoute_Search(b *testing.B) {
	path := "/search/repositories"
	for b.Loop() {
		MatchRoute(path)
	}
}
