package logger

import (
	"errors"
	"fmt"
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
	t.Cleanup(func() { require.NoError(t, CloseAllLoggers()) })
	require.NoError(t, InitJSONLLogger(tmpDir, "test.jsonl"))

	loggerRef := bindGlobalLogger(&globalJSONLMu, &globalJSONLLogger)

	globalJSONLMu.RLock()
	original := globalJSONLLogger
	globalJSONLMu.RUnlock()
	require.NotNil(t, original)

	err := loggerRef.initOnSuccess("/proc/self/invalid", "test.jsonl", os.O_APPEND, jsonlLoggerFactory)
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

	loggerRef := bindGlobalLogger(&globalServerLoggerMu, &globalServerFileLogger)
	err := loggerRef.initNoFile(filepath.Join(blockingFile, "subdir"), serverFileLoggerFactory)
	require.NoError(t, err)

	globalServerLoggerMu.RLock()
	logger := globalServerFileLogger
	globalServerLoggerMu.RUnlock()
	require.NotNil(t, logger)
	assert.True(t, logger.useFallback, "mkdir failure should initialize server logger in fallback mode")
}

func TestCloseLogFileWithCleanup_ClosesAfterCleanupError(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "cleanup-error.log")

	file, err := os.Create(logPath)
	require.NoError(t, err)

	var l lockable
	cleanupCalled := false

	err = closeLogFileWithCleanup(&l, file, "test", func(f *os.File) error {
		cleanupCalled = true
		return fmt.Errorf("footer write failed")
	})
	require.EqualError(t, err, "footer write failed")
	assert.True(t, cleanupCalled, "cleanup callback should run before close")

	_, writeErr := file.WriteString("should fail after close")
	require.Error(t, writeErr, "file should be closed even when cleanup fails")
}

// TestInitAndSetGlobalNoFileLogger_FactorySetupError verifies that
// initAndSetGlobalNoFileLogger propagates errors from factory.setup.
func TestInitAndSetGlobalNoFileLogger_FactorySetupError(t *testing.T) {
	resetAllGlobalLoggers(t)
	t.Cleanup(func() { resetAllGlobalLoggers(t) })

	setupErr := errors.New("setup failed")
	factory := newLoggerFactory(
		func(_ *os.File, _, _ string) (*ServerFileLogger, error) {
			return nil, setupErr
		},
		func(err error, logDir, _ string) (*ServerFileLogger, error) {
			return newServerFileLogger(logDir, true), nil
		},
	)

	tmpDir := t.TempDir()
	err := initAndSetGlobalNoFileLogger(&globalServerLoggerMu, &globalServerFileLogger, tmpDir, factory)
	require.Error(t, err)
	assert.ErrorIs(t, err, setupErr)
}

// TestInitAndSetGlobalNoFileLogger_OnErrorReturnsError verifies that
// initAndSetGlobalNoFileLogger propagates errors from factory.onError when
// the directory cannot be created.
func TestInitAndSetGlobalNoFileLogger_OnErrorReturnsError(t *testing.T) {
	resetAllGlobalLoggers(t)
	t.Cleanup(func() { resetAllGlobalLoggers(t) })

	onErrorErr := errors.New("onError failed")
	factory := newLoggerFactory(
		func(_ *os.File, logDir, _ string) (*ServerFileLogger, error) {
			return newServerFileLogger(logDir, false), nil
		},
		func(_ error, logDir, _ string) (*ServerFileLogger, error) {
			return nil, onErrorErr
		},
	)

	// Use a path that cannot be created (file used as parent directory).
	tmpDir := t.TempDir()
	blockingFile := filepath.Join(tmpDir, "not-a-dir")
	require.NoError(t, os.WriteFile(blockingFile, []byte("x"), 0644))

	err := initAndSetGlobalNoFileLogger(
		&globalServerLoggerMu,
		&globalServerFileLogger,
		filepath.Join(blockingFile, "subdir"),
		factory,
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, onErrorErr)
}

// TestWriteJSONToFile_MarshalError verifies that writeJSONToFile returns an error
// when the provided data cannot be marshaled to JSON.
func TestWriteJSONToFile_MarshalError(t *testing.T) {
	tmpDir := t.TempDir()
	// Channels are not JSON-marshalable.
	err := writeJSONToFile(tmpDir, "out.json", make(chan int), 0644)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal JSON data")
	assert.Contains(t, err.Error(), "out.json")
}
