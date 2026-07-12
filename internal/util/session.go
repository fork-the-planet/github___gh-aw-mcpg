package util

// FormatSessionIDForLog returns a log-safe session ID representation.
// Empty session IDs are rendered as "(none)"; non-empty IDs are truncated to
// the first 8 bytes with an ellipsis when needed.
func FormatSessionIDForLog(sessionID string) string {
	const sessionIDLogMaxLen = 8
	if sessionID == "" {
		return "(none)"
	}
	return Truncate(sessionID, sessionIDLogMaxLen)
}
