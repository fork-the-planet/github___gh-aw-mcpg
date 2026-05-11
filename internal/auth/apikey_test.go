package auth_test

import (
	"encoding/hex"
	"regexp"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateRandomAPIKey verifies that GenerateRandomAPIKey produces a
// non-empty, unique, hex-encoded string per spec §7.3.
func TestGenerateRandomAPIKey(t *testing.T) {
	key, err := auth.GenerateRandomAPIKey()
	require.NoError(t, err, "GenerateRandomAPIKey() should not fail")
	assert.NotEmpty(t, key, "generated key should not be empty")
	// 32 bytes encoded as hex = 64 characters
	assert.Len(t, key, 64, "generated key should be 64 hex characters")

	// Verify keys are unique across calls
	key2, err := auth.GenerateRandomAPIKey()
	require.NoError(t, err)
	assert.NotEqual(t, key, key2, "successive calls should produce unique keys")
}

// TestGenerateRandomAPIKey_IsValidHex verifies the returned key is a valid
// hex-encoded string that decodes to exactly 32 bytes.
func TestGenerateRandomAPIKey_IsValidHex(t *testing.T) {
	key, err := auth.GenerateRandomAPIKey()
	require.NoError(t, err)

	decoded, decodeErr := hex.DecodeString(key)
	require.NoError(t, decodeErr, "key should be valid hex-encoded string; got %q", key)
	assert.Len(t, decoded, 32, "decoded key should be 32 bytes")
}

// TestGenerateRandomAPIKey_IsLowercaseHex verifies the key uses only lowercase
// hex characters (0-9, a-f) as produced by hex.EncodeToString.
func TestGenerateRandomAPIKey_IsLowercaseHex(t *testing.T) {
	key, err := auth.GenerateRandomAPIKey()
	require.NoError(t, err)

	matched, matchErr := regexp.MatchString(`^[0-9a-f]{64}$`, key)
	require.NoError(t, matchErr)
	assert.True(t, matched, "key should consist of exactly 64 lowercase hex chars; got %q", key)
}

// TestGenerateRandomAPIKey_Uniqueness verifies that repeated calls produce
// distinct keys, confirming that crypto/rand entropy is used.
func TestGenerateRandomAPIKey_Uniqueness(t *testing.T) {
	const n = 20
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		key, err := auth.GenerateRandomAPIKey()
		require.NoError(t, err, "call %d: GenerateRandomAPIKey() should not fail", i+1)
		assert.False(t, seen[key], "call %d: generated duplicate key %q", i+1, key)
		seen[key] = true
	}
}

// TestGenerateRandomAPIKey_LengthConsistency verifies that every call returns
// a key of exactly 64 characters, regardless of call order.
func TestGenerateRandomAPIKey_LengthConsistency(t *testing.T) {
	for i := 0; i < 10; i++ {
		key, err := auth.GenerateRandomAPIKey()
		require.NoError(t, err, "call %d: GenerateRandomAPIKey() should not fail", i+1)
		assert.Len(t, key, 64, "call %d: key should always be 64 characters", i+1)
	}
}
