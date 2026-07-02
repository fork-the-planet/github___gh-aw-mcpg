package util

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// SortedSetKeys returns the keys of a string set (map[string]struct{}) as a sorted slice.
// Returns an empty (non-nil) slice when the set is empty.
func SortedSetKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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

// DeepCloneJSON creates a deep copy of a JSON-compatible value.
// It handles the three container types used by encoding/json:
// map[string]interface{} (JSON objects), []interface{} (JSON arrays),
// and any other type (JSON scalars: string, float64, bool, nil), which is
// returned as-is since scalar values are not reference types and need no cloning.
func DeepCloneJSON(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		clone := make(map[string]interface{}, len(val))
		for k, v := range val {
			clone[k] = DeepCloneJSON(v)
		}
		return clone
	case []interface{}:
		clone := make([]interface{}, len(val))
		for i, v := range val {
			clone[i] = DeepCloneJSON(v)
		}
		return clone
	default:
		return v
	}
}

// InterfaceToIntString attempts to convert a JSON-decoded numeric interface value
// (float64 or json.Number) to its decimal integer string representation.
// Returns ("", false) if the value is not a numeric type or is non-integer.
func InterfaceToIntString(v interface{}) (string, bool) {
	switch n := v.(type) {
	case float64:
		// Explicitly guard against out-of-range values before conversion, since
		// converting an out-of-range float64 to int64 is implementation-defined in Go.
		// float64(math.MaxInt64) rounds up to 9.223372036854776e18, so use >=
		// for the upper bound. float64(math.MinInt64) = -(2^63) is exactly
		// representable, so < is appropriate for the lower bound.
		if n < float64(math.MinInt64) || n >= float64(math.MaxInt64) {
			return "", false // out of int64 range
		}
		i := int64(n)
		if n != float64(i) {
			return "", false // non-integer float
		}
		return fmt.Sprintf("%d", i), true
	case json.Number:
		// Validate that the json.Number represents a valid integer and convert to
		// a canonical decimal string (avoids non-canonical forms like "00123").
		i, err := n.Int64()
		if err != nil {
			return "", false // non-integer or out-of-range json.Number
		}
		return fmt.Sprintf("%d", i), true
	}
	return "", false
}
