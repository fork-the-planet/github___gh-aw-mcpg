package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestFilteredItem builds a FilteredItemDetail with the given map data, secrecy tags,
// integrity tags, description, and denial reason.  IsSecrecyViolation is inferred from
// whether secrecyTags is non-empty (matching how FilterCollection sets the field).
func newTestFilteredItem(data map[string]interface{}, description, reason string, secrecyTags, integrityTags []string) difc.FilteredItemDetail {
	labels := difc.NewLabeledResource(description)
	if len(secrecyTags) > 0 {
		labels.Secrecy = *difc.NewSecrecyLabel(difc.StringsToTags(secrecyTags)...)
	}
	if len(integrityTags) > 0 {
		labels.Integrity = *difc.NewIntegrityLabel(difc.StringsToTags(integrityTags)...)
	}
	return difc.FilteredItemDetail{
		Item: difc.LabeledItem{
			Data:   data,
			Labels: labels,
		},
		Reason:             reason,
		IsSecrecyViolation: len(secrecyTags) > 0,
	}
}

// newSecrecyFilteredItem builds a FilteredItemDetail explicitly marked as a secrecy violation.
func newSecrecyFilteredItem(description, reason string) difc.FilteredItemDetail {
	return difc.FilteredItemDetail{
		Item:               difc.LabeledItem{Labels: difc.NewLabeledResource(description)},
		Reason:             reason,
		IsSecrecyViolation: true,
	}
}

// newIntegrityFilteredItem builds a FilteredItemDetail explicitly marked as an integrity violation.
func newIntegrityFilteredItem(description, reason string) difc.FilteredItemDetail {
	return difc.FilteredItemDetail{
		Item:               difc.LabeledItem{Labels: difc.NewLabeledResource(description)},
		Reason:             reason,
		IsSecrecyViolation: false,
	}
}

// initTestLoggers initializes both the file logger and server file logger in tmpDir.
// It returns a cleanup function that closes both loggers.
func initTestLoggers(t *testing.T, tmpDir string) func() {
	t.Helper()
	err := logger.InitFileLogger(tmpDir, "mcp-gateway.log")
	require.NoError(t, err)
	err = logger.InitServerFileLogger(tmpDir)
	require.NoError(t, err)
	return func() {
		logger.CloseAllLoggers()
	}
}

// readLogLines reads a log file and returns all lines containing needle.
func readLogLines(t *testing.T, path, needle string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	var matched []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, needle) {
			matched = append(matched, line)
		}
	}
	return matched
}

// extractJSONFromDIFCLine extracts the JSON payload from a [DIFC-FILTERED] log line.
// The log format is: [timestamp] [INFO] [difc] [DIFC-FILTERED] <json>
func extractJSONFromDIFCLine(t *testing.T, line string) string {
	t.Helper()
	const marker = "[DIFC-FILTERED] "
	idx := strings.Index(line, marker)
	require.GreaterOrEqual(t, idx, 0, "line should contain [DIFC-FILTERED] marker: %s", line)
	return strings.TrimSpace(line[idx+len(marker):])
}

// TestLogFilteredItems_EmitsValidJSONWithExpectedFields verifies that logFilteredItems writes
// a [DIFC-FILTERED] prefixed log line containing valid JSON with the expected fields to
// both the per-server log and the unified mcp-gateway.log.
func TestLogFilteredItems_EmitsValidJSONWithExpectedFields(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := initTestLoggers(t, tmpDir)
	defer cleanup()

	item := newTestFilteredItem(
		map[string]interface{}{
			"html_url": "https://github.com/org/repo/issues/42",
			// JSON unmarshaling from interface{} represents numbers as float64.
			"number":             float64(42),
			"author_association": "CONTRIBUTOR",
			"user":               map[string]interface{}{"login": "alice"},
		},
		"issue:org/repo#42",
		"integrity too low",
		[]string{"private:org/repo"},
		[]string{"none"},
	)

	filtered := &difc.FilteredCollectionLabeledData{
		Filtered: []difc.FilteredItemDetail{item},
	}

	logFilteredItems("github", "list_issues", filtered)

	// Close loggers to flush all writes before reading.
	cleanup()

	// --- unified log ---
	unifiedLines := readLogLines(t, filepath.Join(tmpDir, "mcp-gateway.log"), "[DIFC-FILTERED]")
	require.Len(t, unifiedLines, 1, "unified log should have exactly one DIFC-FILTERED entry")

	jsonStr := extractJSONFromDIFCLine(t, unifiedLines[0])
	var entry logger.FilteredItemLogEntry
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &entry), "log entry must be valid JSON")

	assert.Equal(t, "github", entry.ServerID)
	assert.Equal(t, "list_issues", entry.ToolName)
	assert.Equal(t, "issue:org/repo#42", entry.Description)
	assert.Equal(t, "integrity too low", entry.Reason)
	assert.Equal(t, []string{"private:org/repo"}, entry.SecrecyTags)
	assert.Equal(t, []string{"none"}, entry.IntegrityTags)
	assert.Equal(t, "alice", entry.AuthorLogin)
	assert.Equal(t, "CONTRIBUTOR", entry.AuthorAssociation)
	assert.Equal(t, "https://github.com/org/repo/issues/42", entry.HTMLURL)
	assert.Equal(t, "42", entry.Number)

	// --- per-server log ---
	serverLines := readLogLines(t, filepath.Join(tmpDir, "github.log"), "[DIFC-FILTERED]")
	require.Len(t, serverLines, 1, "server log should have exactly one DIFC-FILTERED entry")

	serverJSONStr := extractJSONFromDIFCLine(t, serverLines[0])
	var serverEntry logger.FilteredItemLogEntry
	require.NoError(t, json.Unmarshal([]byte(serverJSONStr), &serverEntry), "server log entry must be valid JSON")
	assert.Equal(t, entry, serverEntry, "server log entry should match unified log entry")
}

// TestLogFilteredItems_MultipleItems verifies that each filtered item produces a separate
// log line with its own JSON entry.
func TestLogFilteredItems_MultipleItems(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := initTestLoggers(t, tmpDir)
	defer cleanup()

	filtered := &difc.FilteredCollectionLabeledData{
		Filtered: []difc.FilteredItemDetail{
			newTestFilteredItem(
				// float64 matches how JSON unmarshaling represents numbers via interface{}.
				map[string]interface{}{"number": float64(1), "html_url": "https://github.com/org/repo/issues/1"},
				"issue:org/repo#1", "secrecy mismatch",
				[]string{"private:org"}, []string{},
			),
			newTestFilteredItem(
				map[string]interface{}{"number": float64(2), "html_url": "https://github.com/org/repo/issues/2"},
				"issue:org/repo#2", "integrity too low",
				[]string{}, []string{"none"},
			),
		},
	}

	logFilteredItems("github", "list_issues", filtered)
	cleanup()

	lines := readLogLines(t, filepath.Join(tmpDir, "mcp-gateway.log"), "[DIFC-FILTERED]")
	require.Len(t, lines, 2, "one log line per filtered item")

	numbers := map[string]bool{}
	for _, line := range lines {
		jsonStr := extractJSONFromDIFCLine(t, line)
		var entry logger.FilteredItemLogEntry
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &entry))
		numbers[entry.Number] = true
	}
	assert.True(t, numbers["1"], "entry for item #1 should be present")
	assert.True(t, numbers["2"], "entry for item #2 should be present")
}

// TestLogFilteredItems_EmptyFiltered verifies that no log lines are emitted when there are
// no filtered items.
func TestLogFilteredItems_EmptyFiltered(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := initTestLoggers(t, tmpDir)
	defer cleanup()

	logFilteredItems("github", "list_issues", &difc.FilteredCollectionLabeledData{
		Filtered: []difc.FilteredItemDetail{},
	})
	cleanup()

	lines := readLogLines(t, filepath.Join(tmpDir, "mcp-gateway.log"), "[DIFC-FILTERED]")
	assert.Empty(t, lines, "no DIFC-FILTERED lines expected when filtered list is empty")
}

// TestBuildFilteredItemLogEntry_WithNilLabels verifies that buildFilteredItemLogEntry does
// not panic when the item has nil labels and still populates metadata from the raw data.
func TestBuildFilteredItemLogEntry_WithNilLabels(t *testing.T) {
	detail := difc.FilteredItemDetail{
		Item: difc.LabeledItem{
			Data: map[string]interface{}{
				"html_url": "https://github.com/org/repo/issues/7",
				"number":   float64(7),
			},
			Labels: nil,
		},
		Reason: "some reason",
	}

	entry := buildFilteredItemLogEntry("srv", "tool_name", detail)

	assert.Equal(t, "srv", entry.ServerID)
	assert.Equal(t, "tool_name", entry.ToolName)
	assert.Equal(t, "some reason", entry.Reason)
	assert.Empty(t, entry.Description)
	assert.Empty(t, entry.SecrecyTags)
	assert.Empty(t, entry.IntegrityTags)
	assert.Equal(t, "https://github.com/org/repo/issues/7", entry.HTMLURL)
	assert.Equal(t, "7", entry.Number)
}

// TestBuildFilteredItemLogEntry_ExtractAuthorLogin_UserObject verifies that the author
// login is extracted from the nested user.login field (issues / PRs).
func TestBuildFilteredItemLogEntry_ExtractAuthorLogin_UserObject(t *testing.T) {
	detail := newTestFilteredItem(
		map[string]interface{}{
			"user": map[string]interface{}{"login": "bob"},
		},
		"pr:org/repo#3", "secrecy tag missing from agent label", nil, nil,
	)

	entry := buildFilteredItemLogEntry("github", "list_prs", detail)
	assert.Equal(t, "bob", entry.AuthorLogin)
}

// TestBuildFilteredItemLogEntry_ExtractAuthorLogin_AuthorObject verifies that the author
// login is extracted from the nested author.login field (commits).
func TestBuildFilteredItemLogEntry_ExtractAuthorLogin_AuthorObject(t *testing.T) {
	detail := newTestFilteredItem(
		map[string]interface{}{
			"sha":    "abc123",
			"author": map[string]interface{}{"login": "carol"},
		},
		"commit:abc123", "integrity level below agent threshold", nil, nil,
	)

	entry := buildFilteredItemLogEntry("github", "list_commits", detail)
	assert.Equal(t, "carol", entry.AuthorLogin)
	assert.Equal(t, "abc123", entry.SHA)
}

// TestBuildFilteredItemLogEntry_ExtractNumberField_JsonNumber verifies that a
// json.Number value in the "number" field is correctly formatted.
func TestBuildFilteredItemLogEntry_ExtractNumberField_JsonNumber(t *testing.T) {
	detail := newTestFilteredItem(
		map[string]interface{}{
			"number": json.Number("999"),
		},
		"issue", "secrecy requirements not met by agent", nil, nil,
	)

	entry := buildFilteredItemLogEntry("github", "list_issues", detail)
	assert.Equal(t, "999", entry.Number)
}

func TestBuildFilteredItemLogEntry_AuthorLoginEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		data       map[string]interface{}
		wantAuthor string
	}{
		{
			name: "user empty login does not fall back to author",
			data: map[string]interface{}{
				"user":   map[string]interface{}{"login": ""},
				"author": map[string]interface{}{"login": "author-login"},
			},
			wantAuthor: "",
		},
		{
			name: "user non-string login falls back to author",
			data: map[string]interface{}{
				"user":   map[string]interface{}{"login": 42},
				"author": map[string]interface{}{"login": "author-login"},
			},
			wantAuthor: "author-login",
		},
		{
			name: "nil user falls back to author",
			data: map[string]interface{}{
				"user":   nil,
				"author": map[string]interface{}{"login": "author-login"},
			},
			wantAuthor: "author-login",
		},
		{
			name: "nested author user login is ignored",
			data: map[string]interface{}{
				"author": map[string]interface{}{
					"user": map[string]interface{}{"login": "nested-login"},
				},
			},
			wantAuthor: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := newTestFilteredItem(tt.data, "item", "reason", nil, nil)
			entry := buildFilteredItemLogEntry("github", "list_items", detail)
			assert.Equal(t, tt.wantAuthor, entry.AuthorLogin)
		})
	}
}

func TestBuildFilteredItemLogEntry_NumberEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		data       map[string]interface{}
		wantNumber string
	}{
		{name: "zero float64", data: map[string]interface{}{"number": float64(0)}, wantNumber: "0"},
		{name: "decimal float64 returns empty", data: map[string]interface{}{"number": float64(123.9)}, wantNumber: ""},
		{name: "nil number returns empty", data: map[string]interface{}{"number": nil}, wantNumber: ""},
		{name: "int number returns empty", data: map[string]interface{}{"number": 42}, wantNumber: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := newTestFilteredItem(tt.data, "item", "reason", nil, nil)
			entry := buildFilteredItemLogEntry("github", "list_items", detail)
			assert.Equal(t, tt.wantNumber, entry.Number)
		})
	}
}

// TestBuildFilteredItemLogEntry_NonMapData verifies that buildFilteredItemLogEntry
// handles non-map item data gracefully (no metadata extraction, no panic).
func TestBuildFilteredItemLogEntry_NonMapData(t *testing.T) {
	detail := difc.FilteredItemDetail{
		Item: difc.LabeledItem{
			Data:   "raw string data",
			Labels: difc.NewLabeledResource("description"),
		},
		Reason: "integrity too low for agent context",
	}

	assert.NotPanics(t, func() {
		entry := buildFilteredItemLogEntry("github", "tool", detail)
		assert.Empty(t, entry.HTMLURL)
		assert.Empty(t, entry.Number)
		assert.Empty(t, entry.AuthorLogin)
	})
}

// TestBuildDIFCFilteredNotice_NilInput verifies that a nil input returns an empty string
// without panicking.
func TestBuildDIFCFilteredNotice_NilInput(t *testing.T) {
	assert.NotPanics(t, func() {
		assert.Empty(t, buildDIFCFilteredNotice(nil))
	})
}

// TestBuildDIFCFilteredNotice_EmptyFiltered verifies that no notice is returned when
// there are no filtered items.
func TestBuildDIFCFilteredNotice_EmptyFiltered(t *testing.T) {
	f := &difc.FilteredCollectionLabeledData{
		Filtered: []difc.FilteredItemDetail{},
	}
	assert.Empty(t, buildDIFCFilteredNotice(f))
}

// TestBuildDIFCFilteredNotice_SingleItem verifies the notice for a single filtered item
// includes the item description and reason.
func TestBuildDIFCFilteredNotice_SingleItem(t *testing.T) {
	f := &difc.FilteredCollectionLabeledData{
		Filtered: []difc.FilteredItemDetail{
			newTestFilteredItem(nil, "issue:org/repo#14", "integrity too low for agent context", nil, nil),
		},
		TotalCount: 1,
	}

	notice := buildDIFCFilteredNotice(f)

	assert.NotEmpty(t, notice)
	assert.Contains(t, notice, "[Filtered]")
	assert.Contains(t, notice, "1 item(s)")
	assert.Contains(t, notice, "issue:org/repo#14")
	assert.Contains(t, notice, "integrity too low for agent context")
}

// TestBuildDIFCFilteredNotice_MultipleItemsWithinLimit verifies that up to
// maxFilteredItemsInNotice items are listed individually with their descriptions and reasons.
func TestBuildDIFCFilteredNotice_MultipleItemsWithinLimit(t *testing.T) {
	f := &difc.FilteredCollectionLabeledData{
		Filtered: []difc.FilteredItemDetail{
			newTestFilteredItem(nil, "issue:org/repo#1", "integrity too low", nil, nil),
			newTestFilteredItem(nil, "issue:org/repo#2", "integrity too low", nil, nil),
			newTestFilteredItem(nil, "issue:org/repo#3", "integrity too low", nil, nil),
		},
		TotalCount: 3,
	}

	notice := buildDIFCFilteredNotice(f)

	assert.NotEmpty(t, notice)
	assert.Contains(t, notice, "[Filtered]")
	assert.Contains(t, notice, "3 item(s)")
	assert.Contains(t, notice, "issue:org/repo#1")
	assert.Contains(t, notice, "issue:org/repo#2")
	assert.Contains(t, notice, "issue:org/repo#3")
}

// TestBuildDIFCFilteredNotice_ExceedsLimit verifies that when more than
// maxFilteredItemsInNotice items are filtered, only the count is reported.
func TestBuildDIFCFilteredNotice_ExceedsLimit(t *testing.T) {
	items := make([]difc.FilteredItemDetail, maxFilteredItemsInNotice+1)
	for i := range items {
		items[i] = newTestFilteredItem(nil, fmt.Sprintf("issue:org/repo#%d", i+1), "integrity too low", nil, nil)
	}
	f := &difc.FilteredCollectionLabeledData{
		Filtered:   items,
		TotalCount: len(items),
	}

	notice := buildDIFCFilteredNotice(f)

	assert.NotEmpty(t, notice)
	assert.Contains(t, notice, "[Filtered]")
	assert.Contains(t, notice, fmt.Sprintf("%d item(s)", len(items)))
	// Individual descriptions should NOT appear when the count exceeds the limit.
	assert.NotContains(t, notice, "issue:org/repo#1")
}

// TestBuildDIFCFilteredNotice_ItemWithNoDescription verifies that items without
// a description still produce a valid count-only notice.
func TestBuildDIFCFilteredNotice_ItemWithNoDescription(t *testing.T) {
	f := &difc.FilteredCollectionLabeledData{
		Filtered: []difc.FilteredItemDetail{
			{
				Item:   difc.LabeledItem{Data: "raw", Labels: difc.NewLabeledResource("")},
				Reason: "",
			},
		},
		TotalCount: 1,
	}

	notice := buildDIFCFilteredNotice(f)

	assert.NotEmpty(t, notice)
	assert.Contains(t, notice, "[Filtered]")
	assert.Contains(t, notice, "1 item(s)")
}

// TestBuildDIFCFilteredNotice_SecrecyViolation verifies that secrecy-blocked items
// produce a notice that says "secrecy policy", not "integrity policy".
func TestBuildDIFCFilteredNotice_SecrecyViolation(t *testing.T) {
	f := &difc.FilteredCollectionLabeledData{
		Filtered: []difc.FilteredItemDetail{
			newSecrecyFilteredItem("resource:actions_get", "has secrecy requirements that agent doesn't meet"),
		},
		TotalCount: 1,
	}

	notice := buildDIFCFilteredNotice(f)

	assert.NotEmpty(t, notice)
	assert.Contains(t, notice, "[Filtered]")
	assert.Contains(t, notice, "1 item(s)")
	assert.Contains(t, notice, "secrecy policy")
	assert.NotContains(t, notice, "integrity policy")
	assert.Contains(t, notice, "resource:actions_get")
}

// TestBuildDIFCFilteredNotice_IntegrityViolation verifies that integrity-blocked items
// produce a notice that says "integrity policy".
func TestBuildDIFCFilteredNotice_IntegrityViolation(t *testing.T) {
	f := &difc.FilteredCollectionLabeledData{
		Filtered: []difc.FilteredItemDetail{
			newIntegrityFilteredItem("issue:org/repo#14", "integrity too low for agent context"),
		},
		TotalCount: 1,
	}

	notice := buildDIFCFilteredNotice(f)

	assert.Contains(t, notice, "integrity policy")
	assert.NotContains(t, notice, "secrecy policy")
}

// TestBuildDIFCFilteredNotice_MixedViolations verifies that a mix of secrecy and
// integrity blocks produces a notice that says "access policy".
func TestBuildDIFCFilteredNotice_MixedViolations(t *testing.T) {
	f := &difc.FilteredCollectionLabeledData{
		Filtered: []difc.FilteredItemDetail{
			newSecrecyFilteredItem("resource:actions_get", "secrecy mismatch"),
			newIntegrityFilteredItem("issue:org/repo#1", "integrity too low"),
		},
		TotalCount: 2,
	}

	notice := buildDIFCFilteredNotice(f)

	assert.Contains(t, notice, "[Filtered]")
	assert.Contains(t, notice, "2 item(s)")
	assert.Contains(t, notice, "access policy")
	assert.NotContains(t, notice, "integrity policy")
	assert.NotContains(t, notice, "secrecy policy")
}

// TestDifcPolicyLabel verifies the policy label selection logic.
func TestDifcPolicyLabel(t *testing.T) {
	tests := []struct {
		name     string
		items    []difc.FilteredItemDetail
		expected string
	}{
		{
			name:     "all secrecy violations",
			items:    []difc.FilteredItemDetail{{IsSecrecyViolation: true}, {IsSecrecyViolation: true}},
			expected: "secrecy policy",
		},
		{
			name:     "all integrity violations",
			items:    []difc.FilteredItemDetail{{IsSecrecyViolation: false}, {IsSecrecyViolation: false}},
			expected: "integrity policy",
		},
		{
			name:     "mixed violations",
			items:    []difc.FilteredItemDetail{{IsSecrecyViolation: true}, {IsSecrecyViolation: false}},
			expected: "access policy",
		},
		{
			name:     "empty items defaults to access policy",
			items:    []difc.FilteredItemDetail{},
			expected: "access policy",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, difcPolicyLabel(tc.items))
		})
	}
}

// TestBuildDIFCSingleItemFilteredError_IntegrityViolation verifies that an integrity
// violation produces an error message containing [Filtered], the resource description,
// and the denial reason.
func TestBuildDIFCSingleItemFilteredError_IntegrityViolation(t *testing.T) {
	detail := newIntegrityFilteredItem("issue:org/repo#42", "integrity too low for agent context")

	err := buildDIFCSingleItemFilteredError(detail)

	require.Error(t, err)
	assert.ErrorContains(t, err, "[Filtered]")
	assert.ErrorContains(t, err, "issue:org/repo#42")
	assert.ErrorContains(t, err, "integrity policy")
	assert.ErrorContains(t, err, "integrity too low for agent context")
}

// TestBuildDIFCSingleItemFilteredError_SecrecyViolation verifies that a secrecy
// violation produces an error message containing "secrecy policy".
func TestBuildDIFCSingleItemFilteredError_SecrecyViolation(t *testing.T) {
	detail := newSecrecyFilteredItem("resource:actions_get", "secrecy requirements not met")

	err := buildDIFCSingleItemFilteredError(detail)

	require.Error(t, err)
	assert.ErrorContains(t, err, "[Filtered]")
	assert.ErrorContains(t, err, "resource:actions_get")
	assert.ErrorContains(t, err, "secrecy policy")
	assert.ErrorContains(t, err, "secrecy requirements not met")
}

// TestBuildDIFCSingleItemFilteredError_NoDescription verifies that a missing description
// falls back to a generic "resource" label and still produces a valid error.
func TestBuildDIFCSingleItemFilteredError_NoDescription(t *testing.T) {
	detail := difc.FilteredItemDetail{
		Item:               difc.LabeledItem{Labels: difc.NewLabeledResource("")},
		Reason:             "integrity too low",
		IsSecrecyViolation: false,
	}

	err := buildDIFCSingleItemFilteredError(detail)

	require.Error(t, err)
	assert.ErrorContains(t, err, "[Filtered]")
	assert.ErrorContains(t, err, "resource exists but is not accessible")
	assert.ErrorContains(t, err, "integrity policy")
}

// TestBuildDIFCSingleItemFilteredError_NoReason verifies that a missing reason still
// produces a valid error that is not blank.
func TestBuildDIFCSingleItemFilteredError_NoReason(t *testing.T) {
	detail := newIntegrityFilteredItem("issue:org/repo#7", "")

	err := buildDIFCSingleItemFilteredError(detail)

	require.Error(t, err)
	assert.ErrorContains(t, err, "[Filtered]")
	assert.ErrorContains(t, err, "issue:org/repo#7")
	// No trailing "()" should appear when reason is empty.
	assert.NotContains(t, err.Error(), "()")
}

// TestIsSingularReadTool verifies the heuristic that distinguishes singular-read tools
// (get_*, *_read) from collection tools (list_*, search_*).
func TestIsSingularReadTool(t *testing.T) {
	tests := []struct {
		toolName string
		singular bool
	}{
		{"issue_read", true},
		{"pull_request_read", true},
		{"get_issue", true},
		{"get_pull_request", true},
		{"get_file_contents", true},
		{"get_repository", true},
		{"get_commit", true},
		{"list_issues", false},
		{"list_pull_requests", false},
		{"list_commits", false},
		{"list_branches", false},
		{"search_issues", false},
		{"search_pull_requests", false},
		{"search_code", false},
		{"search_repositories", false},
	}
	for _, tc := range tests {
		t.Run(tc.toolName, func(t *testing.T) {
			assert.Equal(t, tc.singular, difc.IsSingularReadTool(tc.toolName),
				"difc.IsSingularReadTool(%q) should be %v", tc.toolName, tc.singular)
		})
	}
}
