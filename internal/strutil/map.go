package strutil

// GetStringFromMap returns the string value for key when it is present and typed as string.
// It returns an empty string for missing keys, nil maps, and non-string values.
func GetStringFromMap(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
