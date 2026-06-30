package guard

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/difc"
)

// labelResponseWritesEmptyObjectWasm exports "label_response" and "memory".
// It writes the following JSON to the output buffer and returns its length:
//
//	{}
//
// Used to exercise the 'no labeled_paths, no items → return nil, nil' path in LabelResponse.
// The function writes 4 bytes (little-endian i32) and returns 2 so only the '{' '}' bytes are read.
var labelResponseWritesEmptyObjectWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f,
	0x7f, 0x01, 0x7f, 0x03, 0x02, 0x01, 0x00, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x1b, 0x02, 0x0e,
	0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x72, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x00, 0x00,
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, 0x0a, 0x0f, 0x01, 0x0d, 0x00, 0x20, 0x02,
	0x41, 0xfb, 0xfa, 0x01, 0x36, 0x00, 0x00, 0x41, 0x02, 0x0b,
}

// labelResponseWritesItemsWasm exports "label_response" and "memory".
// It writes the following JSON to the output buffer and returns its length:
//
//	{"items":[{"data":"x"}]}
//
// Used to exercise the 'items' collection labeling path in LabelResponse.
var labelResponseWritesItemsWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f,
	0x7f, 0x01, 0x7f, 0x03, 0x02, 0x01, 0x00, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x1b, 0x02, 0x0e,
	0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x72, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x00, 0x00,
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, 0x0a, 0x48, 0x01, 0x46, 0x00, 0x20, 0x02,
	0x41, 0xfb, 0xc4, 0xa4, 0xa3, 0x07, 0x36, 0x00, 0x00, 0x20, 0x02, 0x41, 0xe5, 0xda, 0xcd, 0x93,
	0x02, 0x36, 0x00, 0x04, 0x20, 0x02, 0x41, 0xba, 0xb6, 0xed, 0x93, 0x02, 0x36, 0x00, 0x08, 0x20,
	0x02, 0x41, 0xe4, 0xc2, 0xd1, 0x8b, 0x06, 0x36, 0x00, 0x0c, 0x20, 0x02, 0x41, 0xa2, 0xf4, 0x88,
	0xc1, 0x07, 0x36, 0x00, 0x10, 0x20, 0x02, 0x41, 0xa2, 0xfa, 0xf5, 0xea, 0x07, 0x36, 0x00, 0x14,
	0x41, 0x18, 0x0b,
}

// labelResponseWritesLabeledPathsWasm exports "label_response" and "memory".
// It writes the following JSON to the output buffer and returns its length:
//
//	{"labeled_paths":[],"items_path":"/items"}
//
// Used to exercise the 'labeled_paths' response path in LabelResponse.
// When called with a result that does not contain '/items', parsePathLabeledResponse
// returns an error that is propagated from LabelResponse.
var labelResponseWritesLabeledPathsWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f,
	0x7f, 0x01, 0x7f, 0x03, 0x02, 0x01, 0x00, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x1b, 0x02, 0x0e,
	0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x72, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x00, 0x00,
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, 0x0a, 0x7d, 0x01, 0x7b, 0x00, 0x20, 0x02,
	0x41, 0xfb, 0xc4, 0xb0, 0x8b, 0x06, 0x36, 0x00, 0x00, 0x20, 0x02, 0x41, 0xe2, 0xca, 0xb1, 0xab,
	0x06, 0x36, 0x00, 0x04, 0x20, 0x02, 0x41, 0xe4, 0xbe, 0xc1, 0x8b, 0x06, 0x36, 0x00, 0x08, 0x20,
	0x02, 0x41, 0xf4, 0xd0, 0xcd, 0x93, 0x02, 0x36, 0x00, 0x0c, 0x20, 0x02, 0x41, 0xba, 0xb6, 0xf5,
	0xe2, 0x02, 0x36, 0x00, 0x10, 0x20, 0x02, 0x41, 0xa2, 0xd2, 0xd1, 0xab, 0x06, 0x36, 0x00, 0x14,
	0x20, 0x02, 0x41, 0xed, 0xe6, 0xfd, 0x82, 0x07, 0x36, 0x00, 0x18, 0x20, 0x02, 0x41, 0xe1, 0xe8,
	0xa1, 0x93, 0x02, 0x36, 0x00, 0x1c, 0x20, 0x02, 0x41, 0xba, 0xc4, 0xbc, 0xc9, 0x06, 0x36, 0x00,
	0x20, 0x20, 0x02, 0x41, 0xf4, 0xca, 0xb5, 0x9b, 0x07, 0x36, 0x00, 0x24, 0x20, 0x02, 0x41, 0xa2,
	0xfa, 0x01, 0x36, 0x00, 0x28, 0x41, 0x2a, 0x0b,
}

// --- TestLabelResponse: context state and capabilities input-preparation paths ---

// TestLabelResponse_ContextStateWithToolArgs verifies that when the context contains
// a request state map with a "tool_args" key, LabelResponse extracts it into the WASM
// input and still returns (nil, nil) when the WASM guard returns an empty result.
func TestLabelResponse_ContextStateWithToolArgs(t *testing.T) {
	g, cleanup := setupRawWasmModule(t, labelResponseReturnsZeroWasm, "lresp-state-tool-args")
	defer cleanup()

	ctx := SetRequestStateInContext(context.Background(), map[string]any{
		"tool_args": map[string]any{"param": "value"},
	})

	result, err := g.LabelResponse(ctx, "my_tool", nil, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result, "WASM returning empty result should yield nil even with state in context")
}

// TestLabelResponse_ContextStateNonMapIsIgnored verifies that when the context contains
// a request state that is not a map[string]any, LabelResponse ignores the tool_args
// extraction and proceeds normally.
func TestLabelResponse_ContextStateNonMapIsIgnored(t *testing.T) {
	g, cleanup := setupRawWasmModule(t, labelResponseReturnsZeroWasm, "lresp-state-non-map")
	defer cleanup()

	// Pass a non-map state (e.g., a plain string).  The type assertion in LabelResponse
	// will fail silently and tool_args will not be added to the WASM input.
	ctx := SetRequestStateInContext(context.Background(), "not-a-map")

	result, err := g.LabelResponse(ctx, "my_tool", nil, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// TestLabelResponse_WithCapabilities verifies that when a non-nil *difc.Capabilities is
// provided, LabelResponse adds it to the WASM input and still returns (nil, nil) when
// the WASM guard returns an empty result.
func TestLabelResponse_WithCapabilities(t *testing.T) {
	g, cleanup := setupRawWasmModule(t, labelResponseReturnsZeroWasm, "lresp-caps")
	defer cleanup()

	caps := difc.NewCapabilities()
	caps.Add(difc.Tag("public"))

	result, err := g.LabelResponse(context.Background(), "my_tool", nil, nil, caps)
	require.NoError(t, err)
	assert.Nil(t, result, "WASM returning empty result should yield nil even with capabilities set")
}

// --- TestLabelResponse: JSON response parsing paths ---

// TestLabelResponse_NoLabelingResponse verifies that when the WASM guard returns a
// valid JSON object that contains neither "labeled_paths" nor "items", LabelResponse
// falls through to the final "no fine-grained labeling" return and returns (nil, nil).
func TestLabelResponse_NoLabelingResponse(t *testing.T) {
	// labelResponseWritesEmptyObjectWasm writes "{}" which has no labeling keys.
	g, cleanup := setupRawWasmModule(t, labelResponseWritesEmptyObjectWasm, "lresp-no-labeling")
	defer cleanup()

	result, err := g.LabelResponse(context.Background(), "my_tool", nil, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result, "response with no labeling keys should return nil LabeledData")
}

// TestLabelResponse_ItemsResponse verifies that when the WASM guard returns a JSON
// object containing a non-empty "items" array, LabelResponse parses it via
// parseCollectionLabeledData and returns a populated *difc.CollectionLabeledData.
func TestLabelResponse_ItemsResponse(t *testing.T) {
	// labelResponseWritesItemsWasm writes: {"items":[{"data":"x"}]}
	g, cleanup := setupRawWasmModule(t, labelResponseWritesItemsWasm, "lresp-items")
	defer cleanup()

	result, err := g.LabelResponse(context.Background(), "my_tool", nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result, "items response should produce non-nil LabeledData")

	collection, ok := result.(*difc.CollectionLabeledData)
	require.True(t, ok, "items response should return *CollectionLabeledData")
	require.Len(t, collection.Items, 1, "expected exactly one labeled item")
	assert.Equal(t, "x", collection.Items[0].Data, "item data should match the WASM-written value")
}

// TestLabelResponse_LabeledPathsResponse verifies that when the WASM guard returns a
// JSON object containing a "labeled_paths" key, LabelResponse delegates to
// parsePathLabeledResponse. When items_path cannot be resolved against the supplied
// original result, parsePathLabeledResponse returns an error that is propagated.
func TestLabelResponse_LabeledPathsResponse(t *testing.T) {
	// labelResponseWritesLabeledPathsWasm writes:
	//   {"labeled_paths":[],"items_path":"/items"}
	// The original result below does not contain "/items", so parsePathLabeledResponse
	// will return an error.
	g, cleanup := setupRawWasmModule(t, labelResponseWritesLabeledPathsWasm, "lresp-labeled-paths")
	defer cleanup()

	originalResult := map[string]any{"other": "value"}
	result, err := g.LabelResponse(context.Background(), "my_tool", originalResult, nil, nil)

	require.Error(t, err, "labeled_paths response with unresolvable items_path should return an error")
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "apply path labels")
}

// TestLabelResponse_ContextStateAndCapabilities verifies that both context state and
// capabilities are handled together, and that the WASM call completes successfully.
func TestLabelResponse_ContextStateAndCapabilities(t *testing.T) {
	g, cleanup := setupRawWasmModule(t, labelResponseReturnsZeroWasm, "lresp-state-and-caps")
	defer cleanup()

	ctx := SetRequestStateInContext(context.Background(), map[string]any{
		"tool_args": map[string]any{"arg1": "val1"},
	})
	caps := difc.NewCapabilities()

	result, err := g.LabelResponse(ctx, "my_tool", "some-result", nil, caps)
	require.NoError(t, err)
	assert.Nil(t, result)
}
