package strutil

// Truncate truncates a string to the specified maximum length.
// If the string is longer than maxLen, it's truncated and "..." is appended.
// If maxLen is 0, returns "..." for non-empty strings, empty string for empty strings.
// If maxLen is negative, the original string is returned.
func Truncate(s string, maxLen int) string {
	if maxLen < 0 {
		return s
	}
	if maxLen == 0 {
		if len(s) > 0 {
			return "..."
		}
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// TruncateWithSuffix truncates a string to the specified maximum length with a custom suffix.
// If the string is longer than maxLen, it's truncated and suffix is appended.
// If maxLen is 0 or negative, the original string is returned.
func TruncateWithSuffix(s string, maxLen int, suffix string) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + suffix
}

// TruncateSessionID returns a truncated session ID for safe logging (first 8 bytes).
// Returns "(none)" for empty session IDs, and appends "..." for truncated values.
// This is useful for logging session IDs without exposing sensitive information.
func TruncateSessionID(sessionID string) string {
	if sessionID == "" {
		return "(none)"
	}
	return Truncate(sessionID, 8)
}

// TruncateRunes truncates s to at most maxRunes Unicode code points (runes).
// Unlike Truncate, which counts bytes, TruncateRunes is safe for non-ASCII
// content (e.g. emoji, CJK characters). If maxRunes is 0 or negative, returns
// an empty string.
func TruncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes])
}

// GetStringFromMap returns the string value for key when it is present and typed as string.
// It returns an empty string for missing keys, nil maps, and non-string values.
func GetStringFromMap(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
