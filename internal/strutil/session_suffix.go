package strutil

import "fmt"

// SessionSuffix returns a formatted session suffix for log messages.
// Returns " for session '<sessionID>'" when sessionID is non-empty, or "" otherwise.
func SessionSuffix(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	return fmt.Sprintf(" for session '%s'", sessionID)
}
