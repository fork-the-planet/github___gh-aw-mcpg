package middleware

// Tests targeting previously uncovered branches in jqschema.go:
//   - inferSchema default case (custom types via reflect)
//   - savePayload error paths (unwritable directories)
//   - WrapToolHandler with custom struct data (triggers default branch in type switch)
//   - WrapToolHandler when savePayload fails (continues and returns metadata on save failure)

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// inferSchema default case (lines 104-115 in jqschema.go)
// ---------------------------------------------------------------------------

// customIntType has kind reflect.Int32, which is NOT listed in the explicit
// type-switch cases (those only list the untyped int/int32 etc. kinds), so it
// falls through to the default branch and is classified via reflect.
type customIntType int32

// customUintType similarly exercises the uint reflect path.
type customUintType uint64

// customFloatType exercises the float reflect path.
type customFloatType float32

// myStruct is an arbitrary struct that falls through to the inner "string" default.
type myStruct struct {
	X int
	Y string
}

func TestInferSchema_DefaultCaseFallback(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "custom named int type returns number",
			input:    customIntType(42),
			expected: "number",
		},
		{
			name:     "custom named uint type returns number",
			input:    customUintType(100),
			expected: "number",
		},
		{
			name:     "custom named float type returns number",
			input:    customFloatType(3.14),
			expected: "number",
		},
		{
			name:     "struct type returns string",
			input:    myStruct{X: 1, Y: "hello"},
			expected: "string",
		},
		{
			name:     "chan type returns string",
			input:    make(chan int),
			expected: "string",
		},
		{
			// []int is NOT []interface{} so it falls through to default.
			name:     "typed int slice returns string",
			input:    []int{1, 2, 3},
			expected: "string",
		},
		{
			// map[string]string is NOT map[string]interface{} so it falls through.
			name:     "typed string map returns string",
			input:    map[string]string{"key": "value"},
			expected: "string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferSchema(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// TestInferSchema_DefaultCaseNestedInMap exercises the default branch when a
// custom type appears as a value inside a map[string]interface{}.
func TestInferSchema_DefaultCaseNestedInMap(t *testing.T) {
	input := map[string]interface{}{
		"normal":  float64(1),
		"custom":  customIntType(99),
		"strType": []int{1, 2, 3},
	}
	got := inferSchema(input)
	schema, ok := got.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "number", schema["normal"])
	assert.Equal(t, "number", schema["custom"])  // customIntType → reflect.Int32 → "number"
	assert.Equal(t, "string", schema["strType"]) // []int → reflect.Slice → "string"
}

// ---------------------------------------------------------------------------
// savePayload error paths (lines 231-256 in jqschema.go)
// ---------------------------------------------------------------------------

// TestSavePayload_MkdirAllFailure covers the os.MkdirAll error path by using a
// base directory whose parent is read-only (preventing subdirectory creation).
func TestSavePayload_MkdirAllFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission model differs on Windows")
	}

	// Create a temporary directory and make it read-only so that MkdirAll
	// cannot create any subdirectories inside it.
	readOnlyDir := t.TempDir()
	require.NoError(t, os.Chmod(readOnlyDir, 0444))
	t.Cleanup(func() {
		// Restore permissions so t.TempDir cleanup can remove the directory.
		_ = os.Chmod(readOnlyDir, 0755)
	})

	// Skip the test when running as root (root can write to read-only dirs).
	if os.Getuid() == 0 {
		t.Skip("skipping read-only directory test when running as root")
	}

	_, err := savePayload(readOnlyDir, "", "session1", "query1", []byte(`{"key":"value"}`))
	require.Error(t, err, "savePayload should return an error when MkdirAll fails")
	assert.Contains(t, err.Error(), "failed to create payload directory")
}

// TestSavePayload_WriteFileFailure covers the os.WriteFile error path by using a
// directory path that resolves to an existing file (so the path cannot be a dir).
func TestSavePayload_WriteFileFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission model differs on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping file-permission test when running as root")
	}

	// Create the target directory structure that savePayload would create,
	// then place a read-only file at the exact payload path.
	baseDir := t.TempDir()
	sessionID := "test-session"
	queryID := "test-query"
	payloadDir := filepath.Join(baseDir, sessionID, queryID)
	require.NoError(t, os.MkdirAll(payloadDir, 0755))

	// Write a read-only file at the exact location savePayload would write.
	payloadFile := filepath.Join(payloadDir, "payload.json")
	require.NoError(t, os.WriteFile(payloadFile, []byte("existing"), 0444))
	t.Cleanup(func() { _ = os.Chmod(payloadFile, 0644) })

	_, err := savePayload(baseDir, "", sessionID, queryID, []byte(`{"key":"value"}`))
	// On Linux, writing to a 0444 file returns "permission denied"
	require.Error(t, err, "savePayload should return an error when WriteFile fails")
	assert.Contains(t, err.Error(), "failed to write payload file")
}

// ---------------------------------------------------------------------------
// WrapToolHandler with custom struct data (default branch in type switch)
// ---------------------------------------------------------------------------

// TestWrapToolHandler_CustomStructData exercises the default arm of the data
// type switch inside WrapToolHandler (lines 388-391 in jqschema.go).  A plain
// Go struct is returned as the data value so that none of the explicit cases
// (map[string]interface{}, []interface{}, string, float64, bool, nil,
// json.Number) match.
func TestWrapToolHandler_CustomStructData(t *testing.T) {
	baseDir := t.TempDir()

	type ToolOutput struct {
		Count int    `json:"count"`
		Label string `json:"label"`
	}

	// Return a custom struct as data.
	customData := ToolOutput{Count: 42, Label: "hello"}
	mockHandler := func(ctx context.Context, req *sdk.CallToolRequest, args interface{}) (*sdk.CallToolResult, interface{}, error) {
		return &sdk.CallToolResult{IsError: false}, customData, nil
	}

	// Use threshold 0 so the large-payload path is always taken.
	wrapped := WrapToolHandler(mockHandler, "test_tool", baseDir, "", 0, testGetSessionID)
	result, _, err := wrapped(context.Background(), &sdk.CallToolRequest{}, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)

	// Verify the response is a metadata object (large-payload path taken).
	require.NotEmpty(t, result.Content)
	textContent, ok := result.Content[0].(*sdk.TextContent)
	require.True(t, ok)

	var meta map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textContent.Text), &meta))
	// payloadSchema for {"count": 42, "label": "hello"} should be a map with "number" and "string".
	schema, ok := meta["payloadSchema"].(map[string]interface{})
	require.True(t, ok, "payloadSchema should be an object, got: %T %v", meta["payloadSchema"], meta["payloadSchema"])
	assert.Equal(t, "number", schema["count"])
	assert.Equal(t, "string", schema["label"])
}

// ---------------------------------------------------------------------------
// WrapToolHandler when savePayload fails
// (lines 358-365: save error is logged but processing continues)
// ---------------------------------------------------------------------------

// TestWrapToolHandler_SavePayloadFailure verifies that when savePayload cannot
// write the file (e.g. unwritable base directory), WrapToolHandler logs the
// error but still returns a metadata response (schema + preview) rather than
// propagating the failure to the caller.
func TestWrapToolHandler_SavePayloadFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission model differs on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping read-only directory test when running as root")
	}

	// Make baseDir read-only so that MkdirAll cannot create subdirectories.
	baseDir := t.TempDir()
	require.NoError(t, os.Chmod(baseDir, 0444))
	t.Cleanup(func() { _ = os.Chmod(baseDir, 0755) })

	largeData := map[string]interface{}{
		"items": make([]interface{}, 100),
	}
	for i := range largeData["items"].([]interface{}) {
		largeData["items"].([]interface{})[i] = map[string]interface{}{"id": float64(i)}
	}

	mockHandler := func(ctx context.Context, req *sdk.CallToolRequest, args interface{}) (*sdk.CallToolResult, interface{}, error) {
		return &sdk.CallToolResult{IsError: false}, largeData, nil
	}

	// Threshold of 1 forces large-payload path.
	wrapped := WrapToolHandler(mockHandler, "test_tool", baseDir, "", 1, testGetSessionID)
	result, _, err := wrapped(context.Background(), &sdk.CallToolRequest{}, nil)

	// WrapToolHandler must not propagate the savePayload error.
	require.NoError(t, err, "WrapToolHandler must not return an error when savePayload fails")
	require.NotNil(t, result)

	// Assert the observable failure-specific behavior: the wrapper still returns
	// metadata (schema + preview), but no payload path/file is produced.
	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(*sdk.TextContent)
	require.True(t, ok, "expected TextContent in result")

	var meta map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(textContent.Text), &meta))
	assert.Contains(t, meta, "payloadSchema", "expected metadata response to include payloadSchema when payload saving fails")
	assert.Contains(t, meta, "payloadPreview", "expected metadata response to include payloadPreview when payload saving fails")
	// payloadPath should be empty (no file was saved).
	assert.Equal(t, "", meta["payloadPath"], "payloadPath should be empty when savePayload fails")

	entries, readErr := os.ReadDir(baseDir)
	require.NoError(t, readErr)
	assert.Len(t, entries, 0, "savePayload failure path should not leave any saved payload files or directories behind")
}
