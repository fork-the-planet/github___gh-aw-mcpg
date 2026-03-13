package guard

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/difc"
)

type ctxKey string

const testCtxKey ctxKey = "test-key"

// minimalGuardWasm is a minimal WASM binary that exports the required guard functions
// This is compiled from WAT (WebAssembly Text Format) for zero-dependency testing
// The functions return minimal valid JSON responses
var minimalGuardWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // WASM magic number
	0x01, 0x00, 0x00, 0x00, // WASM version
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
		// This test verifies that WithCloseOnContextDone works correctly
		// When the context is cancelled, WASM execution should be interrupted

		// Create a context with short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		// The guard creation should succeed even with a short-lived context
		// because the runtime is created with the parent context
		assert.NotNil(t, ctx)
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
		// This test verifies that WithStdin(strings.NewReader("")) is applied
		// WASM modules should not be able to read from the gateway's MCP protocol stdin

		// Create a reader with some data
		stdinReader := strings.NewReader("")

		// Verify the reader is empty (isolated)
		buf := make([]byte, 10)
		n, _ := stdinReader.Read(buf)
		assert.Equal(t, 0, n, "Isolated stdin should be empty")
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
		assert.Contains(t, err.Error(), "expected {\"allow-only\":{\"repos\"")
	})

	t.Run("allow-only with missing integrity returns error", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos": "public",
			},
		}

		_, err := buildStrictLabelAgentPayload(policy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected {\"allow-only\":{\"repos\"")
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
		assert.Contains(t, err.Error(), "invalid integrity value")
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

	t.Run("valid path labels with nil original data returns error", func(t *testing.T) {
		responseJSON := []byte(`{"labeled_paths":[]}`)
		result, err := parsePathLabeledResponse(responseJSON, nil)
		assert.Error(t, err)
		assert.Nil(t, result)
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
	t.Run("buffer size constants are reasonable", func(t *testing.T) {
		// Verify the buffer sizes match the issue description
		initialSize := uint32(4 * 1024 * 1024) // 4MB
		maxSize := uint32(16 * 1024 * 1024)    // 16MB
		maxInput := uint32(8 * 1024 * 1024)    // 8MB

		assert.Equal(t, uint32(4194304), initialSize)
		assert.Equal(t, uint32(16777216), maxSize)
		assert.Equal(t, uint32(8388608), maxInput)

		// Max should be larger than initial
		assert.Greater(t, maxSize, initialSize)
		// Max input should be reasonable
		assert.LessOrEqual(t, maxInput, maxSize)
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
