package mcp

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
		{name: "get prefix is singular", toolName: "get_issue", want: true},
		{name: "read suffix is singular", toolName: "issue_read", want: true},
		{name: "create tool is singular", toolName: "create_issue", want: true},
		{name: "list prefix is collection", toolName: "list_issues", want: false},
		{name: "search prefix is collection", toolName: "search_code", want: false},
		{name: "list without underscore stays singular", toolName: "list", want: true},
		{name: "search without underscore stays singular", toolName: "search", want: true},
		{name: "case sensitive prefix check", toolName: "List_issues", want: true},
		{name: "empty string defaults singular", toolName: "", want: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsSingularReadTool(tt.toolName))
		})
	}
}
