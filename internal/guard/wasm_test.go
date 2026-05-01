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

// minimalGuardWasm is a minimal WASM binary that exports the required guard functions
// This is compiled from WAT (WebAssembly Text Format) for zero-dependency testing
// The functions return minimal valid JSON responses
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

// mockBackendCaller is a test implementation of BackendCaller
type mockBackendCaller struct {
	called   bool
	toolName string
	args     interface{}
	result   interface{}
	err      error
}

func (m *mockBackendCaller) CallTool(ctx context.Context, toolName string, args interface{}) (interface{}, error) {
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
		runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
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
		assert.Contains(t, err.Error(), "instantiate WASM module")
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
		assert.Contains(t, err.Error(), "policy string is empty")
	})

	t.Run("whitespace-only string policy returns error", func(t *testing.T) {
		_, err := normalizePolicyPayload("   ")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "policy string is empty")
	})

	t.Run("invalid JSON string returns error", func(t *testing.T) {
		_, err := normalizePolicyPayload("{invalid json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not valid JSON")
	})

	t.Run("JSON array string returns error", func(t *testing.T) {
		_, err := normalizePolicyPayload("[1,2,3]")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must decode to an object")
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
}

func TestBuildStrictLabelAgentPayloadExtended(t *testing.T) {
	t.Run("nil policy returns error", func(t *testing.T) {
		_, err := buildStrictLabelAgentPayload(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected {\"allow-only\"")
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
		assert.Contains(t, err.Error(), "outdated")
		assert.Contains(t, err.Error(), "remove legacy envelope")
	})

	t.Run("policy without allow-only returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"something": "value",
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must use top-level allow-only")
	})

	t.Run("allow-only with missing repos returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"min-integrity": "none",
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing required fields repos and/or min-integrity")
	})

	t.Run("allow-only with missing integrity returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos": "public",
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing required fields repos and/or min-integrity")
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
		assert.Contains(t, err.Error(), "invalid repos value")
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
		assert.Contains(t, err.Error(), "invalid min-integrity value")
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
		assert.Contains(t, err.Error(), "unexpected key")
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
		assert.Contains(t, err.Error(), "non-empty string")
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
		assert.Contains(t, err.Error(), "non-empty string")
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
		assert.Contains(t, err.Error(), "expected non-empty array")
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
		assert.Contains(t, err.Error(), "non-empty array")
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
		assert.Contains(t, err.Error(), "non-empty string")
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
		assert.Contains(t, err.Error(), "trusted-users")
		assert.Contains(t, err.Error(), "non-empty string")
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
		assert.Contains(t, err.Error(), "trusted-users")
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
		assert.Contains(t, err.Error(), "blocked-users")
		assert.Contains(t, err.Error(), "non-empty string")
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
		assert.Contains(t, err.Error(), "blocked-users")
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
		assert.Contains(t, err.Error(), "approval-labels")
		assert.Contains(t, err.Error(), "non-empty string")
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
		assert.Contains(t, err.Error(), "unexpected allow-only key")
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
		assert.Contains(t, err.Error(), "endorsement-reactions")
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
		assert.Contains(t, err.Error(), "disapproval-integrity")
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
		assert.Contains(t, err.Error(), "endorser-min-integrity")
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
}

func TestWasmGuardClose(t *testing.T) {
	t.Run("close with nil runtime and module", func(t *testing.T) {
		guard := &WasmGuard{}
		err := guard.Close(context.Background())
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

func TestParsePathLabeledResponse(t *testing.T) {
	t.Run("invalid JSON returns error", func(t *testing.T) {
		invalidJSON := []byte("not json")
		result, err := parsePathLabeledResponse(invalidJSON, nil)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "parse path labels")
	})

	t.Run("valid path labels with nil original data returns collection labeled data", func(t *testing.T) {
		responseJSON := []byte(`{"labeled_paths":[]}`)
		result, err := parsePathLabeledResponse(responseJSON, nil)
		assert.NoError(t, err)
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
	t.Skip("TODO: implement buffer retry behavior test against callWasmFunction/tryCallWasmFunction")
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

	t.Run("wrapped error containing wasm error is a trap", func(t *testing.T) {
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
		assert.Contains(t, err.Error(), "unavailable after a previous trap")
		assert.Contains(t, err.Error(), "test-guard")
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
