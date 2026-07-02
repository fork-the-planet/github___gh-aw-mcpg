package util

import (
	"sort"
	"strings"
)

// DeduplicateStrings returns a new slice with whitespace-trimmed, empty, and duplicate
// entries removed from input. When sorted is true the result is sorted in ascending order.
// The relative order of first-seen entries is preserved when sorted is false.
func DeduplicateStrings(input []string, sorted bool) []string {
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, s := range input {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, exists := seen[s]; exists {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if sorted {
		sort.Strings(out)
	}
	return out
}

// StringsToAny converts a []string to []interface{}.
func StringsToAny(input []string) []interface{} {
	out := make([]interface{}, len(input))
	for i, value := range input {
		out[i] = value
	}
	return out
}

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
