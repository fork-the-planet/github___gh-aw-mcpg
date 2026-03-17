package server

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

// FilteredItemLogEntry is a structured log entry for a DIFC-filtered item.
// It captures enough context to identify the object, understand why it was
// filtered, and link back to it in a post-processing report.
type FilteredItemLogEntry struct {
	ServerID          string   `json:"server_id"`
	ToolName          string   `json:"tool_name"`
	Description       string   `json:"description"`
	Reason            string   `json:"reason"`
	SecrecyTags       []string `json:"secrecy_tags"`
	IntegrityTags     []string `json:"integrity_tags"`
	AuthorAssociation string   `json:"author_association,omitempty"`
	AuthorLogin       string   `json:"author_login,omitempty"`
	HTMLURL           string   `json:"html_url,omitempty"`
	Number            string   `json:"number,omitempty"`
	SHA               string   `json:"sha,omitempty"`
}

// logFilteredItems logs structured details for every item removed by DIFC filtering.
// Each item is logged individually to the configured logger outputs so that
// post-processing tools can reconstruct exactly what was filtered and why.
func logFilteredItems(serverID, toolName string, filtered *difc.FilteredCollectionLabeledData) {
	for _, detail := range filtered.Filtered {
		entry := buildFilteredItemLogEntry(serverID, toolName, detail)

		entryJSON, err := json.Marshal(entry)
		if err != nil {
			logger.LogWarnWithServer(serverID, "difc",
				"[DIFC-FILTERED] %s | %s | description=%s | reason=%s (json marshal failed: %v)",
				serverID, toolName, entry.Description, entry.Reason, err)
			continue
		}

		logger.LogInfoWithServer(serverID, "difc",
			"[DIFC-FILTERED] %s", string(entryJSON))
	}
}

// buildFilteredItemLogEntry constructs a structured log entry from a filtered item.
func buildFilteredItemLogEntry(serverID, toolName string, detail difc.FilteredItemDetail) FilteredItemLogEntry {
	entry := FilteredItemLogEntry{
		ServerID: serverID,
		ToolName: toolName,
		Reason:   detail.Reason,
	}

	if detail.Item.Labels != nil {
		entry.Description = detail.Item.Labels.Description
		entry.SecrecyTags = tagsToStrings(detail.Item.Labels.Secrecy.Label.GetTags())
		entry.IntegrityTags = tagsToStrings(detail.Item.Labels.Integrity.Label.GetTags())
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

// tagsToStrings converts DIFC tags to string slice.
func tagsToStrings(tags []difc.Tag) []string {
	s := make([]string, len(tags))
	for i, t := range tags {
		s[i] = string(t)
	}
	return s
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
	// Try user.login (issues, PRs)
	if user, ok := m["user"].(map[string]interface{}); ok {
		if login, ok := user["login"].(string); ok {
			return login
		}
	}
	// Try author.login (commits)
	if author, ok := m["author"].(map[string]interface{}); ok {
		if login, ok := author["login"].(string); ok {
			return login
		}
	}
	return ""
}

// extractNumberField extracts the item number as a string.
// GitHub API returns numbers as float64 from JSON parsing.
func extractNumberField(m map[string]interface{}) string {
	if n, ok := m["number"]; ok {
		switch v := n.(type) {
		case float64:
			return fmt.Sprintf("%d", int(v))
		case json.Number:
			return v.String()
		}
	}
	return ""
}
