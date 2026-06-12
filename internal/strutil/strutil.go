package strutil

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

// GetStringFromMap returns the first non-empty string value found for any of
// the given keys in m.  For each key, the value must be present, typed as
// string, and non-empty to be returned.  Returns an empty string when no
// matching non-empty string value is found, when the map is nil, or when no
// keys are provided.
//
// With a single key the behaviour is equivalent to `v, _ := m[key].(string)`:
//
//	GetStringFromMap(m, "owner")
//
// With multiple keys the function returns the first non-empty match, which is
// useful for maps that may use either snake_case or camelCase field names:
//
//	GetStringFromMap(m, "html_url", "htmlUrl")
func GetStringFromMap(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
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
