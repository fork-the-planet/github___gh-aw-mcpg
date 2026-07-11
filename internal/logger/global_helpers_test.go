package logger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetAllGlobalLoggers resets all global logger pointers to nil for test isolation.
// It acquires each logger's mutex before resetting to avoid races.
func resetAllGlobalLoggers(t *testing.T) {
	t.Helper()
	globalLoggerMu.Lock()
	globalFileLogger = nil
	globalLoggerMu.Unlock()

	globalJSONLMu.Lock()
	globalJSONLLogger = nil
	globalJSONLMu.Unlock()

	globalMarkdownMu.Lock()
	globalMarkdownLogger = nil
	globalMarkdownMu.Unlock()

	globalToolsMu.Lock()
	globalToolsLogger = nil
	globalToolsMu.Unlock()

	globalServerLoggerMu.Lock()
	globalServerFileLogger = nil
	globalServerLoggerMu.Unlock()
}

// TestSyncFileWithWarning_NilFile verifies that syncFileWithWarning silently
// ignores a nil file and does not panic.
func TestSyncFileWithWarning_NilFile(t *testing.T) {
	assert.NotPanics(t, func() {
		syncFileWithWarning(nil, " for server test")
	}, "syncFileWithWarning should not panic when called with a nil file")
}

// TestCloseAllLoggers_NoLoggersInitialized verifies that CloseAllLoggers returns nil
// when no loggers are currently initialized.
func TestCloseAllLoggers_NoLoggersInitialized(t *testing.T) {
	resetAllGlobalLoggers(t)
	t.Cleanup(func() { resetAllGlobalLoggers(t) })

	err := CloseAllLoggers()
	assert.NoError(t, err)
}

// TestCloseAllLoggers_AllSucceed verifies that CloseAllLoggers returns nil and
// clears all global logger pointers when all loggers close without error.
func TestCloseAllLoggers_AllSucceed(t *testing.T) {
	resetAllGlobalLoggers(t)
	t.Cleanup(func() { resetAllGlobalLoggers(t) })

	tmpDir := t.TempDir()
	require.NoError(t, InitFileLogger(tmpDir, "test.log"))
	require.NoError(t, InitJSONLLogger(tmpDir, "test.jsonl"))
	require.NoError(t, InitMarkdownLogger(tmpDir, "test.md"))
	require.NoError(t, InitToolsLogger(tmpDir, "tools.json"))
	require.NoError(t, InitServerFileLogger(tmpDir))

	err := CloseAllLoggers()
	assert.NoError(t, err)

	globalLoggerMu.RLock()
	assert.Nil(t, globalFileLogger, "FileLogger should be nil after CloseAllLoggers")
	globalLoggerMu.RUnlock()

	globalJSONLMu.RLock()
	assert.Nil(t, globalJSONLLogger, "JSONLLogger should be nil after CloseAllLoggers")
	globalJSONLMu.RUnlock()

	globalMarkdownMu.RLock()
	assert.Nil(t, globalMarkdownLogger, "MarkdownLogger should be nil after CloseAllLoggers")
	globalMarkdownMu.RUnlock()

	globalToolsMu.RLock()
	assert.Nil(t, globalToolsLogger, "ToolsLogger should be nil after CloseAllLoggers")
	globalToolsMu.RUnlock()

	globalServerLoggerMu.RLock()
	assert.Nil(t, globalServerFileLogger, "ServerFileLogger should be nil after CloseAllLoggers")
	globalServerLoggerMu.RUnlock()
}

// TestCloseAllLoggers_AllCalledEvenIfEarlyFails verifies that CloseAllLoggers
// invokes every CloseXxx function even when an earlier one returns an error.
func TestCloseAllLoggers_AllCalledEvenIfEarlyFails(t *testing.T) {
	resetAllGlobalLoggers(t)
	t.Cleanup(func() { resetAllGlobalLoggers(t) })

	tmpDir := t.TempDir()
	require.NoError(t, InitFileLogger(tmpDir, "test.log"))
	require.NoError(t, InitJSONLLogger(tmpDir, "test.jsonl"))
	require.NoError(t, InitMarkdownLogger(tmpDir, "test.md"))
	require.NoError(t, InitToolsLogger(tmpDir, "tools.json"))
	require.NoError(t, InitServerFileLogger(tmpDir))

	// Force the file logger closer (the first closer) to fail by pre-closing its
	// underlying file.  The FileLogger.Close() will then return an error when
	// it tries to close an already-closed file descriptor.
	globalLoggerMu.Lock()
	_ = globalFileLogger.logFile.Close()
	globalLoggerMu.Unlock()

	err := CloseAllLoggers()
	assert.Error(t, err, "CloseAllLoggers should return an error when a closer fails")

	// All loggers must be nil: every closer was attempted, not just the first one.
	globalLoggerMu.RLock()
	assert.Nil(t, globalFileLogger, "FileLogger should be nil after CloseAllLoggers")
	globalLoggerMu.RUnlock()

	globalJSONLMu.RLock()
	assert.Nil(t, globalJSONLLogger, "JSONLLogger should be nil after CloseAllLoggers")
	globalJSONLMu.RUnlock()

	globalMarkdownMu.RLock()
	assert.Nil(t, globalMarkdownLogger, "MarkdownLogger should be nil after CloseAllLoggers")
	globalMarkdownMu.RUnlock()

	globalToolsMu.RLock()
	assert.Nil(t, globalToolsLogger, "ToolsLogger should be nil after CloseAllLoggers")
	globalToolsMu.RUnlock()

	globalServerLoggerMu.RLock()
	assert.Nil(t, globalServerFileLogger, "ServerFileLogger should be nil after CloseAllLoggers")
	globalServerLoggerMu.RUnlock()
}

// TestCloseAllLoggers_FirstErrorIsReturned verifies that when multiple closers fail,
// CloseAllLoggers returns only the first error encountered.
func TestCloseAllLoggers_FirstErrorIsReturned(t *testing.T) {
	resetAllGlobalLoggers(t)
	t.Cleanup(func() { resetAllGlobalLoggers(t) })

	firstLogDir := filepath.Join(t.TempDir(), "first")
	secondLogDir := filepath.Join(t.TempDir(), "second")

	// Initialize the first two closers (FileLogger and JSONLLogger) in distinct
	// directories so their errors contain distinguishable file paths.
	require.NoError(t, InitFileLogger(firstLogDir, "test.log"))
	require.NoError(t, InitJSONLLogger(secondLogDir, "test.jsonl"))

	// Pre-close both underlying files so both closers will return errors.
	globalLoggerMu.Lock()
	_ = globalFileLogger.logFile.Close()
	globalLoggerMu.Unlock()

	globalJSONLMu.Lock()
	_ = globalJSONLLogger.logFile.Close()
	globalJSONLMu.Unlock()

	err := CloseAllLoggers()
	require.Error(t, err)

	// The returned error must come from the first closer (FileLogger, using firstLogDir),
	// not from the second closer (JSONLLogger, using secondLogDir).
	assert.ErrorContains(t, err, firstLogDir,
		"error should originate from the first closer (FileLogger)")
}

func TestInitAndSetGlobalLoggerOnSuccess_DoesNotOverwriteOnError(t *testing.T) {
	resetAllGlobalLoggers(t)
	t.Cleanup(func() { resetAllGlobalLoggers(t) })

	tmpDir := t.TempDir()
	require.NoError(t, InitJSONLLogger(tmpDir, "test.jsonl"))

	globalJSONLMu.RLock()
	original := globalJSONLLogger
	globalJSONLMu.RUnlock()
	require.NotNil(t, original)

	err := initAndSetGlobalLoggerOnSuccess(
		&globalJSONLMu,
		&globalJSONLLogger,
		"/proc/self/invalid",
		"test.jsonl",
		os.O_APPEND,
		jsonlLoggerFactory,
	)
	require.Error(t, err)

	globalJSONLMu.RLock()
	current := globalJSONLLogger
	globalJSONLMu.RUnlock()
	assert.Same(t, original, current, "failed init should preserve existing global JSONL logger")
}

func TestInitAndSetGlobalNoFileLogger_UsesFallbackOnMkdirError(t *testing.T) {
	resetAllGlobalLoggers(t)
	t.Cleanup(func() { resetAllGlobalLoggers(t) })

	tmpDir := t.TempDir()
	blockingFile := filepath.Join(tmpDir, "not-a-dir")
	require.NoError(t, os.WriteFile(blockingFile, []byte("x"), 0644))

	err := initAndSetGlobalNoFileLogger(
		&globalServerLoggerMu,
		&globalServerFileLogger,
		filepath.Join(blockingFile, "subdir"),
		serverFileLoggerFactory,
	)
	require.NoError(t, err)

	globalServerLoggerMu.RLock()
	require.NotNil(t, globalServerFileLogger)
	assert.True(t, globalServerFileLogger.useFallback, "mkdir failure should initialize server logger in fallback mode")
	globalServerLoggerMu.RUnlock()
}
