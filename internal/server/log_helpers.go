package server

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/util"
)

// truncateSessionID returns a truncated session ID for safe logging (first 8 bytes).
// Returns "(none)" for empty session IDs, and appends "..." for truncated values.
func truncateSessionID(sessionID string) string {
	if sessionID == "" {
		return "(none)"
	}
	return util.Truncate(sessionID, 8)
}

// truncateCacheKeyForLog returns a log-safe version of a cache key of the form
// "backendID/sessionID" by truncating the session ID portion.
func truncateCacheKeyForLog(key string) string {
	backendID, sessionID, found := strings.Cut(key, "/")
	if !found {
		return key
	}

	return fmt.Sprintf("%s/%s", backendID, truncateSessionID(sessionID))
}
