package mcpresult

// NormalizeContentItems normalizes an MCP "content" field into a slice of item
// maps. It supports both []interface{} values produced by json.Unmarshal and
// []map[string]interface{} values produced by helper constructors.
//
// Non-map items in []interface{} are skipped so callers can decide whether to
// ignore them or treat them as an error.
func NormalizeContentItems(contentVal interface{}) ([]map[string]interface{}, bool) {
	switch v := contentVal.(type) {
	case []interface{}:
		items := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			ci, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			items = append(items, ci)
		}
		return items, true
	case []map[string]interface{}:
		return v, true
	default:
		return nil, false
	}
}
