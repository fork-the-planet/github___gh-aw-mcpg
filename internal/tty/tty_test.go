package tty

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/term"
)

// TestIsStderrTerminal verifies the function agrees with the underlying
// term.IsTerminal check for os.Stderr.
func TestIsStderrTerminal(t *testing.T) {
	expected := term.IsTerminal(int(os.Stderr.Fd()))
	result := IsStderrTerminal()
	assert.Equal(t, expected, result, "IsStderrTerminal should match term.IsTerminal(stderr)")
}

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
