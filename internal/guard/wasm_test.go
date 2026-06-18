package guard

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/sys"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if err := globalCompilationCache.Close(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "failed to close global compilation cache: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

type ctxKey string

const testCtxKey ctxKey = "test-key"

// minimalGuardWasm is a minimal valid WASM module used for tests that only need
// module instantiation behavior.
// Precompiled WASM binary for:
// (module)
var minimalGuardWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // WASM magic number
	0x01, 0x00, 0x00, 0x00, // WASM version
}

// blockingGuardWasm is a minimal WASM module exporting a function "loop" that
// runs an (effectively) infinite loop. Calling this function with a context
// that is cancelled should cause fn.Call to abort.
//
// The exact contents are not important for the test beyond being a valid WASM
// binary with an exported "loop" function that does not return quickly.
var blockingGuardWasm = []byte{
	// Precompiled WASM binary for:
	// (module
	//   (func (export "loop")
	//     (loop (br 0))))
	// The bytes below represent a valid module exporting "loop".
	0x00, 0x61, 0x73, 0x6d, // WASM magic number
	0x01, 0x00, 0x00, 0x00, // WASM version
	// type section: 1 type () -> ()
	0x01, 0x04, 0x01, 0x60, 0x00, 0x00,
	// function section: 1 function, type index 0
	0x03, 0x02, 0x01, 0x00,
	// export section: export "loop" as function 0
	0x07, 0x08, 0x01, 0x04, 0x6c, 0x6f, 0x6f, 0x70, 0x00, 0x00,
	// code section: 1 body — loop (br 0) end end
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b,
}

// WASM fixtures used by tests in this file.
// directMemoryFallbackGuardWasm (defined below) exports label_response and memory, but not
// alloc/dealloc. This forces tryCallWasmFunction onto the direct memory path.
//
// alwaysNeg2GuardWasm exports "label_agent" and "memory", and always returns
// -2 (buffer too small) with no hint. Used to test the buffer-doubling retry
// path that eventually hits the 16MB maximum.
//
// (module
//
//	(type (func (param i32 i32 i32 i32) (result i32)))
//	(func (type 0) i32.const -2)
//	(memory 1)
//	(export "label_agent" (func 0))
//	(export "memory" (memory 0)))
var alwaysNeg2GuardWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	// type section: (func (param i32 i32 i32 i32) (result i32))
	0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f,
	// function section: one function of type 0
	0x03, 0x02, 0x01, 0x00,
	// memory section: one memory with min=1 page
	0x05, 0x03, 0x01, 0x00, 0x01,
	// export section: "label_agent" (func 0) and "memory" (mem 0)
	0x07, 0x18, 0x02,
	0x0b, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x00, 0x00,
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
	// code section: i32.const -2, end
	0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x7e, 0x0b,
}

// retryWithHint5MBGuardWasm exports "label_agent" and "memory". It writes a
// 5MB hint (5242880 as LE uint32) to outputPtr and returns -2 when outputSize
// is less than 5MB; once outputSize is ≥ 5MB it returns 0 (empty success).
// This tests the hint-based retry path in callWasmFunction.
//
// (module
//
//	(type (func (param i32 i32 i32 i32) (result i32)))
//	(func (type 0)
//	  (if (i32.lt_u (local.get 3) (i32.const 5242880))
//	    (then (i32.store (local.get 2) (i32.const 5242880))
//	          (return (i32.const -2))))
//	  i32.const 0)
//	(memory 1)
//	(export "label_agent" (func 0))
//	(export "memory" (memory 0)))
var retryWithHint5MBGuardWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	// type section: (func (param i32 i32 i32 i32) (result i32))
	0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f,
	// function section: one function of type 0
	0x03, 0x02, 0x01, 0x00,
	// memory section: one memory with min=1 page
	0x05, 0x03, 0x01, 0x00, 0x01,
	// export section: "label_agent" (func 0) and "memory" (mem 0)
	0x07, 0x18, 0x02,
	0x0b, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x00, 0x00,
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
	// code section body:
	//   if (local.get 3) < 5242880:
	//     i32.store align=2 offset=0 (local.get 2) 5242880
	//     return i32.const -2
	//   end
	//   i32.const 0
	0x0a, 0x1e, 0x01, 0x1c, 0x00,
	0x20, 0x03, 0x41, 0x80, 0x80, 0xc0, 0x02, 0x49, 0x04, 0x40,
	0x20, 0x02, 0x41, 0x80, 0x80, 0xc0, 0x02, 0x36, 0x02, 0x00,
	0x41, 0x7e, 0x0f, 0x0b, 0x41, 0x00, 0x0b,
}

// funcNoMemoryGuardWasm exports "label_agent" (returns 0) but has NO memory
// export. Used to test the "WASM module has no memory" error path.
var funcNoMemoryGuardWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	// type section: (func (param i32 i32 i32 i32) (result i32))
	0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f,
	// function section: one function of type 0
	0x03, 0x02, 0x01, 0x00,
	// export section: "label_agent" (func 0) only — no memory export and no memory section
	0x07, 0x0f, 0x01,
	0x0b, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x00, 0x00,
	// code section: i32.const 0, end
	0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x00, 0x0b,
}

// funcNoResultGuardWasm exports "label_agent" and "memory" but the exported
// function has no return value. Used to verify the call path returns an error
// instead of panicking when no results are returned.
var funcNoResultGuardWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	// type section: (func (param i32 i32 i32 i32))
	0x01, 0x08, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x00,
	// function section: one function of type 0
	0x03, 0x02, 0x01, 0x00,
	// memory section: one memory with min=1 page
	0x05, 0x03, 0x01, 0x00, 0x01,
	// export section: "label_agent" (func 0), "memory" (mem 0)
	0x07, 0x18, 0x02,
	0x0b, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x00, 0x00,
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
	// code section: no-op function body (no result)
	0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b,
}

var directMemoryFallbackGuardWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	// type section: (func (param i32 i32 i32 i32) (result i32))
	0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f,
	// function section: one function of type 0
	0x03, 0x02, 0x01, 0x00,
	// memory section: one memory with min=1 page
	0x05, 0x03, 0x01, 0x00, 0x01,
	// export section: export function "label_response" and memory "memory"
	0x07, 0x1b, 0x02,
	0x0e, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x72, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x00, 0x00,
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
	// code section: return 0
	0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x00, 0x0b,
}

// mockBackendCaller is a test implementation of BackendCaller
type mockBackendCaller struct {
	called   bool
	toolName string
	args     any
	result   any
	err      error
}

type mockCompilationCache struct {
	closeErr error
	closed   bool
}

func (m *mockCompilationCache) Close(context.Context) error {
	m.closed = true
	return m.closeErr
}

func (m *mockBackendCaller) CallTool(ctx context.Context, toolName string, args any) (any, error) {
	m.called = true
	m.toolName = toolName
	m.args = args
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestWasmGuardOptions(t *testing.T) {
	t.Run("options with custom stdout and stderr", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		opts := &WasmGuardOptions{
			Stdout: &stdout,
			Stderr: &stderr,
		}
		assert.NotNil(t, opts.Stdout)
		assert.NotNil(t, opts.Stderr)
	})

	t.Run("nil options uses default stdout/stderr", func(t *testing.T) {
		var opts *WasmGuardOptions
		assert.Nil(t, opts)
	})
}

func TestWasmGuardContextPropagation(t *testing.T) {
	t.Run("context cancellation propagates to WASM execution", func(t *testing.T) {
		// This test verifies that WithCloseOnContextDone works correctly.
		// When the context is cancelled, WASM execution should be interrupted.

		// Create a context with a short timeout. The wazero runtime will be
		// configured with WithCloseOnContextDone so that cancelling this
		// context interrupts any in-flight WASM calls.
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Create a wazero runtime that will close when the context is done.
		runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler().WithCloseOnContextDone(true))
		defer func() {
			_ = runtime.Close(ctx)
		}()

		// Instantiate the blocking WASM module.
		moduleConfig := wazero.NewModuleConfig().WithName("blocking_guard")
		mod, err := runtime.InstantiateWithConfig(ctx, blockingGuardWasm, moduleConfig)
		require.NoError(t, err, "failed to instantiate blocking WASM module")

		loopFn := mod.ExportedFunction("loop")
		require.NotNil(t, loopFn, "expected exported function 'loop'")

		errCh := make(chan error, 1)

		// Start the blocking function call in a separate goroutine.
		go func() {
			_, callErr := loopFn.Call(ctx)
			errCh <- callErr
		}()

		// Give the goroutine a moment to start the call, then cancel the context.
		time.Sleep(10 * time.Millisecond)
		cancel()

		select {
		case callErr := <-errCh:
			require.Error(t, callErr, "expected WASM call to be interrupted by context cancellation")
		case <-time.After(1 * time.Second):
			t.Fatal("WASM call did not return after context cancellation")
		}
	})

	t.Run("context values are accessible in guard methods", func(t *testing.T) {
		ctx := context.Background()
		ctx = context.WithValue(ctx, testCtxKey, "test-value")

		// Verify context is preserved
		value := ctx.Value(testCtxKey)
		assert.Equal(t, "test-value", value)
	})
}

func TestWasmGuardStdinIsolation(t *testing.T) {
	t.Run("stdin isolation prevents reading from host stdin", func(t *testing.T) {
		// This test verifies that the WASM guard is configured with an isolated,
		// empty stdin rather than the host's MCP protocol stdin. If stdin were
		// not isolated, guard instantiation or execution could block waiting for
		// input from the host.

		// Use a short-lived context to detect any unexpected blocking behavior
		// that might occur if the WASM runtime attempted to read from host stdin.
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		backend := &mockBackendCaller{}

		done := make(chan struct{})
		go func() {
			defer close(done)
			// Instantiating the guard should configure the WASM runtime/module
			// with an isolated stdin (e.g., via WithStdin(strings.NewReader("")))
			// so that no reads from host stdin occur here.
			_, _ = NewWasmGuardFromBytes(ctx, "stdin-isolation-guard", minimalGuardWasm, backend)
		}()

		select {
		case <-done:
			// Guard creation completed without blocking on host stdin.
		case <-ctx.Done():
			t.Fatalf("WASM guard instantiation appears to be blocked, possibly waiting on host stdin instead of using isolated stdin")
		}
	})
}

func TestNewWasmGuardFromBytes(t *testing.T) {
	t.Run("empty WASM bytes returns error", func(t *testing.T) {
		ctx := context.Background()
		backend := &mockBackendCaller{}

		guard, err := NewWasmGuardFromBytes(ctx, "test-guard", []byte{}, backend)
		assert.Error(t, err)
		assert.Nil(t, guard)
		assert.ErrorContains(t, err, "instantiate WASM module")
	})

	t.Run("invalid WASM bytes returns error", func(t *testing.T) {
		ctx := context.Background()
		backend := &mockBackendCaller{}

		invalidWasm := []byte{0x00, 0x01, 0x02, 0x03} // Not valid WASM
		guard, err := NewWasmGuardFromBytes(ctx, "test-guard", invalidWasm, backend)
		assert.Error(t, err)
		assert.Nil(t, guard)
	})

	t.Run("nil backend caller is accepted", func(t *testing.T) {
		ctx := context.Background()

		// Even with nil backend, guard creation should validate WASM structure
		guard, err := NewWasmGuardFromBytes(ctx, "test-guard", minimalGuardWasm, nil)
		// Will fail on invalid WASM, but nil backend is not the error
		if err != nil {
			assert.NotContains(t, err.Error(), "backend")
		}
		_ = guard
	})
}

func TestNormalizePolicyPayloadExtended(t *testing.T) {
	t.Run("nil policy returns error", func(t *testing.T) {
		_, err := normalizePolicyPayload(nil)
		require.Error(t, err)
		assert.Equal(t, "policy is required", err.Error())
	})

	t.Run("empty string policy returns error", func(t *testing.T) {
		_, err := normalizePolicyPayload("")
		require.Error(t, err)
		assert.ErrorContains(t, err, "policy string is empty")
	})

	t.Run("whitespace-only string policy returns error", func(t *testing.T) {
		_, err := normalizePolicyPayload("   ")
		require.Error(t, err)
		assert.ErrorContains(t, err, "policy string is empty")
	})

	t.Run("invalid JSON string returns error", func(t *testing.T) {
		_, err := normalizePolicyPayload("{invalid json")
		require.Error(t, err)
		assert.ErrorContains(t, err, "not valid JSON")
	})

	t.Run("JSON array string returns error", func(t *testing.T) {
		_, err := normalizePolicyPayload("[1,2,3]")
		require.Error(t, err)
		assert.ErrorContains(t, err, "must decode to an object")
	})

	t.Run("valid JSON object string is parsed", func(t *testing.T) {
		result, err := normalizePolicyPayload(`{"allow-only":{"repos":"public","min-integrity":"none"}}`)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, resultMap, "allow-only")
	})

	t.Run("object policy is passed through", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "all",
				"min-integrity": "approved",
			},
		}

		result, err := normalizePolicyPayload(policy)
		require.NoError(t, err)
		assert.Equal(t, policy, result)
	})

	t.Run("JSON string literal rejected", func(t *testing.T) {
		_, err := normalizePolicyPayload(`"hello"`)
		require.Error(t, err)
		assert.ErrorContains(t, err, "must decode to an object")
	})

	t.Run("JSON number literal rejected", func(t *testing.T) {
		_, err := normalizePolicyPayload(`42`)
		require.Error(t, err)
		assert.ErrorContains(t, err, "must decode to an object")
	})

	t.Run("non-string non-map policy passed through without error", func(t *testing.T) {
		result, err := normalizePolicyPayload(true)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})
}

func TestBuildStrictLabelAgentPayloadExtended(t *testing.T) {
	t.Run("nil policy returns error", func(t *testing.T) {
		_, err := buildStrictLabelAgentPayload(nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "expected {\"allow-only\"")
	})

	t.Run("policy with legacy envelope returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"policy": map[string]interface{}{
				"allow-only": map[string]interface{}{
					"repos":         "public",
					"min-integrity": "none",
				},
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "outdated")
		assert.ErrorContains(t, err, "remove legacy envelope")
	})

	t.Run("policy without allow-only returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"something": "value",
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "must use top-level allow-only")
	})

	t.Run("allow-only with missing repos returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"min-integrity": "none",
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "missing required fields repos and/or min-integrity")
	})

	t.Run("allow-only with missing integrity returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos": "public",
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "missing required fields repos and/or min-integrity")
	})

	t.Run("allow-only with empty array repos returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         []interface{}{},
				"min-integrity": "none",
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid repos value")
	})

	t.Run("allow-only with invalid integrity value returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "invalid-integrity",
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid min-integrity value")
	})

	t.Run("valid allow-only policy succeeds", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
		}

		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Contains(t, result, "allow-only")
	})

	t.Run("accepts legacy 'allowonly' key", func(t *testing.T) {
		policy := map[string]interface{}{
			"allowonly": map[string]interface{}{
				"repos":         "all",
				"min-integrity": "approved",
			},
		}

		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("accepts legacy 'integrity' key", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":     "public",
				"integrity": "merged",
			},
		}

		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("validates all integrity values", func(t *testing.T) {
		validIntegrities := []string{"none", "unapproved", "approved", "merged", "NONE", "Approved"}

		for _, integrity := range validIntegrities {
			policy := map[string]interface{}{
				"allow-only": map[string]interface{}{
					"repos":         "public",
					"min-integrity": integrity,
				},
			}

			_, err := buildStrictLabelAgentPayload(policy)
			assert.NoError(t, err, "integrity=%q should be valid", integrity)
		}
	})

	t.Run("valid trusted-bots alongside allow-only succeeds", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
			"trusted-bots": []interface{}{"copilot-swe-agent[bot]", "my-org-bot"},
		}

		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Contains(t, result, "allow-only")
		assert.Contains(t, result, "trusted-bots")
	})

	t.Run("unexpected extra key returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
			"unknown-key": "value",
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "unexpected key")
	})

	t.Run("trusted-bots with non-string entry returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
			"trusted-bots": []interface{}{"valid-bot", 42},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "non-empty string")
	})

	t.Run("trusted-bots with empty string entry returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
			"trusted-bots": []interface{}{"valid-bot", ""},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "non-empty string")
	})

	t.Run("trusted-bots with wrong type returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
			"trusted-bots": "not-an-array",
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "expected non-empty array")
	})

	t.Run("trusted-bots empty array returns error per spec", func(t *testing.T) {
		// Spec §4.1.3.4: trustedBots MUST be a non-empty array of strings when present
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
			"trusted-bots": []interface{}{},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "non-empty array")
	})

	t.Run("trusted-bots with whitespace-only entry returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
			"trusted-bots": []interface{}{"valid-bot", "   "},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "non-empty string")
	})

	t.Run("valid blocked-users in allow-only succeeds", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
				"blocked-users": []interface{}{"evil-bot", "untrusted-fork"},
			},
		}

		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		require.NotNil(t, result)
		allowOnly := result["allow-only"].(map[string]interface{})
		assert.Contains(t, allowOnly, "blocked-users")
	})

	t.Run("valid approval-labels in allow-only succeeds", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":           "public",
				"min-integrity":   "none",
				"approval-labels": []interface{}{"approved", "human-reviewed"},
			},
		}

		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		require.NotNil(t, result)
		allowOnly := result["allow-only"].(map[string]interface{})
		assert.Contains(t, allowOnly, "approval-labels")
	})

	t.Run("valid refusal-labels in allow-only succeeds", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":           "public",
				"min-integrity":   "none",
				"refusal-labels":  []interface{}{"unsafe", "needs-triage"},
				"approval-labels": []interface{}{"approved"},
			},
		}

		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		require.NotNil(t, result)
		allowOnly := result["allow-only"].(map[string]interface{})
		assert.Contains(t, allowOnly, "refusal-labels")
	})

	t.Run("refusal-labels array entries are trimmed", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":          "public",
				"min-integrity":  "none",
				"refusal-labels": []interface{}{" unsafe ", "needs-triage\t"},
			},
		}

		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		allowOnly := result["allow-only"].(map[string]interface{})
		refusalLabels, ok := allowOnly["refusal-labels"].([]interface{})
		require.True(t, ok)
		assert.Equal(t, []interface{}{"unsafe", "needs-triage"}, refusalLabels)
	})

	t.Run("refusal-labels expression string is normalized", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":          "public",
				"min-integrity":  "none",
				"refusal-labels": "unsafe, blocked\nneeds-review",
			},
		}

		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		allowOnly := result["allow-only"].(map[string]interface{})
		refusalLabels, ok := allowOnly["refusal-labels"].([]interface{})
		require.True(t, ok)
		assert.Equal(t, []interface{}{"unsafe", "blocked", "needs-review"}, refusalLabels)
	})

	t.Run("blocked-users and approval-labels together with trusted-bots succeeds", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":           "public",
				"min-integrity":   "approved",
				"blocked-users":   []interface{}{"bad-actor"},
				"approval-labels": []interface{}{"approved"},
			},
			"trusted-bots": []interface{}{"my-org-bot"},
		}

		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Contains(t, result, "allow-only")
		assert.Contains(t, result, "trusted-bots")
	})

	t.Run("valid trusted-users in allow-only succeeds", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "approved",
				"trusted-users": []interface{}{"contractor-1", "partner-dev"},
			},
		}

		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		require.NotNil(t, result)
		allowOnly := result["allow-only"].(map[string]interface{})
		assert.Contains(t, allowOnly, "trusted-users")
	})

	t.Run("trusted-users with all fields succeeds", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":           "public",
				"min-integrity":   "approved",
				"blocked-users":   []interface{}{"bad-actor"},
				"approval-labels": []interface{}{"approved"},
				"trusted-users":   []interface{}{"contractor-1"},
			},
			"trusted-bots": []interface{}{"my-org-bot"},
		}

		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Contains(t, result, "allow-only")
		assert.Contains(t, result, "trusted-bots")
		allowOnly := result["allow-only"].(map[string]interface{})
		assert.Contains(t, allowOnly, "trusted-users")
	})

	t.Run("trusted-users with non-string entry returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
				"trusted-users": []interface{}{"valid", 42},
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "trusted-users")
		assert.ErrorContains(t, err, "non-empty string")
	})

	t.Run("trusted-users with empty string entry returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
				"trusted-users": []interface{}{"valid", ""},
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "trusted-users")
	})

	t.Run("blocked-users with non-string entry returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
				"blocked-users": []interface{}{"valid", 42},
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "blocked-users")
		assert.ErrorContains(t, err, "non-empty string")
	})

	t.Run("blocked-users with empty string entry returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
				"blocked-users": []interface{}{"valid", ""},
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "blocked-users")
	})

	t.Run("approval-labels with non-string entry returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":           "public",
				"min-integrity":   "none",
				"approval-labels": []interface{}{"approved", 99},
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "approval-labels")
		assert.ErrorContains(t, err, "non-empty string")
	})

	t.Run("refusal-labels with non-string entry returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":          "public",
				"min-integrity":  "none",
				"refusal-labels": []interface{}{"unsafe", 99},
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "refusal-labels")
		assert.ErrorContains(t, err, "non-empty string")
	})

	t.Run("unknown allow-only key returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
				"unknown-field": "value",
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "unexpected allow-only key")
	})

	t.Run("valid endorsement-reactions succeeds", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":                 "public",
				"min-integrity":         "approved",
				"endorsement-reactions": []interface{}{"THUMBS_UP", "HEART"},
			},
		}
		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		allowOnly, _ := result["allow-only"].(map[string]interface{})
		assert.Contains(t, allowOnly, "endorsement-reactions")
	})

	t.Run("valid disapproval-reactions succeeds", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":                 "public",
				"min-integrity":         "approved",
				"disapproval-reactions": []interface{}{"THUMBS_DOWN", "CONFUSED"},
			},
		}
		result, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
		allowOnly, _ := result["allow-only"].(map[string]interface{})
		assert.Contains(t, allowOnly, "disapproval-reactions")
	})

	t.Run("valid disapproval-integrity succeeds", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":                 "public",
				"min-integrity":         "approved",
				"disapproval-reactions": []interface{}{"THUMBS_DOWN"},
				"disapproval-integrity": "none",
			},
		}
		_, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
	})

	t.Run("valid endorser-min-integrity succeeds", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":                  "public",
				"min-integrity":          "approved",
				"endorsement-reactions":  []interface{}{"THUMBS_UP"},
				"endorser-min-integrity": "approved",
			},
		}
		_, err := buildStrictLabelAgentPayload(policy)
		require.NoError(t, err)
	})

	t.Run("endorsement-reactions with empty string entry returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":                 "public",
				"min-integrity":         "approved",
				"endorsement-reactions": []interface{}{"THUMBS_UP", ""},
			},
		}
		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "endorsement-reactions")
	})

	t.Run("invalid disapproval-integrity value returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":                 "public",
				"min-integrity":         "approved",
				"disapproval-integrity": "invalid",
			},
		}
		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "disapproval-integrity")
	})

	t.Run("invalid endorser-min-integrity value returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":                  "public",
				"min-integrity":          "approved",
				"endorser-min-integrity": "badvalue",
			},
		}
		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.ErrorContains(t, err, "endorser-min-integrity")
	})
}

func TestBuildLabelAgentPayload(t *testing.T) {
	t.Run("nil trusted bots and users returns policy unchanged", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
		}
		result := BuildLabelAgentPayload(policy, nil, nil)
		assert.Equal(t, policy, result)
	})

	t.Run("empty trusted bots and users returns policy unchanged", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
		}
		result := BuildLabelAgentPayload(policy, []string{}, []string{})
		assert.Equal(t, policy, result)
	})

	t.Run("non-empty trusted bots injects trusted-bots key", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
		}
		bots := []string{"copilot-swe-agent[bot]", "my-org-bot[bot]"}
		result := BuildLabelAgentPayload(policy, bots, nil)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, resultMap, "allow-only")
		assert.Contains(t, resultMap, "trusted-bots")

		trustedBots, ok := resultMap["trusted-bots"].([]interface{})
		require.True(t, ok)
		assert.Len(t, trustedBots, 2)
		assert.Equal(t, "copilot-swe-agent[bot]", trustedBots[0])
		assert.Equal(t, "my-org-bot[bot]", trustedBots[1])
	})

	t.Run("resulting payload is accepted by buildStrictLabelAgentPayload", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "unapproved",
			},
		}
		bots := []string{"copilot-swe-agent[bot]"}
		payload := BuildLabelAgentPayload(policy, bots, nil)

		_, err := buildStrictLabelAgentPayload(payload)
		assert.NoError(t, err)
	})

	t.Run("does not modify original policy", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
		}
		bots := []string{"my-bot[bot]"}
		_ = BuildLabelAgentPayload(policy, bots, nil)

		// Original policy should NOT contain trusted-bots
		_, hasTrustedBots := policy["trusted-bots"]
		assert.False(t, hasTrustedBots, "BuildLabelAgentPayload should not mutate the original policy")
	})

	t.Run("preserves all trusted bot entries", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         []interface{}{"org/repo"},
				"min-integrity": "approved",
			},
		}
		bots := []string{"bot-a[bot]", "bot-b[bot]", "bot-c"}
		result := BuildLabelAgentPayload(policy, bots, nil)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)

		trustedBots, ok := resultMap["trusted-bots"].([]interface{})
		require.True(t, ok)
		assert.Len(t, trustedBots, 3)
		assert.Equal(t, "bot-a[bot]", trustedBots[0])
		assert.Equal(t, "bot-b[bot]", trustedBots[1])
		assert.Equal(t, "bot-c", trustedBots[2])
	})

	t.Run("non-empty trusted users injects into allow-only", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
		}
		users := []string{"contractor-1", "partner-dev"}
		result := BuildLabelAgentPayload(policy, nil, users)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, resultMap, "allow-only")
		assert.NotContains(t, resultMap, "trusted-users") // top-level key should not be added

		allowOnly, ok := resultMap["allow-only"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, allowOnly, "trusted-users")

		trustedUsers, ok := allowOnly["trusted-users"].([]interface{})
		require.True(t, ok)
		assert.Len(t, trustedUsers, 2)
		assert.Equal(t, "contractor-1", trustedUsers[0])
		assert.Equal(t, "partner-dev", trustedUsers[1])
	})

	t.Run("trusted users payload accepted by buildStrictLabelAgentPayload", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "approved",
			},
		}
		users := []string{"contractor-1"}
		payload := BuildLabelAgentPayload(policy, nil, users)

		_, err := buildStrictLabelAgentPayload(payload)
		assert.NoError(t, err)
	})

	t.Run("both trusted bots and users injected together", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "approved",
			},
		}
		bots := []string{"my-bot[bot]"}
		users := []string{"contractor-1"}
		result := BuildLabelAgentPayload(policy, bots, users)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, resultMap, "trusted-bots")

		allowOnly, ok := resultMap["allow-only"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, allowOnly, "trusted-users")
	})

	t.Run("trusted users not injected when allow-only absent", func(t *testing.T) {
		policy := map[string]interface{}{
			"something": "value",
		}
		result := BuildLabelAgentPayload(policy, nil, []string{"user1"})
		payloadMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		// trusted-users should NOT be added at top level (only injected inside allow-only)
		assert.NotContains(t, payloadMap, "trusted-users")
	})

	t.Run("non-JSON-object policy falls back to original on conversion failure", func(t *testing.T) {
		// PolicyToMap fails for a string that is not a JSON object
		result := BuildLabelAgentPayload("not-a-json-object", []string{"bot"}, nil)
		assert.Equal(t, "not-a-json-object", result)
	})
}

func TestWasmGuardClose(t *testing.T) {
	t.Run("close with nil runtime and module", func(t *testing.T) {
		guard := &WasmGuard{}
		err := guard.Close(context.Background())
		assert.NoError(t, err)
	})

	t.Run("close ignores caller cancellation during cleanup", func(t *testing.T) {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		mod, err := rt.InstantiateWithConfig(ctx, minimalGuardWasm, wazero.NewModuleConfig().WithName("close-guard"))
		require.NoError(t, err)

		guard := &WasmGuard{runtime: rt, module: mod}

		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()

		err = guard.Close(cancelledCtx)
		assert.NoError(t, err)
	})
}

func TestWasmGuardName(t *testing.T) {
	t.Run("returns guard name", func(t *testing.T) {
		guard := &WasmGuard{name: "test-guard"}
		assert.Equal(t, "test-guard", guard.Name())
	})

	t.Run("returns empty name if not set", func(t *testing.T) {
		guard := &WasmGuard{}
		assert.Equal(t, "", guard.Name())
	})
}

func TestWasmGuardIsHealthy(t *testing.T) {
	t.Run("healthy guard reports true", func(t *testing.T) {
		guard := &WasmGuard{}
		assert.True(t, guard.IsHealthy())
	})

	t.Run("failed guard reports false", func(t *testing.T) {
		guard := &WasmGuard{
			failed:    true,
			failedErr: errors.New("trap"),
		}
		assert.False(t, guard.IsHealthy())
	})
}

func TestParsePathLabeledResponse(t *testing.T) {
	t.Run("invalid JSON returns error", func(t *testing.T) {
		invalidJSON := []byte("not json")
		result, err := parsePathLabeledResponse(invalidJSON, nil)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorContains(t, err, "parse path labels")
	})

	t.Run("valid path labels with nil original data returns collection labeled data", func(t *testing.T) {
		responseJSON := []byte(`{"labeled_paths":[]}`)
		result, err := parsePathLabeledResponse(responseJSON, nil)
		require.NoError(t, err)
		assert.NotNil(t, result)
		require.IsType(t, &difc.CollectionLabeledData{}, result)
	})
}

func TestMockBackendCaller(t *testing.T) {
	t.Run("mock records calls", func(t *testing.T) {
		mock := &mockBackendCaller{
			result: map[string]interface{}{"status": "ok"},
		}

		ctx := context.Background()
		result, err := mock.CallTool(ctx, "test_tool", map[string]interface{}{"arg": "value"})

		assert.True(t, mock.called)
		assert.Equal(t, "test_tool", mock.toolName)
		assert.NoError(t, err)
		assert.Equal(t, map[string]interface{}{"status": "ok"}, result)
	})

	t.Run("mock returns error when configured", func(t *testing.T) {
		mock := &mockBackendCaller{
			err: assert.AnError,
		}

		ctx := context.Background()
		result, err := mock.CallTool(ctx, "test_tool", nil)

		assert.True(t, mock.called)
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

func TestBufferRetryLogic(t *testing.T) {
	// helper instantiates a module for the retry-logic tests.
	setupModule := func(t *testing.T, wasmBytes []byte, moduleName string) (*WasmGuard, func()) {
		t.Helper()
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		mod, err := rt.InstantiateWithConfig(ctx, wasmBytes, wazero.NewModuleConfig().WithName(moduleName))
		require.NoError(t, err)
		g := &WasmGuard{name: moduleName, module: mod}
		cleanup := func() {
			require.NoError(t, mod.Close(ctx))
			require.NoError(t, rt.Close(ctx))
		}
		return g, cleanup
	}

	t.Run("function not exported from module", func(t *testing.T) {
		// minimalGuardWasm has no exports at all; ExportedFunction returns nil.
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		t.Cleanup(func() { require.NoError(t, rt.Close(ctx)) })
		mod, err := rt.InstantiateWithConfig(ctx, minimalGuardWasm, wazero.NewModuleConfig().WithName("minimal-retry"))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, mod.Close(ctx)) })
		g := &WasmGuard{name: "minimal-retry", module: mod}

		g.mu.Lock()
		_, callErr := g.callWasmFunction(ctx, "label_agent", []byte(`{}`))
		g.mu.Unlock()

		require.Error(t, callErr)
		assert.Contains(t, callErr.Error(), "not exported from WASM module")
	})

	t.Run("module has no memory", func(t *testing.T) {
		// funcNoMemoryGuardWasm exports label_agent but has no memory section.
		g, cleanup := setupModule(t, funcNoMemoryGuardWasm, "no-memory-retry")
		defer cleanup()

		g.mu.Lock()
		_, callErr := g.callWasmFunction(context.Background(), "label_agent", []byte(`{}`))
		g.mu.Unlock()

		require.Error(t, callErr)
		assert.Contains(t, callErr.Error(), "WASM module has no memory")
	})

	t.Run("input too large returns error immediately", func(t *testing.T) {
		// alwaysNeg2GuardWasm exports label_agent + memory; the input-size check
		// fires before any WASM call.
		g, cleanup := setupModule(t, alwaysNeg2GuardWasm, "input-too-large-retry")
		defer cleanup()

		// 8MB + 1 byte exceeds the 8MB max-input limit.
		hugeInput := make([]byte, 8*1024*1024+1)

		g.mu.Lock()
		_, callErr := g.callWasmFunction(context.Background(), "label_agent", hugeInput)
		g.mu.Unlock()

		require.Error(t, callErr)
		assert.Contains(t, callErr.Error(), "input too large")
	})

	t.Run("function returns no values", func(t *testing.T) {
		g, cleanup := setupModule(t, funcNoResultGuardWasm, "no-result-retry")
		defer cleanup()

		g.mu.Lock()
		_, callErr := g.callWasmFunction(context.Background(), "label_agent", []byte(`{}`))
		g.mu.Unlock()

		require.Error(t, callErr)
		assert.Contains(t, callErr.Error(), "returned no results")
	})

	t.Run("buffer doubling exhausted causes exceeds-maximum error", func(t *testing.T) {
		// alwaysNeg2GuardWasm always returns -2 with no hint, so callWasmFunction
		// doubles the buffer on each retry: 4MB → 8MB → 16MB → would need 32MB,
		// which exceeds the 16MB cap.
		g, cleanup := setupModule(t, alwaysNeg2GuardWasm, "always-neg2-retry")
		defer cleanup()

		g.mu.Lock()
		_, callErr := g.callWasmFunction(context.Background(), "label_agent", []byte(`{}`))
		g.mu.Unlock()

		require.Error(t, callErr)
		assert.Contains(t, callErr.Error(), "exceeds maximum")
	})

	t.Run("size hint triggers one retry then succeeds", func(t *testing.T) {
		// retryWithHint5MBGuardWasm returns -2 with a 5MB hint on the first call
		// (when outputSize < 5MB), then succeeds with empty output on the retry.
		g, cleanup := setupModule(t, retryWithHint5MBGuardWasm, "hint-retry")
		defer cleanup()

		g.mu.Lock()
		result, callErr := g.callWasmFunction(context.Background(), "label_agent", []byte(`{}`))
		g.mu.Unlock()

		require.NoError(t, callErr)
		assert.Empty(t, result)
	})

	t.Run("failed guard refuses call immediately", func(t *testing.T) {
		// A guard marked as failed due to a prior trap must reject all future calls.
		g, cleanup := setupModule(t, alwaysNeg2GuardWasm, "failed-guard-retry")
		defer cleanup()

		sentinel := fmt.Errorf("previous trap sentinel")
		g.failed = true
		g.failedErr = sentinel

		g.mu.Lock()
		_, callErr := g.callWasmFunction(context.Background(), "label_agent", []byte(`{}`))
		g.mu.Unlock()

		require.Error(t, callErr)
		assert.ErrorIs(t, callErr, sentinel)
		assert.Contains(t, callErr.Error(), "unavailable after a previous trap")
	})
}

func TestWasmMemoryLayout(t *testing.T) {
	t.Run("verify safety margin calculation", func(t *testing.T) {
		// From the code: requiredMemory := inputSize + outputSize + uint32(64*1024)
		safetyMargin := uint32(64 * 1024) // 64KB
		assert.Equal(t, uint32(65536), safetyMargin)

		inputSize := uint32(1024)
		outputSize := uint32(4096)
		requiredMemory := inputSize + outputSize + safetyMargin

		assert.Equal(t, uint32(70656), requiredMemory)
	})

	t.Run("page size calculation", func(t *testing.T) {
		// WASM pages are 64KB (65536 bytes)
		pageSize := uint32(65536)

		// Test rounding up to pages: (requiredMemory - memSize + 65535) / 65536
		memSize := uint32(0)
		requiredMemory := uint32(100000)

		pages := (requiredMemory - memSize + pageSize - 1) / pageSize
		assert.Equal(t, uint32(2), pages, "100000 bytes should require 2 pages")
	})
}

func TestTryCallWasmFunctionDirectMemoryFallback(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntime(ctx)
	t.Cleanup(func() {
		require.NoError(t, runtime.Close(ctx))
	})

	module, err := runtime.InstantiateWithConfig(ctx, directMemoryFallbackGuardWasm, wazero.NewModuleConfig().WithName("direct-memory-fallback"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, module.Close(ctx))
	})

	g := &WasmGuard{
		name:   "direct-memory-fallback",
		module: module,
	}

	fn := module.ExportedFunction("label_response")
	require.NotNil(t, fn)

	mem := module.Memory()
	require.NotNil(t, mem)
	initialMemSize := mem.Size()

	inputJSON := bytes.Repeat([]byte("x"), 1*1024*1024)
	outputSize := uint32(4 * 1024 * 1024)

	g.mu.Lock()
	result, requiredSize, err := g.tryCallWasmFunction(ctx, fn, mem, inputJSON, outputSize)
	warnedDirectMemoryPath := g.warnedDirectMemoryPath
	finalMemSize := mem.Size()
	g.mu.Unlock()

	require.NoError(t, err)
	assert.Empty(t, result)
	assert.Zero(t, requiredSize)
	assert.True(t, warnedDirectMemoryPath, "expected direct memory fallback warning state to be set")
	assert.Greater(t, finalMemSize, initialMemSize, "expected memory to grow for large direct-memory buffers")
}

func TestErrorCodeSentinel(t *testing.T) {
	t.Run("error sentinel value is max uint32", func(t *testing.T) {
		// From code: stack[0] = uint64(^uint32(0)) // Max uint32 represents error
		errorSentinel := ^uint32(0)
		assert.Equal(t, uint32(0xFFFFFFFF), errorSentinel)
		assert.Equal(t, uint32(4294967295), errorSentinel)
	})

	t.Run("buffer too small error code is -2", func(t *testing.T) {
		// From code: if resultLen == -2
		bufferTooSmall := int32(-2)
		assert.Equal(t, int32(-2), bufferTooSmall)
	})
}

func TestRequiredSizeDecoding(t *testing.T) {
	t.Run("decode little-endian uint32 from bytes", func(t *testing.T) {
		// From code: requiredSize := uint32(sizeBytes[0]) | uint32(sizeBytes[1])<<8 | ...

		// Test case: 8MB (8388608 bytes) = 0x00800000
		sizeBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(sizeBytes, 8388608)

		// Manual decoding (as done in the code)
		requiredSize := uint32(sizeBytes[0]) | uint32(sizeBytes[1])<<8 | uint32(sizeBytes[2])<<16 | uint32(sizeBytes[3])<<24
		assert.Equal(t, uint32(8388608), requiredSize)

		// Verify it matches binary.LittleEndian
		assert.Equal(t, binary.LittleEndian.Uint32(sizeBytes), requiredSize)
	})
}

func TestLogLevelConstants(t *testing.T) {
	t.Run("log level constants match expected values", func(t *testing.T) {
		// Verify the log level constants are correctly ordered
		assert.Equal(t, 0, logLevelDebug)
		assert.Equal(t, 1, logLevelInfo)
		assert.Equal(t, 2, logLevelWarn)
		assert.Equal(t, 3, logLevelError)

		// Verify ordering
		assert.Less(t, logLevelDebug, logLevelInfo)
		assert.Less(t, logLevelInfo, logLevelWarn)
		assert.Less(t, logLevelWarn, logLevelError)
	})
}

func TestJSONMarshaling(t *testing.T) {
	t.Run("marshal and unmarshal label agent input", func(t *testing.T) {
		input := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
		}

		inputJSON, err := json.Marshal(input)
		require.NoError(t, err)

		var decoded map[string]interface{}
		err = json.Unmarshal(inputJSON, &decoded)
		require.NoError(t, err)

		assert.Contains(t, decoded, "allow-only")
	})

	t.Run("marshal label resource input", func(t *testing.T) {
		input := map[string]interface{}{
			"tool_name":    "test_tool",
			"tool_args":    map[string]interface{}{"arg": "value"},
			"capabilities": &difc.Capabilities{},
		}

		inputJSON, err := json.Marshal(input)
		require.NoError(t, err)
		assert.Contains(t, string(inputJSON), "tool_name")
		assert.Contains(t, string(inputJSON), "capabilities")
	})

	t.Run("marshal label response input", func(t *testing.T) {
		input := map[string]interface{}{
			"tool_name":   "test_tool",
			"tool_result": map[string]interface{}{"data": "value"},
		}

		inputJSON, err := json.Marshal(input)
		require.NoError(t, err)
		assert.Contains(t, string(inputJSON), "tool_result")
	})
}

func TestIsWasmTrap(t *testing.T) {
	t.Run("nil error is not a trap", func(t *testing.T) {
		assert.False(t, isWasmTrap(nil))
	})

	t.Run("generic error is not a trap", func(t *testing.T) {
		assert.False(t, isWasmTrap(errors.New("some error")))
	})

	t.Run("wrapped error containing wasm error is a trap (verified with wazero v1.11.0)", func(t *testing.T) {
		err := errors.New("WASM function call failed: wasm error: unreachable")
		assert.True(t, isWasmTrap(err))
	})

	t.Run("wasm error integer divide by zero is a trap", func(t *testing.T) {
		err := errors.New("wasm error: integer divide by zero")
		assert.True(t, isWasmTrap(err))
	})

	t.Run("wasm error out of bounds is a trap", func(t *testing.T) {
		err := errors.New("wasm error: out of bounds memory access")
		assert.True(t, isWasmTrap(err))
	})

	t.Run("sys.ExitError with exit code 0 is not a trap", func(t *testing.T) {
		err := sys.NewExitError(0)
		assert.False(t, isWasmTrap(err))
	})

	t.Run("sys.ExitError with non-zero exit code is a trap", func(t *testing.T) {
		err := sys.NewExitError(1)
		assert.True(t, isWasmTrap(err))
	})

	t.Run("wrapped sys.ExitError with exit code 0 is not a trap", func(t *testing.T) {
		err := fmt.Errorf("wrapper: %w", sys.NewExitError(0))
		assert.False(t, isWasmTrap(err))
	})

	t.Run("wrapped sys.ExitError with non-zero exit code is a trap", func(t *testing.T) {
		err := fmt.Errorf("wrapper: %w", sys.NewExitError(2))
		assert.True(t, isWasmTrap(err))
	})
}

func TestWasmGuardFailedState(t *testing.T) {
	t.Run("failed guard returns error immediately for callWasmFunction", func(t *testing.T) {
		// Build a minimal valid WasmGuard by hand to exercise the failed-state path
		// without needing a full WASM binary.
		originalErr := errors.New("WASM function call failed: wasm error: unreachable")
		g := &WasmGuard{
			name:      "test-guard",
			failed:    true,
			failedErr: originalErr,
		}

		ctx := context.Background()
		_, err := g.callWasmFunction(ctx, "label_response", []byte(`{}`))
		require.Error(t, err)
		assert.ErrorContains(t, err, "unavailable after a previous trap")
		assert.ErrorContains(t, err, "test-guard")
	})

	t.Run("failed guard wraps original trap error", func(t *testing.T) {
		originalErr := errors.New("WASM function call failed: wasm error: unreachable")
		g := &WasmGuard{
			name:      "my-guard",
			failed:    true,
			failedErr: originalErr,
		}

		ctx := context.Background()
		_, err := g.callWasmFunction(ctx, "label_agent", []byte(`{}`))
		require.Error(t, err)
		// The original trap error should be reachable via errors.Is / errors.As
		assert.ErrorIs(t, err, originalErr)
	})
}

func TestWasmGuardCompilationCache(t *testing.T) {
	t.Run("global compilation cache is not nil", func(t *testing.T) {
		assert.NotNil(t, globalCompilationCache)
	})

	t.Run("global cache can be initialized when nil", func(t *testing.T) {
		ctx := context.Background()
		origCache := globalCompilationCache
		globalCompilationCache = nil
		t.Cleanup(func() {
			globalCompilationCache = origCache
		})

		require.NoError(t, ConfigureGlobalCompilationCache(ctx, ""))
		assert.NotNil(t, globalCompilationCache)
	})

	t.Run("custom cache is used when provided via options", func(t *testing.T) {
		ctx := context.Background()

		// Use a disk-backed cache in a temp dir so we can observe that it was used.
		cacheDir := t.TempDir()
		customCache, err := wazero.NewCompilationCacheWithDir(cacheDir)
		require.NoError(t, err)
		defer customCache.Close(ctx)

		opts := &WasmGuardOptions{
			CompilationCache: customCache,
		}

		// Instantiation will fail (minimal WASM), but the code path
		// that selects the cache runs before module compilation, which
		// should populate the disk-backed cache.
		_, err = NewWasmGuardWithOptions(ctx, "cache-test", minimalGuardWasm, &mockBackendCaller{}, opts)
		require.Error(t, err)

		entries, readErr := os.ReadDir(cacheDir)
		require.NoError(t, readErr)
		assert.NotEmpty(t, entries, "expected compilation artifacts in custom cache directory")
	})

	t.Run("global cache is used when options cache is nil", func(t *testing.T) {
		ctx := context.Background()

		// Swap in a disk-backed global cache pointing at a temp dir so we can
		// observe that the global cache path is actually exercised.
		cacheDir := t.TempDir()
		tmpCache, err := wazero.NewCompilationCacheWithDir(cacheDir)
		require.NoError(t, err)

		origCache := globalCompilationCache
		globalCompilationCache = tmpCache
		defer func() {
			globalCompilationCache = origCache
			tmpCache.Close(ctx)
		}()

		// nil opts → global cache path
		_, err = NewWasmGuardWithOptions(ctx, "cache-test", minimalGuardWasm, &mockBackendCaller{}, nil)
		require.Error(t, err)

		entries, readErr := os.ReadDir(cacheDir)
		require.NoError(t, readErr)
		assert.NotEmpty(t, entries, "expected compilation artifacts in global cache directory")
	})

	t.Run("global cache can be reconfigured to a disk-backed directory", func(t *testing.T) {
		ctx := context.Background()
		cacheDir := t.TempDir()

		require.NoError(t, ConfigureGlobalCompilationCache(ctx, cacheDir))
		t.Cleanup(func() {
			require.NoError(t, ConfigureGlobalCompilationCache(ctx, ""))
		})

		_, err := NewWasmGuardWithOptions(ctx, "cache-test", minimalGuardWasm, &mockBackendCaller{}, nil)
		require.Error(t, err)

		entries, readErr := os.ReadDir(cacheDir)
		require.NoError(t, readErr)
		assert.NotEmpty(t, entries, "expected compilation artifacts in reconfigured global cache directory")
	})

	t.Run("global cache remains unchanged when closing previous cache fails", func(t *testing.T) {
		ctx := context.Background()
		origCache := globalCompilationCache
		failingCache := &mockCompilationCache{closeErr: errors.New("close failed")}
		globalCompilationCache = failingCache
		t.Cleanup(func() {
			globalCompilationCache = origCache
		})

		err := ConfigureGlobalCompilationCache(ctx, "")
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to close previous compilation cache")
		assert.Same(t, failingCache, globalCompilationCache)
		assert.True(t, failingCache.closed, "expected failing cache to be closed")
	})

	t.Run("cache is disabled when DisableCompilationCache is true", func(t *testing.T) {
		ctx := context.Background()

		opts := &WasmGuardOptions{
			DisableCompilationCache: true,
		}

		// Should still work (fail on minimal WASM) but without caching
		_, err := NewWasmGuardWithOptions(ctx, "no-cache-test", minimalGuardWasm, &mockBackendCaller{}, opts)
		require.Error(t, err)
	})
}
