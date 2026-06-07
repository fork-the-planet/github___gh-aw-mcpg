package strutil

// GetStringFromMap returns the first non-empty string value found for any of
// the given keys in m.  For each key, the value must be present, typed as
// string, and non-empty to be returned.  Returns an empty string when no
// matching non-empty string value is found, when the map is nil, or when no
// keys are provided.
//
// With a single key the behaviour is identical to a plain map type-assertion:
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
