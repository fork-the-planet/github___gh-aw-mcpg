package difc

import "strings"

// IsSingularReadTool returns true when toolName refers to a tool expected to
// return a single resource (e.g. get_*, *_read). List/search tools are treated
// as collection tools even if they happen to return one item.
func IsSingularReadTool(toolName string) bool {
	return !strings.HasPrefix(toolName, "list_") && !strings.HasPrefix(toolName, "search_")
}
