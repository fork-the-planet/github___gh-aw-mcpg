package mcpresult

import "strings"

// ExtractTextContent returns the concatenated text from text content items in a
// raw MCP tool result map. Content items with a missing "type" are treated as
// text items for compatibility with older callers and tests.
func ExtractTextContent(result map[string]interface{}) string {
	contentVal, hasContent := result["content"]
	if !hasContent || contentVal == nil {
		return ""
	}

	var items []map[string]interface{}
	switch v := contentVal.(type) {
	case []interface{}:
		items = make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			ci, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			items = append(items, ci)
		}
	case []map[string]interface{}:
		items = v
	default:
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
