package server

import "github.com/github/gh-aw-mcpg/internal/strutil"

// truncateSessionID returns a truncated session ID for safe logging (first 8 bytes).
// Returns "(none)" for empty session IDs, and appends "..." for truncated values.
func truncateSessionID(sessionID string) string {
	if sessionID == "" {
		return "(none)"
	}
	return strutil.Truncate(sessionID, 8)
}
