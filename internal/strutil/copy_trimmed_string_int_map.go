package strutil

import "strings"

// CopyTrimmedStringIntMap returns a defensive copy of a string→int map with
// whitespace trimmed from all keys.
func CopyTrimmedStringIntMap(input map[string]int) map[string]int {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]int, len(input))
	for key, value := range input {
		out[strings.TrimSpace(key)] = value
	}
	return out
}
