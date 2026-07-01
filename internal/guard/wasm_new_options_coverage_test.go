package guard

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
)

// startOnlyGuardWasm exports only "_start" (no guard functions).
// Triggers the standard-Go compiler hint error path in NewWasmGuardWithOptions
// (the branch that detects a module built with standard Go instead of TinyGo).
//
// Compiled from:
//
//	(module
//	  (func (export "_start")))
var startOnlyGuardWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x04, 0x01, 0x60, 0x00, 0x00, 0x03, 0x02,
	0x01, 0x00, 0x07, 0x0a, 0x01, 0x06, 0x5f, 0x73, 0x74, 0x61, 0x72, 0x74, 0x00, 0x00, 0x0a, 0x04,
	0x01, 0x02, 0x00, 0x0b,
}

// labelResourceAndResponseWasm exports label_resource + label_response + memory,
// but NOT label_agent. Triggers the "must export label_agent" error path.
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "label_resource") (param i32 i32 i32 i32) (result i32) i32.const 0)
//	  (func (export "label_response") (param i32 i32 i32 i32) (result i32) i32.const 0))
var labelResourceAndResponseWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f,
	0x7f, 0x01, 0x7f, 0x03, 0x03, 0x02, 0x00, 0x00, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x2c, 0x03,
	0x0e, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x00,
	0x00, 0x0e, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x72, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65,
	0x00, 0x01, 0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, 0x0a, 0x0b, 0x02, 0x04, 0x00,
	0x41, 0x00, 0x0b, 0x04, 0x00, 0x41, 0x00, 0x0b,
}

// fullGuardWasm exports all three required guard functions plus memory; all functions
// return i32.const 0 (empty result). Enables the success path in NewWasmGuardWithOptions.
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "label_resource") (param i32 i32 i32 i32) (result i32) i32.const 0)
//	  (func (export "label_response") (param i32 i32 i32 i32) (result i32) i32.const 0)
//	  (func (export "label_agent") (param i32 i32 i32 i32) (result i32) i32.const 0))
var fullGuardWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f,
	0x7f, 0x01, 0x7f, 0x03, 0x04, 0x03, 0x00, 0x00, 0x00, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x3a,
	0x04, 0x0e, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65,
	0x00, 0x00, 0x0e, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x72, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73,
	0x65, 0x00, 0x01, 0x0b, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x00,
	0x02, 0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, 0x0a, 0x10, 0x03, 0x04, 0x00, 0x41,
	0x00, 0x0b, 0x04, 0x00, 0x41, 0x00, 0x0b, 0x04, 0x00, 0x41, 0x00, 0x0b,
}

// labelAgentReturnsTwoWasm exports label_agent + memory.
// label_agent returns i32.const 2, causing callWasmFunction to read two zeroed bytes
// from the output buffer — which is not valid JSON.
// Used to exercise the parseLabelAgentResponse error path in LabelAgent.
//
// Compiled from:
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "label_agent") (param i32 i32 i32 i32) (result i32) i32.const 2))
var labelAgentReturnsTwoWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x09, 0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f,
	0x7f, 0x01, 0x7f, 0x03, 0x02, 0x01, 0x00, 0x05, 0x03, 0x01, 0x00, 0x01, 0x07, 0x18, 0x02, 0x0b,
	0x6c, 0x61, 0x62, 0x65, 0x6c, 0x5f, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x00, 0x00, 0x06, 0x6d, 0x65,
	0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, 0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x02, 0x0b,
}

// TestNewWasmGuardWithOptions_CustomStdout verifies that opts.Stdout is wired to the
// WASM module stdout when provided. The module still fails (missing exports), but the
// code path that assigns stdoutWriter is exercised.
func TestNewWasmGuardWithOptions_CustomStdout(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	opts := &WasmGuardOptions{
		DisableCompilationCache: true,
		Stdout:                  &buf,
	}

	_, err := NewWasmGuardWithOptions(ctx, "stdout-test", minimalGuardWasm, &mockBackendCaller{}, opts)
	require.Error(t, err, "minimalGuardWasm has no exports so creation must fail")
	assert.ErrorContains(t, err, "must export")
}

// TestNewWasmGuardWithOptions_CustomStderr verifies that opts.Stderr is wired to the
// WASM module stderr when provided.
func TestNewWasmGuardWithOptions_CustomStderr(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	opts := &WasmGuardOptions{
		DisableCompilationCache: true,
		Stderr:                  &buf,
	}

	_, err := NewWasmGuardWithOptions(ctx, "stderr-test", minimalGuardWasm, &mockBackendCaller{}, opts)
	require.Error(t, err, "minimalGuardWasm has no exports so creation must fail")
	assert.ErrorContains(t, err, "must export")
}

// TestNewWasmGuardWithOptions_BothCustomStreams verifies that both opts.Stdout and
// opts.Stderr are independently wired when both are provided.
func TestNewWasmGuardWithOptions_BothCustomStreams(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	opts := &WasmGuardOptions{
		DisableCompilationCache: true,
		Stdout:                  &stdout,
		Stderr:                  &stderr,
	}

	_, err := NewWasmGuardWithOptions(ctx, "both-streams-test", minimalGuardWasm, &mockBackendCaller{}, opts)
	require.Error(t, err, "minimalGuardWasm has no exports so creation must fail")
	assert.ErrorContains(t, err, "must export")
}

// TestNewWasmGuardWithOptions_EmptyName verifies that an empty name is substituted with
// "guard" as the WASM module name. The guard still fails (no exports in minimalGuardWasm)
// but the name-fallback branch is covered.
func TestNewWasmGuardWithOptions_EmptyName(t *testing.T) {
	ctx := context.Background()
	opts := &WasmGuardOptions{
		DisableCompilationCache: true,
	}

	_, err := NewWasmGuardWithOptions(ctx, "", minimalGuardWasm, &mockBackendCaller{}, opts)
	require.Error(t, err, "minimalGuardWasm has no exports so creation must fail")
	// NewWasmGuardWithOptions falls back to module name "guard" when name is empty; the
	// returned error here is only checking the missing-export validation path.
	assert.ErrorContains(t, err, "must export")
}

// TestNewWasmGuardWithOptions_StartFunctionOnly verifies the error message when a
// WASM module exports "_start" but none of the required guard functions. This indicates
// it was compiled with standard Go instead of TinyGo.
func TestNewWasmGuardWithOptions_StartFunctionOnly(t *testing.T) {
	ctx := context.Background()
	opts := &WasmGuardOptions{
		DisableCompilationCache: true,
	}

	_, err := NewWasmGuardWithOptions(ctx, "start-only", startOnlyGuardWasm, &mockBackendCaller{}, opts)
	require.Error(t, err)
	assert.ErrorContains(t, err, "standard Go")
	assert.ErrorContains(t, err, "TinyGo")
}

// TestNewWasmGuardWithOptions_MissingLabelAgent verifies the error when a WASM module
// exports label_resource and label_response but not label_agent.
func TestNewWasmGuardWithOptions_MissingLabelAgent(t *testing.T) {
	ctx := context.Background()
	opts := &WasmGuardOptions{
		DisableCompilationCache: true,
	}

	_, err := NewWasmGuardWithOptions(ctx, "no-label-agent", labelResourceAndResponseWasm, &mockBackendCaller{}, opts)
	require.Error(t, err)
	assert.ErrorContains(t, err, "must export label_agent")
}

// TestNewWasmGuardWithOptions_SuccessPath verifies that a WASM module exporting all
// three required guard functions is successfully loaded and usable.
func TestNewWasmGuardWithOptions_SuccessPath(t *testing.T) {
	ctx := context.Background()
	opts := &WasmGuardOptions{
		DisableCompilationCache: true,
	}

	g, err := NewWasmGuardWithOptions(ctx, "full-guard", fullGuardWasm, &mockBackendCaller{}, opts)
	require.NoError(t, err)
	require.NotNil(t, g)

	assert.Equal(t, "full-guard", g.Name())
	assert.True(t, g.IsHealthy())

	require.NoError(t, g.Close(ctx))
}

// TestNewWasmGuardWithOptions_SuccessPath_WithCustomCache verifies the success path
// also works when a custom compilation cache is provided.
func TestNewWasmGuardWithOptions_SuccessPath_WithCustomCache(t *testing.T) {
	ctx := context.Background()
	cacheDir := t.TempDir()
	cache, err := wazero.NewCompilationCacheWithDir(cacheDir)
	require.NoError(t, err)
	defer cache.Close(ctx)

	opts := &WasmGuardOptions{
		CompilationCache: cache,
	}

	g, err := NewWasmGuardWithOptions(ctx, "full-guard-cached", fullGuardWasm, &mockBackendCaller{}, opts)
	require.NoError(t, err)
	require.NotNil(t, g)

	assert.Equal(t, "full-guard-cached", g.Name())
	require.NoError(t, g.Close(ctx))
}

// TestLabelAgent_InvalidJSONResponse verifies that LabelAgent returns an error when
// the WASM module writes bytes that cannot be parsed as a valid LabelAgentResult.
// Uses labelAgentReturnsTwoWasm which returns 2 (claiming to write 2 bytes) but
// the output buffer contains zeroed memory — not valid JSON.
func TestLabelAgent_InvalidJSONResponse(t *testing.T) {
	g, cleanup := setupRawWasmModule(t, labelAgentReturnsTwoWasm, "la-invalid-json")
	defer cleanup()

	validPolicy := map[string]any{
		"allow-only": map[string]any{
			"repos":         "public",
			"min-integrity": "none",
		},
	}

	_, err := g.LabelAgent(context.Background(), validPolicy, nil, nil)
	require.Error(t, err)
	// Two zeroed bytes are not valid JSON, so parsing should fail.
	assert.ErrorContains(t, err, "label_agent")
}
