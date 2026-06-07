package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/strutil"
)

var logDifcLog = logger.New("server:difc_log")

// logFilteredItems logs structured details for every item removed by DIFC filtering.
// Each item is written as a [DIFC-FILTERED] JSON entry to both the unified and
// per-server text log files (via LogInfoToServer), and as a difc_filtered event
// in the JSONL log.
func logFilteredItems(serverID, toolName string, filtered *difc.FilteredCollectionLabeledData) {
	logDifcLog.Printf("Logging filtered items: serverID=%s, toolName=%s, count=%d", serverID, toolName, len(filtered.Filtered))
	for _, detail := range filtered.Filtered {
		entry := buildFilteredItemLogEntry(serverID, toolName, detail)
		b, err := json.Marshal(entry)
		if err != nil {
			logger.LogInfoToServer(serverID, "difc", "Failed to marshal filtered item log entry: %v", err)
			continue
		}
		jsonStr := string(b)
		logger.LogInfoToServer(serverID, "difc", "[DIFC-FILTERED] %s", jsonStr)
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
		logDifcLog.Printf("Filtered item labels: description=%s, secrecy=%v, integrity=%v", entry.Description, entry.SecrecyTags, entry.IntegrityTags)
	}

	// Extract identifying metadata from the raw item data.
	// Data is interface{} from JSON parsing — typically map[string]interface{}.
	if m, ok := detail.Item.Data.(map[string]interface{}); ok {
		entry.AuthorAssociation = strutil.GetStringFromMap(m, "author_association", "authorAssociation")
		entry.AuthorLogin = extractAuthorLogin(m)
		entry.HTMLURL = strutil.GetStringFromMap(m, "html_url", "htmlUrl")
		entry.Number = extractNumberField(m)
		entry.SHA = strutil.GetStringFromMap(m, "sha")
		logDifcLog.Printf("Filtered item metadata: author=%s, number=%s, url=%s", entry.AuthorLogin, entry.Number, entry.HTMLURL)
	}

	return entry
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

// maxFilteredItemsInNotice is the maximum number of individual item descriptions
// to include inline in the DIFC filtered notice surfaced to the agent.
const maxFilteredItemsInNotice = 5

// buildDIFCSingleItemFilteredError constructs an error for when exactly one item is
// entirely blocked by DIFC policy.  Unlike the notice approach (which appends a text
// annotation to a partial or empty list), this returns an actual Go error that the caller
// can surface as an MCP IsError result.  It prevents agents from misinterpreting a
// "filtered" single-item response (e.g. issue_read) as "resource not found".
func buildDIFCSingleItemFilteredError(detail difc.FilteredItemDetail) error {
	policyLabel := difcPolicyLabel([]difc.FilteredItemDetail{detail})

	desc := ""
	if detail.Item.Labels != nil {
		desc = detail.Item.Labels.Description
	}

	var msg string
	if desc != "" {
		msg = fmt.Sprintf("[Filtered] %s exists but is not accessible — filtered by %s", desc, policyLabel)
	} else {
		msg = fmt.Sprintf("[Filtered] resource exists but is not accessible — filtered by %s", policyLabel)
	}
	if detail.Reason != "" {
		msg = fmt.Sprintf("%s (%s)", msg, detail.Reason)
	}
	return fmt.Errorf("%s", msg)
}

// buildDIFCFilteredNotice builds a human-readable notice for the agent when items are
// removed from a tool response by DIFC policy in filter/propagate mode.
//
// The notice is surfaced as an additional text content block appended to the tool
// response so that agents (and targeted-dispatch workflows) are aware that items exist
// but were withheld, rather than concluding the result set is genuinely empty.
//
// The notice distinguishes between secrecy-blocked and integrity-blocked items so that
// downstream consumers can provide accurate guidance (e.g. secrecy violations cannot be
// resolved by lowering min-integrity).
//
// For up to maxFilteredItemsInNotice items the description and reason for each item are
// included. For larger sets only the count is reported to keep the message concise.
func buildDIFCFilteredNotice(filtered *difc.FilteredCollectionLabeledData) string {
	if filtered == nil {
		return ""
	}
	n := filtered.GetFilteredCount()
	if n == 0 {
		return ""
	}

	logDifcLog.Printf("Building DIFC filtered notice: filteredCount=%d, maxInline=%d", n, maxFilteredItemsInNotice)

	// Determine the policy label: distinguish secrecy-only, integrity-only, or mixed.
	policyLabel := difcPolicyLabel(filtered.Filtered)

	// For a small number of filtered items, include per-item descriptions and reasons.
	if n <= maxFilteredItemsInNotice {
		logDifcLog.Printf("Using per-item notice format for %d item(s)", n)
		parts := make([]string, 0, n)
		for _, detail := range filtered.Filtered {
			desc := ""
			if detail.Item.Labels != nil {
				desc = detail.Item.Labels.Description
			}
			// Skip items that carry no useful identifying information.
			if desc == "" && detail.Reason == "" {
				continue
			}
			if desc != "" && detail.Reason != "" {
				parts = append(parts, fmt.Sprintf("%s (%s)", desc, detail.Reason))
			} else if desc != "" {
				parts = append(parts, desc)
			} else {
				parts = append(parts, detail.Reason)
			}
		}
		if len(parts) > 0 {
			return fmt.Sprintf(
				"[Filtered] %d item(s) in this response were removed by %s and are not shown: %s.",
				n, policyLabel, strings.Join(parts, "; "),
			)
		}
	}

	return fmt.Sprintf(
		"[Filtered] %d item(s) in this response were removed by %s and are not shown.",
		n, policyLabel,
	)
}

// difcPolicyLabel returns a human-readable policy label based on whether the filtered
// items were blocked due to secrecy, integrity, or a mix of both.
func difcPolicyLabel(items []difc.FilteredItemDetail) string {
	secrecyCount, integrityCount := 0, 0
	for _, d := range items {
		if d.IsSecrecyViolation {
			secrecyCount++
		} else {
			integrityCount++
		}
	}
	switch {
	case secrecyCount > 0 && integrityCount == 0:
		return "secrecy policy"
	case integrityCount > 0 && secrecyCount == 0:
		return "integrity policy"
	default:
		return "access policy"
	}
}
