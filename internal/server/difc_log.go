package server

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

// logFilteredItems logs structured details for every item removed by DIFC filtering.
// Each item is written as a [DIFC-FILTERED] JSON entry to both the unified and
// per-server text log files (via LogInfoWithServer), and as a DIFC_FILTERED entry
// in the JSONL log.
func logFilteredItems(serverID, toolName string, filtered *difc.FilteredCollectionLabeledData) {
	for _, detail := range filtered.Filtered {
		entry := buildFilteredItemLogEntry(serverID, toolName, detail)
		b, err := json.Marshal(entry)
		if err != nil {
			logger.LogInfoWithServer(serverID, "difc", "Failed to marshal filtered item log entry: %v", err)
			continue
		}
		jsonStr := string(b)
		logger.LogInfoWithServer(serverID, "difc", "[DIFC-FILTERED] %s", jsonStr)
		logger.LogDifcFilteredItem(&logger.JSONLFilteredItem{FilteredItemLogEntry: entry})
	}
}

// buildFilteredItemLogEntry constructs a logger.FilteredItemLogEntry from a filtered item.
func buildFilteredItemLogEntry(serverID, toolName string, detail difc.FilteredItemDetail) logger.FilteredItemLogEntry {
	entry := logger.FilteredItemLogEntry{
		ServerID: serverID,
		ToolName: toolName,
		Reason:   detail.Reason,
	}

	if detail.Item.Labels != nil {
		entry.Description = detail.Item.Labels.Description
		entry.SecrecyTags = difc.TagsToStrings(detail.Item.Labels.Secrecy.Label.GetTags())
		entry.IntegrityTags = difc.TagsToStrings(detail.Item.Labels.Integrity.Label.GetTags())
	}

	// Extract identifying metadata from the raw item data.
	// Data is interface{} from JSON parsing — typically map[string]interface{}.
	if m, ok := detail.Item.Data.(map[string]interface{}); ok {
		entry.AuthorAssociation = getStringField(m, "author_association", "authorAssociation")
		entry.AuthorLogin = extractAuthorLogin(m)
		entry.HTMLURL = getStringField(m, "html_url", "htmlUrl")
		entry.Number = extractNumberField(m)
		entry.SHA = getStringField(m, "sha")
	}

	return entry
}

// getStringField returns the first non-empty string value from the map
// matching any of the given field names.
func getStringField(m map[string]interface{}, fields ...string) string {
	for _, f := range fields {
		if v, ok := m[f]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// extractAuthorLogin extracts the author login from nested user/author objects.
func extractAuthorLogin(m map[string]interface{}) string {
	if user, ok := m["user"].(map[string]interface{}); ok {
		if login, ok := user["login"].(string); ok {
			return login
		}
	}
	if author, ok := m["author"].(map[string]interface{}); ok {
		if login, ok := author["login"].(string); ok {
			return login
		}
	}
	return ""
}

// extractNumberField extracts the item number as a string.
func extractNumberField(m map[string]interface{}) string {
	if n, ok := m["number"]; ok {
		switch v := n.(type) {
		case float64:
			return fmt.Sprintf("%d", int64(v))
		case json.Number:
			return v.String()
		}
	}
	return ""
}
