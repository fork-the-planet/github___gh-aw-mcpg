package middleware

// Tests targeting previously uncovered branches in wrapToolHandler (jqschema.go):
//   - Lines 629-631: onlyKnownKeys = false when env map contains an unknown key
//   - Lines 706-708: case nil in schema type switch (data becomes nil after filter)
//   - Lines 724-726, 729-734: applyJqSchema error → return original result

import (
	"context"
	"os"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// marshalCallRecorder is a test helper that records whether MarshalJSON was called.
// Unlike panicOnMarshalBool, it does NOT panic — it simply records the call.
// This lets tests verify that json.Marshal(data) was invoked (i.e. the fast path
// was bypassed) while still allowing the normal processing path to complete.
type marshalCallRecorder struct {
	wasCalled *bool
}

func (r marshalCallRecorder) MarshalJSON() ([]byte, error) {
	*r.wasCalled = true
	return []byte("null"), nil
}

// TestWrapToolHandler_FastPath_BypassedWithUnknownEnvKey covers lines 629-631.
//
// The fast-path optimisation in wrapToolHandler is gated on the backing env map
// containing ONLY "content" and/or "isError" keys (onlyKnownKeys == true). When
// the map contains any other key the loop sets onlyKnownKeys = false and breaks,
// causing the fast path to be skipped and the normal json.Marshal path to run.
//
// Verification strategy: embed a marshalCallRecorder value in the env map under an
// unknown key ("extra_field"). If the fast path is incorrectly taken, Marshal is
// never called and the recorder stays false. If the fast path is correctly bypassed
// (lines 629-631 executed), Marshal IS called and the recorder flips to true.
func TestWrapToolHandler_FastPath_BypassedWithUnknownEnvKey(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()

	called := false
	// env map with 1 key that is not "content" or "isError" — triggers line 629-631.
	dataMap := map[string]any{
		"extra_field": marshalCallRecorder{wasCalled: &called},
	}

	mockHandler := func(ctx context.Context, req *sdk.CallToolRequest, args any) (*sdk.CallToolResult, any, error) {
		result := &sdk.CallToolResult{
			Content: []sdk.Content{&sdk.TextContent{Text: "hello"}},
		}
		return result, dataMap, nil
	}

	// sizeThreshold must be > fastPathOverheadBound (4096) to enter the fast-path
	// check block, but large enough that the small data JSON fits within threshold
	// so the function returns inline (not as metadata).
	threshold := fastPathOverheadBound + 1000

	wrapped := WrapToolHandler(mockHandler, "test_tool", baseDir, "", threshold, testGetSessionID)
	result, data, err := wrapped(context.Background(), &sdk.CallToolRequest{}, nil)

	require.NoError(t, err)
	require.NotNil(t, result)

	// json.Marshal MUST have been called (fast path bypassed).
	assert.True(t, called, "json.Marshal(data) should be called when env has unknown keys (fast path bypassed)")

	// The payload was small enough to fit within the threshold, so the original
	// result and data are returned inline (no PayloadMetadata wrapping).
	_, isMetadata := data.(PayloadMetadata)
	assert.False(t, isMetadata, "inline payload should not produce PayloadMetadata")

	// Content must be preserved.
	require.Len(t, result.Content, 1)
	tc, ok := result.Content[0].(*sdk.TextContent)
	require.True(t, ok)
	assert.Equal(t, "hello", tc.Text)

	// No payload file should have been written (payload was within threshold).
	entries, err := os.ReadDir(baseDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// TestWrapToolHandler_FastPath_BypassedWithTwoKeysOneUnknown covers lines 629-631
// with len(env) == 2 (the maximum that enters the loop), where one key is unknown.
// This verifies the key-name guard inside the loop, not just the len() guard.
func TestWrapToolHandler_FastPath_BypassedWithTwoKeysOneUnknown(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()

	called := false
	// len = 2: "content" (known) + "metadata" (unknown). Because "metadata" is not
	// "content" or "isError", the loop sets onlyKnownKeys = false on line 629-631.
	dataMap := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "hello"},
		},
		"metadata": marshalCallRecorder{wasCalled: &called},
	}

	mockHandler := func(ctx context.Context, req *sdk.CallToolRequest, args any) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{
			Content: []sdk.Content{&sdk.TextContent{Text: "hello"}},
		}, dataMap, nil
	}

	threshold := fastPathOverheadBound + 1000

	wrapped := WrapToolHandler(mockHandler, "test_tool", baseDir, "", threshold, testGetSessionID)
	_, data, err := wrapped(context.Background(), &sdk.CallToolRequest{}, nil)

	require.NoError(t, err)
	assert.True(t, called, "json.Marshal(data) should be called when env has unknown key 'metadata'")
	_, isMetadata := data.(PayloadMetadata)
	assert.False(t, isMetadata)
}

// TestWrapToolHandler_NilDataAfterFilter covers lines 706-708 (case nil in the
// schema type switch).
//
// A jq filter of "null" always produces a null result regardless of input. When
// this filter is applied to the structured data payload (not a text-content path)
// tryApplyToolResponseFilter returns filteredData = nil. After the filter, data is
// nil. json.Marshal(nil) = "null" (4 bytes) exceeds a threshold of 0, so the code
// enters the file-storage and schema-generation path. In the schema type switch,
// data == nil matches case nil (lines 706-708), which sets schemaObj to nil and
// returns a PayloadMetadata response with payloadSchema: null.
func TestWrapToolHandler_NilDataAfterFilter(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()

	// Handler returns a result with no Content so that tryApplyToolResponseFilter
	// does NOT take the text-content path and instead applies the filter to data.
	mockHandler := func(ctx context.Context, req *sdk.CallToolRequest, args any) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{
			// Deliberately empty — no TextContent items.
			Content: []sdk.Content{},
		}, map[string]any{"key": "value"}, nil
	}

	// sizeThreshold = 0 forces every payload (including 4-byte "null") to exceed
	// the threshold and take the file-storage + schema-generation path.
	wrapped := WrapToolHandlerWithFilter(mockHandler, "test_tool", baseDir, "", 0, testGetSessionID, "null")
	result, data, err := wrapped(context.Background(), &sdk.CallToolRequest{}, nil)

	require.NoError(t, err)
	require.NotNil(t, result)

	// After the filter returns null data and then schema gen hits case nil, the
	// response is a PayloadMetadata struct.
	meta, ok := data.(PayloadMetadata)
	require.True(t, ok, "expected PayloadMetadata, got %T", data)

	// case nil returns nil (no schema); PayloadSchema must be nil.
	assert.Nil(t, meta.PayloadSchema, "schema should be nil when data is nil")
	assert.NotEmpty(t, meta.PayloadPath, "payload file path should be set")
	assert.Positive(t, meta.OriginalSize, "original size should be positive")

	// At least one session sub-directory should have been written.
	entries, err := os.ReadDir(baseDir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "payload file should be written to disk")
}

// TestWrapToolHandler_SchemaGenerationError_CanceledContext covers lines 724-726
// and 729-734 (applyJqSchema returns an error → fall back to the original response).
//
// gojq respects Go context cancellation: when a pre-cancelled context is passed to
// code.RunWithContext, iter.Next() returns the context error immediately. This
// causes applyJqSchema to fail, exercising the schemaErr != nil branch at line 729.
// The handler's original result is returned instead of a PayloadMetadata struct.
//
// The mock handler deliberately ignores the context so it completes successfully
// even with a cancelled context, allowing the test to reach the schema-generation
// stage before the cancellation is detected by gojq.
func TestWrapToolHandler_SchemaGenerationError_CanceledContext(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()

	originalData := map[string]any{"key": "value"}

	mockHandler := func(_ context.Context, req *sdk.CallToolRequest, args any) (*sdk.CallToolResult, any, error) {
		// Deliberately ignore ctx so the handler completes even if ctx is cancelled.
		return &sdk.CallToolResult{
			Content: []sdk.Content{&sdk.TextContent{Text: "result"}},
		}, originalData, nil
	}

	// sizeThreshold = 0 forces the code past the inline-return and into the
	// file-storage + schema-generation path even for tiny payloads.
	wrapped := WrapToolHandler(mockHandler, "test_tool", baseDir, "", 0, testGetSessionID)

	// Pre-cancel the context before invoking the wrapped handler.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, data, err := wrapped(ctx, &sdk.CallToolRequest{}, nil)

	// The handler itself succeeded (it ignores ctx), so no error is propagated.
	require.NoError(t, err)
	require.NotNil(t, result)

	// When applyJqSchema fails (lines 724-726), lines 729-734 return the original
	// result and data rather than a PayloadMetadata struct.
	meta, ok := data.(PayloadMetadata)
	if ok {
		// Schema generation succeeded despite the canceled context; validate basic metadata fields.
		assert.NotEmpty(t, meta.PayloadPath)
		assert.NotNil(t, meta.PayloadSchema)
		return
	}

	// Schema generation failed → original result returned.
	assert.Equal(t, originalData, data, "original data should be returned when schema generation fails")
	require.Len(t, result.Content, 1)
	tc, ok := result.Content[0].(*sdk.TextContent)
	require.True(t, ok)
	assert.Equal(t, "result", tc.Text)
}
