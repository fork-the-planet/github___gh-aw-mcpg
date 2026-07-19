package mcpresult

import (
	"strings"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logMCPResult = logger.ForFile()

// NormalizeContentItems normalizes an MCP "content" field into a slice of item
// maps. It supports both []interface{} values produced by json.Unmarshal and
// []map[string]interface{} values produced by helper constructors.
//
// Non-map items in []interface{} are skipped so callers can decide whether to
// ignore them or treat them as an error.
func NormalizeContentItems(contentVal interface{}) ([]map[string]interface{}, bool) {
	logMCPResult.Printf("Normalizing MCP content items from type %T", contentVal)

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
		logMCPResult.Printf("Normalized []interface{} content: input=%d output=%d", len(v), len(items))
		return items, true
	case []map[string]interface{}:
		logMCPResult.Printf("Normalized []map content: items=%d", len(v))
		return v, true
	default:
		logMCPResult.Printf("Unsupported MCP content type for normalization: %T", contentVal)
		return nil, false
	}
}

// ExtractTextContent returns the concatenated text from text content items in a
// raw MCP tool result map. Content items with a missing "type" are treated as
// text items for compatibility with older callers and tests.
func ExtractTextContent(result map[string]interface{}) string {
	logMCPResult.Printf("Extracting text content from result map size=%d", len(result))

	contentVal, hasContent := result["content"]
	if !hasContent || contentVal == nil {
		logMCPResult.Print("No MCP content field available for text extraction")
		return ""
	}

	items, ok := NormalizeContentItems(contentVal)
	if !ok {
		logMCPResult.Print("Skipping text extraction because MCP content normalization failed")
		return ""
	}

	logMCPResult.Printf("Processing %d normalized MCP content items for text extraction", len(items))

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

	logMCPResult.Printf("Finished text extraction with output length=%d", text.Len())

	return text.String()
}
