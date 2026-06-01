package strutil

// GetStringFromMap returns the string value for key when it is present and typed as string.
// It returns an empty string for missing keys, nil maps, and non-string values.
func GetStringFromMap(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

// DeepCloneJSON creates a deep copy of a JSON-compatible value.
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
