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
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "label_response") (param i32 i32 i32 i32) (result i32)
//	    ;; params: inPtr inLen outPtr outLen
//	    local.get 2          ;; outPtr
//	    i32.const 32123      ;; 0x7D7B – little-endian encoding of "{}"
//	    i32.store align=1
//	    i32.const 2))        ;; return 2 (bytes written)
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
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "label_response") (param i32 i32 i32 i32) (result i32)
//	    ;; params: inPtr inLen outPtr outLen
//	    ;; Writes the 24-byte string {"items":[{"data":"x"}]} at outPtr
//	    local.get 2  i32.const 1953047163  i32.store align=1           ;; {"it
//	    local.get 2  i32.const 577989989   i32.store offset=4  align=1 ;; ems"
//	    local.get 2  i32.const 578509626   i32.store offset=8  align=1 ;; :[{"
//	    local.get 2  i32.const 1635017060  i32.store offset=12 align=1 ;; data
//	    local.get 2  i32.const 2015509026  i32.store offset=16 align=1 ;; ":"x
//	    local.get 2  i32.const 2103278882  i32.store offset=20 align=1 ;; "}]}
//	    i32.const 24))
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
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "label_response") (param i32 i32 i32 i32) (result i32)
//	    ;; params: inPtr inLen outPtr outLen
//	    ;; Writes the 42-byte string {"labeled_paths":[],"items_path":"/items"} at outPtr
//	    local.get 2  i32.const 1634476667  i32.store align=1           ;; {"la
//	    local.get 2  i32.const 1701602658  i32.store offset=4  align=1 ;; bele
//	    local.get 2  i32.const 1634754404  i32.store offset=8  align=1 ;; d_pa
//	    local.get 2  i32.const 577988724   i32.store offset=12 align=1 ;; ths"
//	    local.get 2  i32.const 744315706   i32.store offset=16 align=1 ;; :[],"
//	    local.get 2  i32.const 1702127906  i32.store offset=20 align=1 ;; item
//	    local.get 2  i32.const 1885303661  i32.store offset=24 align=1 ;; s_pa
//	    local.get 2  i32.const 577270881   i32.store offset=28 align=1 ;; th":
//	    local.get 2  i32.const 1764696634  i32.store offset=32 align=1 ;; "/it
//	    local.get 2  i32.const 1936549236  i32.store offset=36 align=1 ;; ems"
//	    local.get 2  i32.const 32034       i32.store offset=40 align=1 ;; }
//	    i32.const 42))
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

// labelResponseWritesItemsIfInputLargeWasm exports "label_response" and "memory".
// It writes {"items":[{"data":"x"}]} (24 bytes) if inLen > 50, otherwise writes {} (2 bytes).
//
// This fixture is used by the context-state and capabilities input-preparation tests.
// When tool_args or capabilities are injected into the WASM input the serialised JSON
// exceeds the 50-byte threshold, so the guard returns an items response that
// LabelResponse parses into a non-nil *CollectionLabeledData. If those fields are
// absent the short baseline input (42 bytes) falls below the threshold and the guard
// returns {}, causing LabelResponse to return (nil, nil). The tests therefore fail
// if the input-preparation logic is removed.
//
// Input size reference (json.Marshal key-sorted output, toolName="my_tool"):
//
//	baseline (no extras)          42 B  – {"tool_name":…,"tool_result":null}
//	+ capabilities ({})           60 B
//	+ tool_args ({"param":"…"})   72 B
//	threshold                     50 B
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "label_response") (param i32 i32 i32 i32) (result i32)
//	    ;; params: inPtr inLen outPtr outLen
//	    local.get 1     ;; inLen
//	    i32.const 50
//	    i32.gt_u
//	    if
//	      ;; Write {"items":[{"data":"x"}]} (24 bytes) at outPtr
//	      local.get 2  i32.const 1953047163  i32.store           ;; {"it
//	      local.get 2  i32.const 4  i32.add  i32.const 577989989   i32.store ;; ems"
//	      local.get 2  i32.const 8  i32.add  i32.const 578509626   i32.store ;; :[{"
//	      local.get 2  i32.const 12 i32.add  i32.const 1635017060  i32.store ;; data
//	      local.get 2  i32.const 16 i32.add  i32.const 2015509026  i32.store ;; ":"x
//	      local.get 2  i32.const 20 i32.add  i32.const 2103278882  i32.store ;; "}]}
//	      i32.const 24
//	      return
//	    end
//	    ;; Write {} (2 bytes) at outPtr
//	    local.get 2  i32.const 32123  i32.store  ;; 0x7D7B – little-endian encoding of "{}"
//	    i32.const 2))
var labelResponseWritesItemsIfInputLargeWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f,
	0x7f, 0x01, 0x7f, 0x03, 0x02, 0x01, 0x00, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x1b, 0x02, 0x06,
	0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, 0x0e, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x72,
	0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x00, 0x00, 0x0a, 0x6b, 0x01, 0x69, 0x00, 0x20, 0x01,
	0x41, 0x32, 0x4b, 0x04, 0x40, 0x20, 0x02, 0x41, 0xfb, 0xc4, 0xa4, 0xa3, 0x07, 0x36, 0x02, 0x00,
	0x20, 0x02, 0x41, 0x04, 0x6a, 0x41, 0xe5, 0xda, 0xcd, 0x93, 0x02, 0x36, 0x02, 0x00, 0x20, 0x02,
	0x41, 0x08, 0x6a, 0x41, 0xba, 0xb6, 0xed, 0x93, 0x02, 0x36, 0x02, 0x00, 0x20, 0x02, 0x41, 0x0c,
	0x6a, 0x41, 0xe4, 0xc2, 0xd1, 0x8b, 0x06, 0x36, 0x02, 0x00, 0x20, 0x02, 0x41, 0x10, 0x6a, 0x41,
	0xa2, 0xf4, 0x88, 0xc1, 0x07, 0x36, 0x02, 0x00, 0x20, 0x02, 0x41, 0x14, 0x6a, 0x41, 0xa2, 0xfa,
	0xf5, 0xea, 0x07, 0x36, 0x02, 0x00, 0x41, 0x18, 0x0f, 0x0b, 0x20, 0x02, 0x41, 0xfb, 0xfa, 0x01,
	0x36, 0x02, 0x00, 0x41, 0x02, 0x0b,
}

// --- TestLabelResponse: context state and capabilities input-preparation paths ---

// TestLabelResponse_ContextStateWithToolArgs verifies that when the context contains
// a request state map with a "tool_args" key, LabelResponse adds it to the WASM input.
// labelResponseWritesItemsIfInputLargeWasm returns {"items":[{"data":"x"}]} only when
// inLen > 50; the baseline input is 42 bytes and grows to 72 bytes once tool_args is
// injected. A nil result therefore means the field was not added to the input.
func TestLabelResponse_ContextStateWithToolArgs(t *testing.T) {
	g, cleanup := setupRawWasmModule(t, labelResponseWritesItemsIfInputLargeWasm, "lresp-state-tool-args")
	defer cleanup()

	ctx := SetRequestStateInContext(context.Background(), map[string]any{
		"tool_args": map[string]any{"param": "value"},
	})

	result, err := g.LabelResponse(ctx, "my_tool", nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result, "tool_args must be included in the WASM input (inLen should exceed the 50-byte threshold)")
	_, ok := result.(*difc.CollectionLabeledData)
	assert.True(t, ok, "expected *CollectionLabeledData from items response")
}

// TestLabelResponse_ContextStateNonMapIsIgnored verifies that when the context contains
// a request state that is not a map[string]any, LabelResponse does NOT add tool_args
// to the input. The input stays at the 42-byte baseline (below the 50-byte threshold),
// so labelResponseWritesItemsIfInputLargeWasm returns {} and LabelResponse returns nil.
func TestLabelResponse_ContextStateNonMapIsIgnored(t *testing.T) {
	g, cleanup := setupRawWasmModule(t, labelResponseWritesItemsIfInputLargeWasm, "lresp-state-non-map")
	defer cleanup()

	// Pass a non-map state (e.g., a plain string). The type assertion in LabelResponse
	// will fail silently and tool_args will not be added to the WASM input.
	ctx := SetRequestStateInContext(context.Background(), "not-a-map")

	result, err := g.LabelResponse(ctx, "my_tool", nil, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result, "non-map state must not expand the input beyond the 50-byte threshold")
}

// TestLabelResponse_WithCapabilities verifies that when a non-nil *difc.Capabilities is
// provided, LabelResponse adds it to the WASM input. The capabilities value marshals to
// {} but still adds the "capabilities" key, growing the input from 42 to 60 bytes
// (above the 50-byte threshold), so the guard returns an items response.
func TestLabelResponse_WithCapabilities(t *testing.T) {
	g, cleanup := setupRawWasmModule(t, labelResponseWritesItemsIfInputLargeWasm, "lresp-caps")
	defer cleanup()

	caps := difc.NewCapabilities()
	caps.Add(difc.Tag("public"))

	result, err := g.LabelResponse(context.Background(), "my_tool", nil, nil, caps)
	require.NoError(t, err)
	require.NotNil(t, result, "capabilities must be included in the WASM input (inLen should exceed the 50-byte threshold)")
	_, ok := result.(*difc.CollectionLabeledData)
	assert.True(t, ok, "expected *CollectionLabeledData from items response")
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
// capabilities are injected into the WASM input together. With both fields present the
// serialised JSON reaches 97 bytes (well above the 50-byte threshold), so
// labelResponseWritesItemsIfInputLargeWasm returns an items response and LabelResponse
// returns a non-nil *CollectionLabeledData.
func TestLabelResponse_ContextStateAndCapabilities(t *testing.T) {
	g, cleanup := setupRawWasmModule(t, labelResponseWritesItemsIfInputLargeWasm, "lresp-state-and-caps")
	defer cleanup()

	ctx := SetRequestStateInContext(context.Background(), map[string]any{
		"tool_args": map[string]any{"arg1": "val1"},
	})
	caps := difc.NewCapabilities()

	result, err := g.LabelResponse(ctx, "my_tool", "some-result", nil, caps)
	require.NoError(t, err)
	require.NotNil(t, result, "tool_args and capabilities together must grow the WASM input beyond the 50-byte threshold")
	_, ok := result.(*difc.CollectionLabeledData)
	assert.True(t, ok, "expected *CollectionLabeledData from items response")
}
