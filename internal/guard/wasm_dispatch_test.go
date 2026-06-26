package guard

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
)

// labelResourceReturnsZeroWasm exports "label_resource" and "memory"; the function
// always returns i32.const 0 (empty result). Used to exercise LabelResource paths
// where callWasmGuardFunction succeeds but returns an empty byte slice.
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "label_resource") (param i32 i32 i32 i32) (result i32) i32.const 0))
var labelResourceReturnsZeroWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f,
	0x7f, 0x01, 0x7f, 0x03, 0x02, 0x01, 0x00, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x1b, 0x02, 0x0e,
	0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x00, 0x00,
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, 0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x00,
	0x0b,
}

// labelResponseReturnsZeroWasm exports "label_response" and "memory"; always returns 0.
// Used to exercise the len(resultJSON)==0 early-return path in LabelResponse.
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "label_response") (param i32 i32 i32 i32) (result i32) i32.const 0))
var labelResponseReturnsZeroWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f,
	0x7f, 0x01, 0x7f, 0x03, 0x02, 0x01, 0x00, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x1b, 0x02, 0x0e,
	0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x72, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x00, 0x00,
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, 0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x00,
	0x0b,
}

// labelResponseReturnsTwoWasm exports "label_response" and "memory"; returns i32.const 2.
// This claims to have written 2 bytes to the output buffer, but the buffer contains
// zeroed WASM memory (\x00\x00), which is not valid JSON. Used to exercise the
// unmarshalWasmResponse error path inside LabelResponse.
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "label_response") (param i32 i32 i32 i32) (result i32) i32.const 2))
var labelResponseReturnsTwoWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f,
	0x7f, 0x01, 0x7f, 0x03, 0x02, 0x01, 0x00, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x1b, 0x02, 0x0e,
	0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x72, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x00, 0x00,
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, 0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x02,
	0x0b,
}

// setupRawWasmModule instantiates a WASM module directly (bypassing NewWasmGuardWithOptions)
// and returns a WasmGuard wired to it plus a cleanup function.
func setupRawWasmModule(t *testing.T, wasmBytes []byte, name string) (*WasmGuard, func()) {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter())
	mod, err := rt.InstantiateWithConfig(ctx, wasmBytes, wazero.NewModuleConfig().WithName(name))
	require.NoError(t, err, "failed to instantiate WASM module %s", name)
	g := &WasmGuard{name: name, module: mod}
	return g, func() {
		require.NoError(t, mod.Close(ctx))
		require.NoError(t, rt.Close(ctx))
	}
}

// --- TestUnmarshalWasmResponse ---

func TestUnmarshalWasmResponse(t *testing.T) {
	t.Run("empty bytes returns error", func(t *testing.T) {
		m, err := unmarshalWasmResponse("test_fn", []byte{})
		require.Error(t, err)
		assert.Nil(t, m)
		assert.ErrorContains(t, err, "failed to unmarshal test_fn WASM response")
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		m, err := unmarshalWasmResponse("test_fn", []byte("not json"))
		require.Error(t, err)
		assert.Nil(t, m)
		assert.ErrorContains(t, err, "test_fn")
	})

	t.Run("JSON array returns error because it cannot decode to map", func(t *testing.T) {
		m, err := unmarshalWasmResponse("test_fn", []byte(`[1,2,3]`))
		require.Error(t, err)
		assert.Nil(t, m)
	})

	t.Run("valid JSON object succeeds", func(t *testing.T) {
		m, err := unmarshalWasmResponse("test_fn", []byte(`{"key":"value","num":42}`))
		require.NoError(t, err)
		require.NotNil(t, m)
		assert.Equal(t, "value", m["key"])
	})

	t.Run("empty JSON object succeeds", func(t *testing.T) {
		m, err := unmarshalWasmResponse("test_fn", []byte(`{}`))
		require.NoError(t, err)
		require.NotNil(t, m)
		assert.Empty(t, m)
	})

	t.Run("funcName is included in error message", func(t *testing.T) {
		_, err := unmarshalWasmResponse("my_special_fn", []byte("bad"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "my_special_fn")
	})
}

// --- TestNewWasmGuard ---

func TestNewWasmGuard(t *testing.T) {
	t.Run("returns error when file does not exist", func(t *testing.T) {
		ctx := context.Background()
		g, err := NewWasmGuard(ctx, "test-guard", "/nonexistent/path/guard.wasm", nil)
		require.Error(t, err)
		assert.Nil(t, g)
		assert.ErrorContains(t, err, "failed to read WASM file")
	})

	t.Run("returns error when file contains invalid WASM bytes", func(t *testing.T) {
		ctx := context.Background()
		tmpDir := t.TempDir()
		wasmPath := filepath.Join(tmpDir, "invalid.wasm")
		require.NoError(t, os.WriteFile(wasmPath, []byte("not wasm at all"), 0o600))

		g, err := NewWasmGuard(ctx, "test-guard", wasmPath, nil)
		require.Error(t, err)
		assert.Nil(t, g)
	})

	t.Run("loads a valid minimal WASM file and returns appropriate error", func(t *testing.T) {
		// minimalGuardWasm is valid WASM but lacks required guard function exports.
		// NewWasmGuard should fail with an informative error about missing exports.
		ctx := context.Background()
		tmpDir := t.TempDir()
		wasmPath := filepath.Join(tmpDir, "minimal.wasm")
		require.NoError(t, os.WriteFile(wasmPath, minimalGuardWasm, 0o600))

		g, err := NewWasmGuard(ctx, "test-guard", wasmPath, nil)
		require.Error(t, err)
		assert.Nil(t, g)
	})
}

// --- TestHostLog ---

func TestHostLog(t *testing.T) {
	const memOffset = uint64(256) // safe to write at this offset in allocGuardWasm memory

	msg := []byte("hello from wasm guard")
	writeMsg := func(t *testing.T, g *WasmGuard) {
		t.Helper()
		ok := g.module.Memory().Write(uint32(memOffset), msg)
		require.True(t, ok, "writing test message to WASM memory should succeed")
	}

	stack := func(level, ptr, length uint64) []uint64 {
		return []uint64{level, ptr, length}
	}

	t.Run("memory read failure returns without panic", func(t *testing.T) {
		g, cleanup := setupWasmGuard(t, allocGuardWasm, "hostlog-no-mem")
		defer cleanup()
		ctx := context.Background()
		// Pass a msgLen that far exceeds the memory size to force Read to fail.
		g.hostLog(ctx, g.module, stack(logLevelInfo, 0, 1<<30))
		// No panic, no crash — the function returns silently on memory read failure.
	})

	t.Run("logLevelDebug logs without error", func(t *testing.T) {
		g, cleanup := setupWasmGuard(t, allocGuardWasm, "hostlog-debug")
		defer cleanup()
		writeMsg(t, g)
		g.hostLog(context.Background(), g.module, stack(logLevelDebug, memOffset, uint64(len(msg))))
	})

	t.Run("logLevelInfo logs without error", func(t *testing.T) {
		g, cleanup := setupWasmGuard(t, allocGuardWasm, "hostlog-info")
		defer cleanup()
		writeMsg(t, g)
		g.hostLog(context.Background(), g.module, stack(logLevelInfo, memOffset, uint64(len(msg))))
	})

	t.Run("logLevelWarn logs without error", func(t *testing.T) {
		g, cleanup := setupWasmGuard(t, allocGuardWasm, "hostlog-warn")
		defer cleanup()
		writeMsg(t, g)
		g.hostLog(context.Background(), g.module, stack(logLevelWarn, memOffset, uint64(len(msg))))
	})

	t.Run("logLevelError logs without error", func(t *testing.T) {
		g, cleanup := setupWasmGuard(t, allocGuardWasm, "hostlog-error")
		defer cleanup()
		writeMsg(t, g)
		g.hostLog(context.Background(), g.module, stack(logLevelError, memOffset, uint64(len(msg))))
	})

	t.Run("unknown log level uses fallback format without panic", func(t *testing.T) {
		g, cleanup := setupWasmGuard(t, allocGuardWasm, "hostlog-unknown")
		defer cleanup()
		writeMsg(t, g)
		const unknownLevel = uint64(99)
		g.hostLog(context.Background(), g.module, stack(unknownLevel, memOffset, uint64(len(msg))))
	})
}

// --- TestLabelAgent ---

func TestLabelAgent_PolicyValidationErrors(t *testing.T) {
	// These tests exercise normalizePolicyPayload and buildStrictLabelAgentPayload
	// error paths. No WASM execution is needed because the errors occur before the
	// WASM call.
	g := &WasmGuard{name: "policy-check-guard"}

	t.Run("nil policy returns normalizePolicyPayload error", func(t *testing.T) {
		_, err := g.LabelAgent(context.Background(), nil, nil, nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "policy is required")
	})

	t.Run("empty string policy returns error", func(t *testing.T) {
		_, err := g.LabelAgent(context.Background(), "", nil, nil)
		require.Error(t, err)
	})

	t.Run("policy without allow-only returns buildStrictLabelAgentPayload error", func(t *testing.T) {
		policy := map[string]any{"unsupported-key": "value"}
		_, err := g.LabelAgent(context.Background(), policy, nil, nil)
		require.Error(t, err)
	})
}

func TestLabelAgent_FailedGuard(t *testing.T) {
	// A guard marked as failed must return an error immediately without calling WASM.
	g := &WasmGuard{
		name:      "failed-guard",
		failed:    true,
		failedErr: errors.New("previous trap"),
	}
	validPolicy := map[string]any{
		"allow-only": map[string]any{
			"repos":         "public",
			"min-integrity": "none",
		},
	}

	_, err := g.LabelAgent(context.Background(), validPolicy, nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "unavailable after a previous trap")
}

func TestLabelAgent_EmptyWasmResponse(t *testing.T) {
	// allocGuardWasm exports label_agent and returns i32.const 0 (empty result).
	// LabelAgent should detect the empty response and return an appropriate error.
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "la-empty-response")
	defer cleanup()

	validPolicy := map[string]any{
		"allow-only": map[string]any{
			"repos":         "public",
			"min-integrity": "none",
		},
	}

	_, err := g.LabelAgent(context.Background(), validPolicy, nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "label_agent returned empty response")
}

// --- TestLabelResource ---

func TestLabelResource_FailedGuard(t *testing.T) {
	g := &WasmGuard{
		name:      "failed-guard",
		failed:    true,
		failedErr: errors.New("previous trap"),
	}

	_, _, err := g.LabelResource(context.Background(), "my_tool", nil, nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "unavailable after a previous trap")
}

func TestLabelResource_FunctionNotExported(t *testing.T) {
	// allocGuardWasm exports label_agent but NOT label_resource.
	// LabelResource should get a "function not exported" error.
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "lr-no-export")
	defer cleanup()

	_, _, err := g.LabelResource(context.Background(), "my_tool", nil, nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "not exported from WASM module")
}

func TestLabelResource_EmptyWasmResponseTriggersUnmarshalError(t *testing.T) {
	// labelResourceReturnsZeroWasm exports label_resource and returns 0 (empty result).
	// LabelResource calls unmarshalWasmResponse on the empty byte slice, which should
	// fail JSON parsing.
	g, cleanup := setupRawWasmModule(t, labelResourceReturnsZeroWasm, "lr-empty-response")
	defer cleanup()

	_, _, err := g.LabelResource(context.Background(), "my_tool", nil, nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to unmarshal label_resource WASM response")
}

// --- TestLabelResponse ---

func TestLabelResponse_FailedGuard(t *testing.T) {
	g := &WasmGuard{
		name:      "failed-guard",
		failed:    true,
		failedErr: errors.New("previous trap"),
	}

	result, err := g.LabelResponse(context.Background(), "my_tool", nil, nil, nil)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "unavailable after a previous trap")
}

func TestLabelResponse_FunctionNotExported(t *testing.T) {
	// allocGuardWasm exports label_agent but NOT label_response.
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "lresp-no-export")
	defer cleanup()

	result, err := g.LabelResponse(context.Background(), "my_tool", nil, nil, nil)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestLabelResponse_EmptyResultReturnsNil(t *testing.T) {
	// labelResponseReturnsZeroWasm returns i32.const 0 (empty byte slice).
	// LabelResponse should detect len(resultJSON)==0 and return (nil, nil).
	g, cleanup := setupRawWasmModule(t, labelResponseReturnsZeroWasm, "lresp-empty")
	defer cleanup()

	result, err := g.LabelResponse(context.Background(), "my_tool", nil, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result, "empty WASM response should yield nil LabeledData")
}

func TestLabelResponse_InvalidJsonResponseReturnsError(t *testing.T) {
	// labelResponseReturnsTwoWasm claims to return 2 bytes but the output buffer
	// contains zeroed WASM memory (\x00\x00), which is not valid JSON.
	// LabelResponse should propagate the unmarshalWasmResponse error.
	g, cleanup := setupRawWasmModule(t, labelResponseReturnsTwoWasm, "lresp-invalid-json")
	defer cleanup()

	result, err := g.LabelResponse(context.Background(), "my_tool", nil, nil, nil)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "failed to unmarshal label_response WASM response")
}

// --- TestHostCallBackend ---

func TestHostCallBackend_ToolNameReadFailure(t *testing.T) {
	// Set toolNameLen to a huge value so Memory().Read fails.
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "hcb-toolname-fail")
	defer cleanup()

	stack := make([]uint64, 6)
	stack[0] = 0       // toolNamePtr
	stack[1] = 1 << 30 // toolNameLen — far exceeds memory size
	stack[2] = 0
	stack[3] = 0
	stack[4] = 0
	stack[5] = 0

	g.hostCallBackend(context.Background(), g.module, stack)
	const errorSentinel = uint64(0xFFFFFFFF)
	assert.Equal(t, errorSentinel, stack[0], "memory read failure should return -1 error sentinel")
}

func TestHostCallBackend_ArgsReadFailure(t *testing.T) {
	// Write a valid tool name at offset 256, but set argsLen to huge to force args read failure.
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "hcb-args-fail")
	defer cleanup()

	toolName := []byte("my_tool")
	ok := g.module.Memory().Write(256, toolName)
	require.True(t, ok)

	stack := make([]uint64, 6)
	stack[0] = 256                   // toolNamePtr
	stack[1] = uint64(len(toolName)) // toolNameLen
	stack[2] = 300                   // argsPtr
	stack[3] = 1 << 30               // argsLen — huge
	stack[4] = 0
	stack[5] = 0

	g.hostCallBackend(context.Background(), g.module, stack)
	const errorSentinel = uint64(0xFFFFFFFF)
	assert.Equal(t, errorSentinel, stack[0], "args read failure should return -1 error sentinel")
}

func TestHostCallBackend_InvalidArgsJSON(t *testing.T) {
	// Write a valid tool name and invalid JSON args to WASM memory.
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "hcb-invalid-args")
	defer cleanup()

	g.backend = &mockBackendCaller{}

	toolName := []byte("my_tool")
	badJSON := []byte("{not-valid-json")
	ok := g.module.Memory().Write(256, toolName)
	require.True(t, ok)
	ok = g.module.Memory().Write(300, badJSON)
	require.True(t, ok)

	stack := make([]uint64, 6)
	stack[0] = 256                   // toolNamePtr
	stack[1] = uint64(len(toolName)) // toolNameLen
	stack[2] = 300                   // argsPtr
	stack[3] = uint64(len(badJSON))  // argsLen
	stack[4] = 400
	stack[5] = 1024

	g.hostCallBackend(context.Background(), g.module, stack)
	const errorSentinel = uint64(0xFFFFFFFF)
	assert.Equal(t, errorSentinel, stack[0], "invalid JSON args should return -1 error sentinel")
}

func TestHostCallBackend_BackendCallFailure(t *testing.T) {
	// Set up a backend that returns an error.
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "hcb-backend-fail")
	defer cleanup()

	g.backend = &mockBackendCaller{err: errors.New("backend unavailable")}

	toolName := []byte("my_tool")
	ok := g.module.Memory().Write(256, toolName)
	require.True(t, ok)

	stack := make([]uint64, 6)
	stack[0] = 256                   // toolNamePtr
	stack[1] = uint64(len(toolName)) // toolNameLen
	stack[2] = 300                   // argsPtr
	stack[3] = 0                     // argsLen = 0 (no args)
	stack[4] = 400
	stack[5] = 1024

	g.hostCallBackend(context.Background(), g.module, stack)
	const errorSentinel = uint64(0xFFFFFFFF)
	assert.Equal(t, errorSentinel, stack[0], "backend error should return -1 error sentinel")
}

func TestHostCallBackend_ResultTooLarge(t *testing.T) {
	// Use a very small resultSize so the JSON result doesn't fit.
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "hcb-result-too-large")
	defer cleanup()

	g.backend = &mockBackendCaller{result: map[string]any{"data": "some response data"}}

	toolName := []byte("my_tool")
	ok := g.module.Memory().Write(256, toolName)
	require.True(t, ok)

	stack := make([]uint64, 6)
	stack[0] = 256                   // toolNamePtr
	stack[1] = uint64(len(toolName)) // toolNameLen
	stack[2] = 300                   // argsPtr
	stack[3] = 0                     // argsLen = 0
	stack[4] = 400                   // resultPtr
	stack[5] = 4                     // resultSize = 4 bytes (too small for JSON result)

	g.hostCallBackend(context.Background(), g.module, stack)
	// Result too large: the gateway signals buffer-too-small by returning -2,
	// which is encoded as uint64(0xFFFFFFFE) via setResult(int32(-2)).
	const bufTooSmall = uint64(0xFFFFFFFE)
	assert.Equal(t, bufTooSmall, stack[0], "result too large should return -2 buffer-too-small sentinel")

	requiredHint, ok := g.module.Memory().Read(400, 4)
	require.True(t, ok)
	requiredSize := binary.LittleEndian.Uint32(requiredHint)
	assert.Equal(t, uint32(len(`{"data":"some response data"}`)), requiredSize, "resultPtr should contain required JSON size hint")
}

func TestHostCallBackend_ResultTooLarge_BufferLessThan4(t *testing.T) {
	// resultSize < 4 means we cannot write the required-size hint.
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "hcb-result-too-large-lt4")
	defer cleanup()

	g.backend = &mockBackendCaller{result: map[string]any{"data": "some response"}}

	toolName := []byte("my_tool")
	ok := g.module.Memory().Write(256, toolName)
	require.True(t, ok)

	stack := make([]uint64, 6)
	stack[0] = 256
	stack[1] = uint64(len(toolName))
	stack[2] = 300
	stack[3] = 0
	stack[4] = 400
	stack[5] = 2 // resultSize < 4 — cannot write required-size hint

	ok = g.module.Memory().Write(400, []byte{0xAA, 0xBB})
	require.True(t, ok)

	g.hostCallBackend(context.Background(), g.module, stack)
	const bufTooSmall = uint64(0xFFFFFFFE)
	assert.Equal(t, bufTooSmall, stack[0], "result too large with small buffer should still return -2")

	bufferAfter, ok := g.module.Memory().Read(400, 2)
	require.True(t, ok)
	assert.Equal(t, []byte{0xAA, 0xBB}, bufferAfter, "result buffer should remain unchanged when resultSize < 4")
}

func TestHostCallBackend_SuccessWithNoArgs(t *testing.T) {
	// Happy path: valid tool name, no args, result fits in buffer.
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "hcb-success-no-args")
	defer cleanup()

	g.backend = &mockBackendCaller{result: map[string]any{"status": "ok"}}

	toolName := []byte("my_tool")
	ok := g.module.Memory().Write(256, toolName)
	require.True(t, ok)

	stack := make([]uint64, 6)
	stack[0] = 256
	stack[1] = uint64(len(toolName))
	stack[2] = 300
	stack[3] = 0    // argsLen = 0 (no args)
	stack[4] = 400  // resultPtr
	stack[5] = 4096 // resultSize large enough

	g.hostCallBackend(context.Background(), g.module, stack)

	// Success: stack[0] should be the result JSON byte length (positive).
	resultLen := int32(stack[0])
	assert.Greater(t, resultLen, int32(0), "successful call should return positive result length")
}

func TestHostCallBackend_SuccessWithValidArgs(t *testing.T) {
	// Happy path: valid tool name with valid JSON args.
	g, cleanup := setupWasmGuard(t, allocGuardWasm, "hcb-success-with-args")
	defer cleanup()

	g.backend = &mockBackendCaller{result: map[string]any{"count": 42}}

	toolName := []byte("list_items")
	argsJSON := []byte(`{"limit":10}`)
	ok := g.module.Memory().Write(256, toolName)
	require.True(t, ok)
	ok = g.module.Memory().Write(300, argsJSON)
	require.True(t, ok)

	stack := make([]uint64, 6)
	stack[0] = 256
	stack[1] = uint64(len(toolName))
	stack[2] = 300
	stack[3] = uint64(len(argsJSON))
	stack[4] = 400
	stack[5] = 4096

	g.hostCallBackend(context.Background(), g.module, stack)

	resultLen := int32(stack[0])
	assert.Greater(t, resultLen, int32(0), "successful call with args should return positive result length")
}
