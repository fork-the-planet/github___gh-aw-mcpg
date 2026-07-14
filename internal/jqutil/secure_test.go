package jqutil

import (
	"context"
	"testing"

	"github.com/itchyny/gojq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecureCompileOpts_DisablesENV(t *testing.T) {
	// Compile a filter that tries to read $ENV — the secure options should
	// make it return null instead of actual environment data.
	query, err := gojq.Parse("$ENV")
	require.NoError(t, err)

	code, err := gojq.Compile(query, SecureCompileOpts...)
	require.NoError(t, err)

	iter := code.RunWithContext(context.Background(), nil)
	v, ok := iter.Next()
	require.True(t, ok, "expected a result from $ENV query")

	// With the environment loader returning nil, $ENV should produce an empty
	// object (no keys) rather than the real process environment.
	envMap, ok := v.(map[string]any)
	require.True(t, ok, "expected $ENV to return a map, got %T", v)
	assert.Empty(t, envMap, "$ENV should be empty when environment loader is disabled")
}

func TestSecureCompileOpts_AllowsNormalFilters(t *testing.T) {
	query, err := gojq.Parse(`.name`)
	require.NoError(t, err)

	code, err := gojq.Compile(query, SecureCompileOpts...)
	require.NoError(t, err)

	input := map[string]any{"name": "test-value", "count": 42}
	iter := code.RunWithContext(context.Background(), input)
	v, ok := iter.Next()
	require.True(t, ok)
	assert.Equal(t, "test-value", v)
}

func TestCompileOptsWithVariables(t *testing.T) {
	varNames := []string{"$x", "$y"}
	opts := CompileOptsWithVariables(varNames)

	// Should have SecureCompileOpts + 1 (WithVariables)
	assert.Len(t, opts, len(SecureCompileOpts)+1)

	// Verify the options work: compile a filter referencing the variables
	query, err := gojq.Parse(`{a: $x, b: $y}`)
	require.NoError(t, err)

	code, err := gojq.Compile(query, opts...)
	require.NoError(t, err)

	iter := code.RunWithContext(context.Background(), nil, "hello", 42)
	v, ok := iter.Next()
	require.True(t, ok)

	result, ok := v.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello", result["a"])
	assert.Equal(t, 42, result["b"])
}

func TestCompileOptsWithVariables_DoesNotMutateSharedSlice(t *testing.T) {
	origLen := len(SecureCompileOpts)
	origCap := cap(SecureCompileOpts)

	_ = CompileOptsWithVariables([]string{"$a"})
	_ = CompileOptsWithVariables([]string{"$b", "$c"})

	assert.Equal(t, origLen, len(SecureCompileOpts), "SecureCompileOpts length should not change")
	assert.Equal(t, origCap, cap(SecureCompileOpts), "SecureCompileOpts capacity should not change")
}
