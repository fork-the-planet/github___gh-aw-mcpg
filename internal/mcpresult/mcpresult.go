package mcpresult

import "strings"

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

// ExtractTextContent returns the concatenated text from text content items in a
// raw MCP tool result map. Content items with a missing "type" are treated as
// text items for compatibility with older callers and tests.
func ExtractTextContent(result map[string]interface{}) string {
	contentVal, hasContent := result["content"]
	if !hasContent || contentVal == nil {
		return ""
	}

	items, ok := NormalizeContentItems(contentVal)
	if !ok {
		return ""
	}

	var text strings.Builder
	for _, item := range items {
		itemType, _ := item["type"].(string)
		switch itemType {
		case "", "text":
			// keep
		case "image", "audio", "resource":
			continue
		default:
			// Unknown types are treated as text for compatibility with ConvertToCallToolResult.
		}
		itemText, _ := item["text"].(string)
		if itemText == "" {
			continue
		}
		text.WriteString(itemText)
	}

	return text.String()
}
