package difc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSingularReadTool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		toolName string
		want     bool
	}{
		// Singular read tools: any name not starting with "list_" or "search_"
		{
			name:     "get_ prefix is a singular read tool",
			toolName: "get_issue",
			want:     true,
		},
		{
			name:     "get_ prefix with long name is a singular read tool",
			toolName: "get_file_contents",
			want:     true,
		},
		{
			name:     "_read suffix is a singular read tool",
			toolName: "issue_read",
			want:     true,
		},
		{
			name:     "create_ prefix is treated as a singular tool",
			toolName: "create_issue",
			want:     true,
		},
		{
			name:     "update_ prefix is treated as a singular tool",
			toolName: "update_pull_request",
			want:     true,
		},
		{
			name:     "delete_ prefix is treated as a singular tool",
			toolName: "delete_branch",
			want:     true,
		},
		{
			name:     "read_ prefix is a singular read tool",
			toolName: "read_file",
			want:     true,
		},
		{
			name:     "arbitrary non-prefixed tool name is singular",
			toolName: "fork_repository",
			want:     true,
		},

		// Collection tools: prefix "list_" → false
		{
			name:     "list_ prefix is a collection tool",
			toolName: "list_issues",
			want:     false,
		},
		{
			name:     "list_ prefix with different resource is a collection tool",
			toolName: "list_commits",
			want:     false,
		},
		{
			name:     "list_ prefix with multi-word resource",
			toolName: "list_pull_requests",
			want:     false,
		},
		{
			name:     "list_ alone (nothing after underscore) is a collection tool",
			toolName: "list_",
			want:     false,
		},

		// Collection tools: prefix "search_" → false
		{
			name:     "search_ prefix is a collection tool",
			toolName: "search_code",
			want:     false,
		},
		{
			name:     "search_ prefix with different resource",
			toolName: "search_issues",
			want:     false,
		},
		{
			name:     "search_ prefix with multi-word resource",
			toolName: "search_pull_requests",
			want:     false,
		},
		{
			name:     "search_ alone is a collection tool",
			toolName: "search_",
			want:     false,
		},

		// Edge cases: prefix without underscore does NOT match
		{
			name:     "list without underscore is singular (no list_ prefix)",
			toolName: "list",
			want:     true,
		},
		{
			name:     "search without underscore is singular (no search_ prefix)",
			toolName: "search",
			want:     true,
		},

		// Edge cases: similar but not matching prefixes
		{
			name:     "listed_ prefix does not match list_ — is singular",
			toolName: "listed_items",
			want:     true,
		},
		{
			name:     "listing_ prefix does not match list_ — is singular",
			toolName: "listing_files",
			want:     true,
		},
		{
			name:     "searching_ prefix does not match search_ — is singular",
			toolName: "searching_code",
			want:     true,
		},
		{
			name:     "get_list name contains list but does not start with list_",
			toolName: "get_list",
			want:     true,
		},
		{
			name:     "global_search name contains search but does not start with search_",
			toolName: "global_search",
			want:     true,
		},

		// Edge cases: case sensitivity — Go's strings.HasPrefix is case-sensitive
		{
			name:     "List_ with capital L is singular (case-sensitive)",
			toolName: "List_issues",
			want:     true,
		},
		{
			name:     "LIST_ uppercase is singular (case-sensitive)",
			toolName: "LIST_ISSUES",
			want:     true,
		},
		{
			name:     "Search_ with capital S is singular (case-sensitive)",
			toolName: "Search_code",
			want:     true,
		},
		{
			name:     "SEARCH_ uppercase is singular (case-sensitive)",
			toolName: "SEARCH_CODE",
			want:     true,
		},

		// Edge cases: empty and minimal strings
		{
			name:     "empty string is singular (no prefix match)",
			toolName: "",
			want:     true,
		},
		{
			name:     "single character is singular",
			toolName: "x",
			want:     true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsSingularReadTool(tt.toolName)
			assert.Equal(t, tt.want, got, "IsSingularReadTool(%q)", tt.toolName)
		})
	}
}
