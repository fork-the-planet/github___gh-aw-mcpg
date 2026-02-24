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
