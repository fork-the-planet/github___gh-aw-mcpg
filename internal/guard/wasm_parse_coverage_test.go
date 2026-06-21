package guard

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
)

// allocGuardWasm is a WASM module that exports alloc, dealloc, label_agent, and memory.
// alloc(size i32) -> i32 always returns pointer 256.
// dealloc(ptr i32, size i32) is a no-op.
// label_agent(i32 i32 i32 i32) -> i32 always returns 0 (empty result).
//
// Used to exercise the alloc/dealloc allocator path in tryCallWasmFunction,
// and the resultLen==0 branch in decodeWasmCallResult.
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "alloc") (param i32) (result i32) i32.const 256)
//	  (func (export "dealloc") (param i32 i32))
//	  (func (export "label_agent") (param i32 i32 i32 i32) (result i32) i32.const 0))
var allocGuardWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x13, 0x03, 0x60, 0x01, 0x7f, 0x01, 0x7f,
	0x60, 0x02, 0x7f, 0x7f, 0x00, 0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f, 0x03, 0x04, 0x03,
	0x00, 0x01, 0x02, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x2a, 0x04, 0x06, 0x6d, 0x65, 0x6d, 0x6f,
	0x72, 0x79, 0x02, 0x00, 0x05, 0x61, 0x6c, 0x6c, 0x6f, 0x63, 0x00, 0x00, 0x07, 0x64, 0x65, 0x61,
	0x6c, 0x6c, 0x6f, 0x63, 0x00, 0x01, 0x0b, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x61, 0x67, 0x65,
	0x6e, 0x74, 0x00, 0x02, 0x0a, 0x0f, 0x03, 0x05, 0x00, 0x41, 0x80, 0x02, 0x0b, 0x02, 0x00, 0x0b,
	0x04, 0x00, 0x41, 0x00, 0x0b,
}

// allocReturnsZeroWasm is a WASM module like allocGuardWasm but alloc always returns 0.
// Used to test the "alloc returned null pointer" error path in wasmAlloc.
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "alloc") (param i32) (result i32) i32.const 0)
//	  (func (export "dealloc") (param i32 i32))
//	  (func (export "label_agent") (param i32 i32 i32 i32) (result i32) i32.const 0))
var allocReturnsZeroWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x13, 0x03, 0x60, 0x01, 0x7f, 0x01, 0x7f,
	0x60, 0x02, 0x7f, 0x7f, 0x00, 0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f, 0x03, 0x04, 0x03,
	0x00, 0x01, 0x02, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x2a, 0x04, 0x06, 0x6d, 0x65, 0x6d, 0x6f,
	0x72, 0x79, 0x02, 0x00, 0x05, 0x61, 0x6c, 0x6c, 0x6f, 0x63, 0x00, 0x00, 0x07, 0x64, 0x65, 0x61,
	0x6c, 0x6c, 0x6f, 0x63, 0x00, 0x01, 0x0b, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x61, 0x67, 0x65,
	0x6e, 0x74, 0x00, 0x02, 0x0a, 0x0e, 0x03, 0x04, 0x00, 0x41, 0x00, 0x0b, 0x02, 0x00, 0x0b, 0x04,
	0x00, 0x41, 0x00, 0x0b,
}

// allocReturnsVoidWasm is a WASM module where alloc returns void (no result).
// Used to test the "alloc returned no result" error path in wasmAlloc.
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "alloc") (param i32))     ;; void, no result
//	  (func (export "dealloc") (param i32 i32))
//	  (func (export "label_agent") (param i32 i32 i32 i32) (result i32) i32.const 0))
var allocReturnsVoidWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x12, 0x03, 0x60, 0x01, 0x7f, 0x00, 0x60,
	0x02, 0x7f, 0x7f, 0x00, 0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f, 0x03, 0x04, 0x03, 0x00,
	0x01, 0x02, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x2a, 0x04, 0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72,
	0x79, 0x02, 0x00, 0x05, 0x61, 0x6c, 0x6c, 0x6f, 0x63, 0x00, 0x00, 0x07, 0x64, 0x65, 0x61, 0x6c,
	0x6c, 0x6f, 0x63, 0x00, 0x01, 0x0b, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x61, 0x67, 0x65, 0x6e,
	0x74, 0x00, 0x02, 0x0a, 0x0c, 0x03, 0x02, 0x00, 0x0b, 0x02, 0x00, 0x0b, 0x04, 0x00, 0x41, 0x00,
	0x0b,
}

// labelReturnsNeg1Wasm is a WASM module where label_agent returns -1 (error code, not -2).
// Used to test the decodeWasmCallResult branch where resultLen < 0 but not -2.
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "label_agent") (param i32 i32 i32 i32) (result i32) i32.const -1))
var labelReturnsNeg1Wasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f,
	0x7f, 0x01, 0x7f, 0x03, 0x02, 0x01, 0x00, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x18, 0x02, 0x06,
	0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, 0x0b, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x61,
	0x67, 0x65, 0x6e, 0x74, 0x00, 0x00, 0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x7f, 0x0b,
}

// setupWasmGuard instantiates the given WASM binary and returns a WasmGuard
// bound to it. The caller must invoke the returned cleanup function.
func setupWasmGuard(t *testing.T, wasmBytes []byte, name string) (*WasmGuard, func()) {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	mod, err := rt.InstantiateWithConfig(ctx, wasmBytes, wazero.NewModuleConfig().WithName(name))
	require.NoError(t, err, "failed to instantiate WASM module %s", name)
	g := &WasmGuard{name: name, module: mod}
	cleanup := func() {
		require.NoError(t, mod.Close(ctx))
		require.NoError(t, rt.Close(ctx))
	}
	return g, cleanup
}

// TestParsePathLabeledResponse_NewPathLabeledDataError covers the branch where
// difc.NewPathLabeledData returns an error because the items_path cannot be
// resolved against the supplied original data.
func TestParsePathLabeledResponse_NewPathLabeledDataError(t *testing.T) {
	// "labeled_paths" is valid JSON, but "items_path" "/items" cannot be
	// found in the map {"other": "value"} that is supplied as originalData.
	responseJSON := []byte(`{"labeled_paths":[],"items_path":"/items"}`)
	originalData := map[string]interface{}{"other": "value"}

	result, err := parsePathLabeledResponse(responseJSON, originalData)

	require.Error(t, err, "expected error when items_path is not found")
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "apply path labels")
}

// TestTryCallWasmFunction_AllocatorPath exercises the alloc/dealloc allocator path
// in tryCallWasmFunction (lines 198-220), which was previously uncovered because
// no test WASM module exported both alloc and dealloc.
func TestTryCallWasmFunction_AllocatorPath(t *testing.T) {
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "alloc-path-test")
	defer cleanup()

	ctx := context.Background()
	fn := g.module.ExportedFunction("label_agent")
	mem := g.module.Memory()
	require.NotNil(t, fn, "label_agent must be exported")
	require.NotNil(t, mem, "memory must be exported")
	// Both alloc and dealloc are exported, so tryCallWasmFunction will use
	// the allocator path rather than the direct-memory fallback.
	require.NotNil(t, g.module.ExportedFunction("alloc"), "alloc must be exported")
	require.NotNil(t, g.module.ExportedFunction("dealloc"), "dealloc must be exported")

	// label_agent returns 0 (empty result) so we expect []byte{} back.
	g.mu.Lock()
	result, err := g.callWasmFunction(ctx, "label_agent", []byte(`{}`))
	g.mu.Unlock()

	require.NoError(t, err, "callWasmFunction should succeed on allocator path")
	assert.Equal(t, []byte{}, result, "empty result expected (label_agent returns 0)")
}

// TestDecodeWasmCallResult_ResultLenZero covers the resultLen == 0 branch in
// decodeWasmCallResult (branch G), which returns an empty byte slice.
// This is exercised via callWasmFunction on allocGuardWasm (returns i32.const 0).
func TestDecodeWasmCallResult_ResultLenZero(t *testing.T) {
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "result-zero-test")
	defer cleanup()

	ctx := context.Background()
	g.mu.Lock()
	result, err := g.callWasmFunction(ctx, "label_agent", []byte(`{}`))
	g.mu.Unlock()

	require.NoError(t, err)
	assert.Equal(t, []byte{}, result, "resultLen 0 should return empty byte slice, not nil")
}

// TestDecodeWasmCallResult_NegativeErrorCode covers the resultLen < 0 (not -2)
// branch in decodeWasmCallResult (branch F). labelReturnsNeg1Wasm returns -1.
func TestDecodeWasmCallResult_NegativeErrorCode(t *testing.T) {
	g, cleanup := setupWasmGuard(t, labelReturnsNeg1Wasm, "neg1-test")
	defer cleanup()

	ctx := context.Background()
	g.mu.Lock()
	_, err := g.callWasmFunction(ctx, "label_agent", []byte(`{}`))
	g.mu.Unlock()

	require.Error(t, err, "negative return code should produce an error")
	assert.ErrorContains(t, err, "error code")
}

// TestWasmAlloc_NullPointerReturned covers the "alloc returned null pointer"
// error path in wasmAlloc when the WASM alloc function returns 0.
func TestWasmAlloc_NullPointerReturned(t *testing.T) {
	g, cleanup := setupWasmGuard(t, allocReturnsZeroWasm, "alloc-null-test")
	defer cleanup()

	ctx := context.Background()
	g.mu.Lock()
	_, err := g.callWasmFunction(ctx, "label_agent", []byte(`{}`))
	g.mu.Unlock()

	require.Error(t, err, "null pointer from alloc should return an error")
	assert.ErrorContains(t, err, "null pointer")
}

// TestWasmAlloc_NoResultReturned covers the "alloc returned no result" error
// path in wasmAlloc when the WASM alloc function returns void.
func TestWasmAlloc_NoResultReturned(t *testing.T) {
	g, cleanup := setupWasmGuard(t, allocReturnsVoidWasm, "alloc-void-test")
	defer cleanup()

	ctx := context.Background()
	g.mu.Lock()
	_, err := g.callWasmFunction(ctx, "label_agent", []byte(`{}`))
	g.mu.Unlock()

	require.Error(t, err, "void alloc should return an error")
	assert.ErrorContains(t, err, "no result")
}

// TestWasmDealloc_NilFunctionIsNoOp verifies that wasmDealloc is safe to call
// when the dealloc function is nil.
func TestWasmDealloc_NilFunctionIsNoOp(t *testing.T) {
	g := &WasmGuard{name: "dealloc-nil-test"}
	ctx := context.Background()
	// Should not panic
	g.wasmDealloc(ctx, nil, 256, 128)
}

// TestWasmDealloc_ZeroPtrIsNoOp verifies that wasmDealloc is safe to call
// when ptr is zero.
func TestWasmDealloc_ZeroPtrIsNoOp(t *testing.T) {
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "dealloc-zero-ptr-test")
	defer cleanup()

	ctx := context.Background()
	deallocFn := g.module.ExportedFunction("dealloc")
	require.NotNil(t, deallocFn)

	// Should not panic or error
	g.wasmDealloc(ctx, deallocFn, 0, 128)
}

// TestWasmDealloc_ZeroSizeIsNoOp verifies that wasmDealloc is safe to call
// when size is zero.
func TestWasmDealloc_ZeroSizeIsNoOp(t *testing.T) {
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "dealloc-zero-size-test")
	defer cleanup()

	ctx := context.Background()
	deallocFn := g.module.ExportedFunction("dealloc")
	require.NotNil(t, deallocFn)

	// Should not panic or error
	g.wasmDealloc(ctx, deallocFn, 256, 0)
}

// TestWasmDealloc_ValidCall verifies that wasmDealloc successfully calls the
// WASM dealloc function with a valid pointer and size.
func TestWasmDealloc_ValidCall(t *testing.T) {
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "dealloc-valid-test")
	defer cleanup()

	ctx := context.Background()
	deallocFn := g.module.ExportedFunction("dealloc")
	require.NotNil(t, deallocFn)

	// dealloc is a no-op in allocGuardWasm; just verify it doesn't panic/error.
	g.wasmDealloc(ctx, deallocFn, 256, 128)
}

// TestWasmMemorySize_NilMemory covers the nil memory branch in wasmMemorySize.
func TestWasmMemorySize_NilMemory(t *testing.T) {
	size, ok := wasmMemorySize(nil)
	assert.False(t, ok, "nil memory should return ok=false")
	assert.Equal(t, uint32(0), size, "nil memory should return size=0")
}
