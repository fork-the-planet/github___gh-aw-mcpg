package logger

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStdLog redirects the standard log.Logger output to a buffer for the
// duration of fn and returns the captured output.
func captureStdLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(oldWriter) })
	fn()
	return buf.String()
}

// TestStartupInfo verifies that StartupInfo writes to both the file logger and markdown logger.
func TestStartupInfo(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitFileLogger(logDir, "test.log")
	require.NoError(t, err)
	defer CloseGlobalLogger()

	err = InitMarkdownLogger(logDir, "test.md")
	require.NoError(t, err)
	defer CloseMarkdownLogger()

	StartupInfo("Server started on %s", "localhost:3000")

	CloseMarkdownLogger()
	CloseGlobalLogger()

	// Verify file logger received the message
	logPath := filepath.Join(logDir, "test.log")
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	logContent := string(content)
	assert.Contains(t, logContent, "[INFO]")
	assert.Contains(t, logContent, "[startup]")
	assert.Contains(t, logContent, "Server started on localhost:3000")

	// Verify markdown logger received the message
	mdPath := filepath.Join(logDir, "test.md")
	mdContent, err := os.ReadFile(mdPath)
	require.NoError(t, err)

	mdLog := string(mdContent)
	assert.Contains(t, mdLog, "Server started on localhost:3000")
}

// TestStartupWarn verifies that StartupWarn writes to the file logger with WARN level.
func TestStartupWarn(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitFileLogger(logDir, "test.log")
	require.NoError(t, err)
	defer CloseGlobalLogger()

	StartupWarn("tracing provider failed: %v", "connection refused")

	CloseGlobalLogger()

	// Verify file logger received the message with WARN level
	logPath := filepath.Join(logDir, "test.log")
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	logContent := string(content)
	assert.Contains(t, logContent, "[WARN]")
	assert.Contains(t, logContent, "[startup]")
	assert.Contains(t, logContent, "tracing provider failed: connection refused")
}

// TestStartupInfoWithoutFormatArgs verifies StartupInfo works with plain strings.
func TestStartupInfoWithoutFormatArgs(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitFileLogger(logDir, "test.log")
	require.NoError(t, err)
	defer CloseGlobalLogger()

	StartupInfo("Environment validation passed")

	CloseGlobalLogger()

	logPath := filepath.Join(logDir, "test.log")
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	logContent := string(content)
	assert.Contains(t, logContent, "[INFO]")
	assert.Contains(t, logContent, "Environment validation passed")
}

// TestStartupInfo_WritesToStandardLogger verifies that StartupInfo also calls
// log.Printf so the message appears on the standard logger (stderr in production).
// This dual-output behaviour is the whole point of the StartupInfo helper.
func TestStartupInfo_WritesToStandardLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitFileLogger(logDir, "test.log")
	require.NoError(t, err)
	defer CloseGlobalLogger()

	logOutput := captureStdLog(t, func() {
		StartupInfo("Gateway listening on %s", ":3000")
	})

	CloseGlobalLogger()

	assert.Contains(t, logOutput, "Gateway listening on :3000",
		"StartupInfo should call log.Printf so the message appears on the standard logger")
}

// TestStartupWarn_WritesToStandardLoggerWithWarningPrefix verifies that
// StartupWarn calls log.Printf with a "Warning: " prefix.
func TestStartupWarn_WritesToStandardLoggerWithWarningPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitFileLogger(logDir, "test.log")
	require.NoError(t, err)
	defer CloseGlobalLogger()

	logOutput := captureStdLog(t, func() {
		StartupWarn("tracing disabled: %s", "no endpoint configured")
	})

	CloseGlobalLogger()

	assert.Contains(t, logOutput, "Warning: tracing disabled: no endpoint configured",
		"StartupWarn should call log.Printf with 'Warning: ' prefix")
}

// TestStartupWarn_DoesNotWriteToMarkdownLogger verifies that StartupWarn uses
// LogWarn (file-only) and not LogInfoToMarkdown, so warnings are NOT mirrored to the
// markdown log. This is intentional: markdown logs are for informational
// startup summaries shown in CI previews, not for warnings.
func TestStartupWarn_DoesNotWriteToMarkdownLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitFileLogger(logDir, "test.log")
	require.NoError(t, err)
	defer CloseGlobalLogger()

	err = InitMarkdownLogger(logDir, "test.md")
	require.NoError(t, err)
	defer CloseMarkdownLogger()

	StartupWarn("startup warning: %s", "something is off")

	CloseMarkdownLogger()
	CloseGlobalLogger()

	// Verify the message IS in the file log
	logPath := filepath.Join(logDir, "test.log")
	logContent, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(logContent), "startup warning: something is off",
		"StartupWarn should write to the file logger")

	// Verify the message is NOT in the markdown log
	mdPath := filepath.Join(logDir, "test.md")
	mdContent, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	assert.NotContains(t, string(mdContent), "startup warning: something is off",
		"StartupWarn should NOT write to the markdown logger")
}

// TestStartupWarnWithoutFormatArgs verifies StartupWarn works with plain strings,
// mirroring the equivalent TestStartupInfoWithoutFormatArgs test.
func TestStartupWarnWithoutFormatArgs(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	err := InitFileLogger(logDir, "test.log")
	require.NoError(t, err)
	defer CloseGlobalLogger()

	StartupWarn("Docker daemon not reachable")

	CloseGlobalLogger()

	logPath := filepath.Join(logDir, "test.log")
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	logContent := string(content)
	assert.Contains(t, logContent, "[WARN]")
	assert.Contains(t, logContent, "Docker daemon not reachable")
}
