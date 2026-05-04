package strutil

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandomHex(t *testing.T) {
	tests := []struct {
		name    string
		n       int
		wantLen int
		wantErr bool
	}{
		{
			name:    "zero bytes produces empty string",
			n:       0,
			wantLen: 0,
		},
		{
			name:    "1 byte produces 2 hex chars",
			n:       1,
			wantLen: 2,
		},
		{
			name:    "16 bytes produces 32 hex chars",
			n:       16,
			wantLen: 32,
		},
		{
			name:    "32 bytes produces 64 hex chars",
			n:       32,
			wantLen: 64,
		},
		{
			name:    "negative n returns error",
			n:       -1,
			wantErr: true,
		},
		{
			name:    "large negative n returns error",
			n:       -100,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RandomHex(tt.n)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, result, "result should be empty on error")
				return
			}
			require.NoError(t, err)
			assert.Len(t, result, tt.wantLen)
		})
	}
}

// TestRandomHex_ErrorMessageContainsSize verifies the error for negative n includes the invalid value.
func TestRandomHex_ErrorMessageContainsSize(t *testing.T) {
	_, err := RandomHex(-5)
	require.Error(t, err)
	assert.ErrorContains(t, err, "-5", "error message should include the invalid size")
}

// TestRandomHex_IsValidHex verifies the output is a valid lowercase hex-encoded string
// and that decoding it yields exactly n bytes.
func TestRandomHex_IsValidHex(t *testing.T) {
	result, err := RandomHex(16)
	require.NoError(t, err)

	decoded, decodeErr := hex.DecodeString(result)
	require.NoError(t, decodeErr, "result should be valid hex-encoded string")
	assert.Len(t, decoded, 16, "decoded bytes should have length equal to input n")
}

func TestRandomHex_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := RandomHex(16)
		require.NoError(t, err)
		assert.NotEmpty(t, id)
		assert.False(t, seen[id], "RandomHex should produce unique values")
		seen[id] = true
	}
}

// TestRandomHexWithFallback_ReturnsValidHex verifies the normal (non-fallback) path returns
// a valid hex-encoded string of the correct length.
func TestRandomHexWithFallback_ReturnsValidHex(t *testing.T) {
	result := RandomHexWithFallback(16)
	require.NotEmpty(t, result, "result should not be empty")
	// On a healthy system crypto/rand always succeeds, so we expect a 32-char hex string.
	assert.Len(t, result, 32, "16 bytes should produce 32 hex chars")
	_, err := hex.DecodeString(result)
	assert.NoError(t, err, "result should be valid hex")
}

// TestRandomHexWithFallback_ZeroBytes verifies zero bytes produces an empty string.
func TestRandomHexWithFallback_ZeroBytes(t *testing.T) {
	result := RandomHexWithFallback(0)
	assert.Empty(t, result, "zero bytes should produce empty string")
}

// TestRandomHexWithFallback_Uniqueness verifies the function produces unique values.
func TestRandomHexWithFallback_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := RandomHexWithFallback(16)
		assert.NotEmpty(t, id)
		assert.False(t, seen[id], "RandomHexWithFallback should produce unique values")
		seen[id] = true
	}
}

// TestRandomHexWithFallback_NegativeFallsBack verifies that a negative size triggers the
// fallback path (since RandomHex returns an error for negative n) and that the fallback
// still produces a valid hex-encoded string.
func TestRandomHexWithFallback_NegativeFallsBack(t *testing.T) {
	result := RandomHexWithFallback(-1)
	// The fallback produces a valid 32-character hex string (pid + nanoseconds encoded).
	assert.NotEmpty(t, result, "fallback should produce a non-empty string")
	_, err := hex.DecodeString(result)
	assert.NoError(t, err, "fallback should produce valid hex")
	assert.Len(t, result, 32, "fallback should produce 32 hex chars (16 bytes)")
}
