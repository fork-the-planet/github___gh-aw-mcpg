//go:build tty_stub

// Package tty provides utilities for TTY (terminal) detection.
package tty

// IsStderrTerminal returns false when the tty_stub build tag is active.
// This stub enables testing in environments where golang.org/x/term is unavailable
// (e.g. sandboxed CI environments with restricted network access).
func IsStderrTerminal() bool {
	return false
}
