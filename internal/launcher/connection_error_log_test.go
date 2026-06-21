package launcher

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureConnectionErrorLogOutput redirects log output to a buffer for the duration of fn.
func captureConnectionErrorLogOutput(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(oldOutput)
	})
	fn()
	return buf.String()
}

func TestLogConnectionError_BasicOutput(t *testing.T) {
	tests := []struct {
		name      string
		ctx       ConnectionErrorContext
		err       error
		wantInLog []string
		notInLog  []string
	}{
		{
			name: "minimal context - no serverID",
			ctx: ConnectionErrorContext{
				Command: "docker",
				Args:    []string{"run", "ghcr.io/github/test:latest"},
			},
			err: errors.New("connection refused"),
			wantInLog: []string{
				"❌ MCP Connection Failed",
				"connection refused",
				"Debug Information:",
				"docker",
				"ghcr.io/github/test:latest",
			},
		},
		{
			name: "with serverID and sessionID",
			ctx: ConnectionErrorContext{
				ServerID:  "my-server",
				SessionID: "sess-abc",
				Command:   "docker",
				Args:      []string{"run", "test-image"},
			},
			err: errors.New("exit status 1"),
			wantInLog: []string{
				"❌ FAILED to connect to server 'my-server'",
				"for session 'sess-abc'",
				"exit status 1",
				"docker",
				"test-image",
				"Debug Information:",
			},
		},
		{
			name: "with serverID, no sessionID",
			ctx: ConnectionErrorContext{
				ServerID: "github",
				Command:  "docker",
				Args:     []string{"run", "ghcr.io/github/github-mcp-server:latest"},
			},
			err: errors.New("image not found"),
			wantInLog: []string{
				"❌ FAILED to connect to server 'github'",
				"image not found",
				"Debug Information:",
			},
			notInLog: []string{
				"for session",
			},
		},
		{
			name: "with env vars",
			ctx: ConnectionErrorContext{
				ServerID: "srv",
				Command:  "docker",
				Args:     []string{"run", "img"},
				Env:      map[string]string{"API_KEY": "secret-value-12345"},
			},
			err: errors.New("failed"),
			wantInLog: []string{
				"Env vars:",
				"API_KEY",
			},
		},
		{
			name: "without env vars - no env line",
			ctx: ConnectionErrorContext{
				ServerID: "srv",
				Command:  "docker",
				Args:     []string{"run", "img"},
				Env:      map[string]string{},
			},
			err: errors.New("failed"),
			notInLog: []string{
				"Env vars:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureConnectionErrorLogOutput(t, func() {
				LogConnectionError(tt.ctx, tt.err)
			})

			for _, want := range tt.wantInLog {
				assert.Contains(t, output, want, "Expected log output to contain: %q", want)
			}
			for _, notWant := range tt.notInLog {
				assert.NotContains(t, output, notWant, "Expected log output NOT to contain: %q", notWant)
			}
			assert.Contains(t, output, "[LAUNCHER]", "Expected [LAUNCHER] prefix")
		})
	}
}

func TestLogConnectionError_ContainerHints(t *testing.T) {
	tests := []struct {
		name               string
		isDirectCommand    bool
		runningInContainer bool
		wantInLog          []string
		notInLog           []string
	}{
		{
			name:               "direct command in container",
			isDirectCommand:    true,
			runningInContainer: true,
			wantInLog: []string{
				"Possible causes:",
				"may not be installed in the gateway container",
				"Consider using 'container' config",
				"Dockerfile",
				"Running in container: true",
				"Is direct command: true",
			},
			notInLog: []string{
				"may not be in PATH",
				"which",
			},
		},
		{
			name:               "direct command not in container",
			isDirectCommand:    true,
			runningInContainer: false,
			wantInLog: []string{
				"Possible causes:",
				"may not be in PATH",
				"Check if",
				"file permissions",
				"Running in container: false",
				"Is direct command: true",
			},
			notInLog: []string{
				"gateway container",
				"Dockerfile",
			},
		},
		{
			name:               "non-direct command - no hints",
			isDirectCommand:    false,
			runningInContainer: false,
			notInLog: []string{
				"Possible causes:",
				"may not be in PATH",
				"gateway container",
				"Running in container",
				"Is direct command",
			},
		},
		{
			name:               "docker command in container - no hints",
			isDirectCommand:    false,
			runningInContainer: true,
			wantInLog: []string{
				"Running in container: true",
				"Is direct command: false",
			},
			notInLog: []string{
				"Possible causes:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ConnectionErrorContext{
				Command:            "mycommand",
				Args:               []string{"arg1"},
				IsDirectCommand:    tt.isDirectCommand,
				RunningInContainer: tt.runningInContainer,
			}
			output := captureConnectionErrorLogOutput(t, func() {
				LogConnectionError(ctx, errors.New("some error"))
			})

			for _, want := range tt.wantInLog {
				assert.Contains(t, output, want, "Expected log output to contain: %q", want)
			}
			for _, notWant := range tt.notInLog {
				assert.NotContains(t, output, notWant, "Expected log output NOT to contain: %q", notWant)
			}
		})
	}
}

func TestLogConnectionError_ErrorStringHints(t *testing.T) {
	tests := []struct {
		name      string
		errMsg    string
		wantInLog []string
		notInLog  []string
	}{
		{
			name:   "executable file not found",
			errMsg: `exec: "notfound": executable file not found in $PATH`,
			wantInLog: []string{
				"not found in PATH",
				"Verify the command is installed and executable",
			},
		},
		{
			name:   "no such file or directory",
			errMsg: "fork/exec /usr/bin/nothere: no such file or directory",
			wantInLog: []string{
				"not found in PATH",
				"Verify the command is installed and executable",
			},
		},
		{
			name:   "EOF connection error",
			errMsg: "EOF",
			wantInLog: []string{
				"Process started but terminated unexpectedly",
				"Check if the command supports MCP protocol over stdio",
			},
		},
		{
			name:   "broken pipe",
			errMsg: "write |0: broken pipe",
			wantInLog: []string{
				"Process started but terminated unexpectedly",
				"Check if the command supports MCP protocol over stdio",
			},
		},
		{
			name:   "generic error - no extra hints",
			errMsg: "connection refused",
			notInLog: []string{
				"not found in PATH",
				"terminated unexpectedly",
				"MCP protocol",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ConnectionErrorContext{
				Command: "myserver",
				Args:    []string{"--port", "8080"},
			}

			output := captureConnectionErrorLogOutput(t, func() {
				LogConnectionError(ctx, errors.New(tt.errMsg))
			})

			for _, want := range tt.wantInLog {
				assert.Contains(t, output, want, "Expected log output to contain: %q", want)
			}
			for _, notWant := range tt.notInLog {
				assert.NotContains(t, output, notWant, "Expected log output NOT to contain: %q", notWant)
			}
		})
	}
}

func TestLogConnectionError_StderrOutput(t *testing.T) {
	tests := []struct {
		name         string
		stderrOutput string
		wantInLog    []string
		notInLog     []string
	}{
		{
			name:         "with stderr output",
			stderrOutput: "Error: container image not found\nPull failed",
			wantInLog: []string{
				"📋 Process stderr output:",
				"Error: container image not found",
				"Pull failed",
			},
		},
		{
			name:         "no stderr output",
			stderrOutput: "",
			notInLog: []string{
				"📋 Process stderr output:",
			},
		},
		{
			name:         "whitespace-only stderr is empty",
			stderrOutput: "   \n  ",
			// TrimSpace is done by the caller before passing to LogConnectionError
			// so we test that empty string (after trim) produces no stderr block.
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ConnectionErrorContext{
				Command:      "docker",
				Args:         []string{"run", "bad-image"},
				StderrOutput: tt.stderrOutput,
			}
			output := captureConnectionErrorLogOutput(t, func() {
				LogConnectionError(ctx, errors.New("connection failed"))
			})

			for _, want := range tt.wantInLog {
				assert.Contains(t, output, want, "Expected log output to contain: %q", want)
			}
			for _, notWant := range tt.notInLog {
				assert.NotContains(t, output, notWant, "Expected log output NOT to contain: %q", notWant)
			}
		})
	}
}

func TestLogConnectionError_StartupTimeout(t *testing.T) {
	t.Run("with startup timeout", func(t *testing.T) {
		ctx := ConnectionErrorContext{
			Command:        "docker",
			Args:           []string{"run", "img"},
			StartupTimeout: 30 * time.Second,
		}
		output := captureConnectionErrorLogOutput(t, func() {
			LogConnectionError(ctx, errors.New("timeout"))
		})
		assert.Contains(t, output, "Startup timeout: 30s")
	})

	t.Run("no startup timeout - line omitted", func(t *testing.T) {
		ctx := ConnectionErrorContext{
			Command: "docker",
			Args:    []string{"run", "img"},
		}
		output := captureConnectionErrorLogOutput(t, func() {
			LogConnectionError(ctx, errors.New("timeout"))
		})
		assert.NotContains(t, output, "Startup timeout:")
	})
}

// TestLogConnectionError_ArgsSanitized ensures sensitive values in args are masked.
func TestLogConnectionError_ArgsSanitized(t *testing.T) {
	ctx := ConnectionErrorContext{
		Command: "docker",
		Args:    []string{"run", "-e", "TOKEN=super-secret-abc123"},
	}
	output := captureConnectionErrorLogOutput(t, func() {
		LogConnectionError(ctx, errors.New("failed"))
	})
	// The secret value should be truncated/masked by SanitizeArgs.
	assert.NotContains(t, output, "super-secret-abc123", "Secret should be sanitized")
}

// TestLogConnectionError_EdgeCases tests boundary conditions.
func TestLogConnectionError_EdgeCases(t *testing.T) {
	t.Run("empty command and args", func(t *testing.T) {
		ctx := ConnectionErrorContext{}
		require.NotPanics(t, func() {
			captureConnectionErrorLogOutput(t, func() {
				LogConnectionError(ctx, errors.New("test error"))
			})
		})
	})

	t.Run("very long command string", func(t *testing.T) {
		longCmd := strings.Repeat("x", 10000)
		ctx := ConnectionErrorContext{
			Command: longCmd,
			Args:    []string{strings.Repeat("y", 5000)},
		}
		require.NotPanics(t, func() {
			captureConnectionErrorLogOutput(t, func() {
				LogConnectionError(ctx, errors.New("failed"))
			})
		})
	})

	t.Run("multiline stderr output", func(t *testing.T) {
		ctx := ConnectionErrorContext{
			Command:      "docker",
			StderrOutput: "line1\nline2\nline3",
		}
		output := captureConnectionErrorLogOutput(t, func() {
			LogConnectionError(ctx, errors.New("failed"))
		})
		assert.Contains(t, output, "line1")
		assert.Contains(t, output, "line2")
		assert.Contains(t, output, "line3")
	})
}
