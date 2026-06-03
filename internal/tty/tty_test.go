package tty

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsStderrTerminal_ConsistentResult verifies that repeated calls return the same value,
// confirming deterministic behavior with no side effects.
func TestIsStderrTerminal_ConsistentResult(t *testing.T) {
	first := IsStderrTerminal()
	for i := 0; i < 5; i++ {
		assert.Equal(t, first, IsStderrTerminal(), "call %d: IsStderrTerminal should be deterministic", i+1)
	}
}

// TestIsStderrTerminal_NotATerminalInCI verifies the expected false result in automated
// environments such as CI pipelines where stderr is a pipe, not a terminal.
func TestIsStderrTerminal_NotATerminalInCI(t *testing.T) {
	if os.Getenv("CI") == "" && os.Getenv("GITHUB_ACTIONS") == "" {
		t.Skip("Skipping CI-specific assertion: not running in a CI environment")
	}
	assert.False(t, IsStderrTerminal(), "stderr should not be a terminal in CI")
}

// TestIsStderrTerminal_WithPipe verifies that a pipe-backed stderr is not treated as a terminal.
func TestIsStderrTerminal_WithPipe(t *testing.T) {
	originalStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		os.Stderr = originalStderr
		require.NoError(t, r.Close())
		require.NoError(t, w.Close())
	})
	os.Stderr = w

	assert.False(t, IsStderrTerminal(), "pipe-backed stderr should not be a terminal")
}
