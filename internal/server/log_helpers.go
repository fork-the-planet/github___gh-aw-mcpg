package server

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/util"
)

// truncateCacheKeyForLog returns a log-safe version of a cache key of the form
// "backendID/sessionID" by truncating the session ID portion.
func truncateCacheKeyForLog(key string) string {
	backendID, sessionID, found := strings.Cut(key, "/")
	if !found {
		return key
	}

	return fmt.Sprintf("%s/%s", backendID, util.FormatSessionIDForLog(sessionID))
}
