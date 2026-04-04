package auth_test

import (
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
