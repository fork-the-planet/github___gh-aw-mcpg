package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogWithLevel verifies the logWithLevel helper function works correctly
// and eliminates duplicate code in file_logger.go
func TestLogWithLevel(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")
	fileName := "helper-test.log"

	err := InitFileLogger(logDir, fileName)
	require.NoError(t, err)
	defer CloseGlobalLogger()

	// Test all log levels using the helper
	LogInfo("test", "Info message via helper")
	LogWarn("test", "Warning message via helper")
	LogError("test", "Error message via helper")
	LogDebug("test", "Debug message via helper")

	CloseGlobalLogger()

	// Read and verify
	logPath := filepath.Join(logDir, fileName)
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	logContent := string(content)
	assert.Contains(t, logContent, "[INFO]")
	assert.Contains(t, logContent, "Info message via helper")
	assert.Contains(t, logContent, "[WARN]")
	assert.Contains(t, logContent, "Warning message via helper")
	assert.Contains(t, logContent, "[ERROR]")
	assert.Contains(t, logContent, "Error message via helper")
	assert.Contains(t, logContent, "[DEBUG]")
	assert.Contains(t, logContent, "Debug message via helper")
}

// TestLogWithLevelAndServer verifies the logWithLevelAndServer helper function
// works correctly and handles both per-server and unified logging
func TestLogWithLevelAndServer(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	// Initialize both loggers
	err := InitFileLogger(logDir, "mcp-gateway.log")
	require.NoError(t, err)
	defer CloseGlobalLogger()

	err = InitServerFileLogger(logDir)
	require.NoError(t, err)
	defer CloseServerFileLogger()

	serverID := "test-server"

	// Test all log levels using the helper
	LogInfoWithServer(serverID, "test", "Info message via server helper")
	LogWarnWithServer(serverID, "test", "Warning message via server helper")
	LogErrorWithServer(serverID, "test", "Error message via server helper")
	LogDebugWithServer(serverID, "test", "Debug message via server helper")

	CloseServerFileLogger()
	CloseGlobalLogger()

	// Verify server-specific log file
	serverLogPath := filepath.Join(logDir, serverID+".log")
	serverContent, err := os.ReadFile(serverLogPath)
	require.NoError(t, err)

	serverLog := string(serverContent)
	assert.Contains(t, serverLog, "[INFO]")
	assert.Contains(t, serverLog, "Info message via server helper")
	assert.Contains(t, serverLog, "[WARN]")
	assert.Contains(t, serverLog, "Warning message via server helper")
	assert.Contains(t, serverLog, "[ERROR]")
	assert.Contains(t, serverLog, "Error message via server helper")
	assert.Contains(t, serverLog, "[DEBUG]")
	assert.Contains(t, serverLog, "Debug message via server helper")

	// Verify unified log file contains messages with server prefix
	unifiedLogPath := filepath.Join(logDir, "mcp-gateway.log")
	unifiedContent, err := os.ReadFile(unifiedLogPath)
	require.NoError(t, err)

	unifiedLog := string(unifiedContent)
	assert.Contains(t, unifiedLog, "[test-server]")
	assert.Contains(t, unifiedLog, "Info message via server helper")
	assert.Contains(t, unifiedLog, "Warning message via server helper")
	assert.Contains(t, unifiedLog, "Error message via server helper")
	assert.Contains(t, unifiedLog, "Debug message via server helper")
}

// TestLogWithMarkdown verifies the logWithMarkdown helper function
// correctly handles both regular and markdown logging
func TestLogWithMarkdown(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	// Initialize both loggers
	err := InitFileLogger(logDir, "test.log")
	require.NoError(t, err)
	defer CloseGlobalLogger()

	err = InitMarkdownLogger(logDir, "test.md")
	require.NoError(t, err)
	defer CloseMarkdownLogger()

	// Test all log levels using the helper
	LogInfoMd("test", "Info message via markdown helper")
	LogWarnMd("test", "Warning message via markdown helper")
	LogErrorMd("test", "Error message via markdown helper")
	LogDebugMd("test", "Debug message via markdown helper")

	CloseMarkdownLogger()
	CloseGlobalLogger()

	// Verify regular log file
	logPath := filepath.Join(logDir, "test.log")
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logContent := string(content)
	assert.Contains(t, logContent, "[INFO]")
	assert.Contains(t, logContent, "Info message via markdown helper")

	// Verify markdown log file
	mdPath := filepath.Join(logDir, "test.md")
	mdContent, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	mdLog := string(mdContent)

	// Check for markdown formatting with emojis
	assert.Contains(t, mdLog, "✓")  // Info emoji
	assert.Contains(t, mdLog, "⚠️") // Warning emoji
	assert.Contains(t, mdLog, "✗")  // Error emoji
	assert.Contains(t, mdLog, "🔍")  // Debug emoji

	// Check for messages
	assert.Contains(t, mdLog, "Info message via markdown helper")
	assert.Contains(t, mdLog, "Warning message via markdown helper")
	assert.Contains(t, mdLog, "Error message via markdown helper")
	assert.Contains(t, mdLog, "Debug message via markdown helper")
}

// TestHelperFunctionsWithFormatting verifies helpers work with format strings
func TestHelperFunctionsWithFormatting(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")
	fileName := "format-test.log"

	err := InitFileLogger(logDir, fileName)
	require.NoError(t, err)
	defer CloseGlobalLogger()

	// Test formatted messages
	LogInfo("test", "Value: %d, String: %s", 42, "hello")
	LogWarn("test", "Float: %.2f", 3.14159)
	LogError("test", "Multiple: %d %s %v", 1, "two", true)

	CloseGlobalLogger()

	logPath := filepath.Join(logDir, fileName)
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	logContent := string(content)
	assert.Contains(t, logContent, "Value: 42, String: hello")
	assert.Contains(t, logContent, "Float: 3.14")
	assert.Contains(t, logContent, "Multiple: 1 two true")
}

// TestHelperFunctionsConcurrency verifies helpers are thread-safe
func TestHelperFunctionsConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")
	fileName := "concurrent-helper-test.log"

	err := InitFileLogger(logDir, fileName)
	require.NoError(t, err)
	defer CloseGlobalLogger()

	// Write from multiple goroutines
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				LogInfo("concurrent", "Message from goroutine %d, iteration %d", id, j)
				LogWarn("concurrent", "Warning from goroutine %d, iteration %d", id, j)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	CloseGlobalLogger()

	// Verify log file
	logPath := filepath.Join(logDir, fileName)
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	// Count log lines (should be 200: 10 goroutines * 10 messages * 2 levels)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	assert.Equal(t, 200, len(lines), "Expected 200 log lines from concurrent helper calls")
}
