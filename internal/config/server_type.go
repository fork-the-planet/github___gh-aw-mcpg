package config

// IsStdioServerType reports whether t is a stdio-family server type.
// Empty type, "stdio", and the "local" alias all map to stdio.
func IsStdioServerType(t string) bool {
	return t == "" || t == "stdio" || t == "local"
}

// NormalizeServerType maps legacy/empty type strings to canonical values.
// "" and "local" normalize to "stdio"; all other values are returned unchanged.
func NormalizeServerType(t string) string {
	if t == "" || t == "local" {
		return "stdio"
	}
	return t
}
