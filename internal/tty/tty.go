//go:build !tty_stub

// Package tty provides utilities for TTY (terminal) detection.
package tty

import (
	"os"

	"golang.org/x/term"
)

// IsStderrTerminal returns true if stderr is connected to a terminal.
func IsStderrTerminal() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}
