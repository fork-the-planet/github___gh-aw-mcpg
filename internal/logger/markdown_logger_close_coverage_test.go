package logger

import (
	stdlog "log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarkdownLogger_Close_FooterWriteError covers the branch in Close() where
// WriteString(footer) fails (e.g. because the underlying file is already closed).
// Per the implementation, Close should still attempt to close the file even if
// the footer write fails, and should return the closeLogFile result.
func TestMarkdownLogger_Close_FooterWriteError(t *testing.T) {
	tmpDir := t.TempDir()
	f, err := os.CreateTemp(tmpDir, "test-*.md")
	require.NoError(t, err)

	ml, err := setupMarkdownLogger(f, tmpDir, filepath.Base(f.Name()))
	require.NoError(t, err)
	require.NotNil(t, ml)

	// Pre-close the underlying file so that WriteString(footer) returns an error.
	require.NoError(t, f.Close())

	// Close() should still return without panic, handling the write error gracefully.
	closeErr := ml.Close()
	// The error comes from closeLogFile; the important thing is no panic and
	// that the error path is exercised.
	_ = closeErr
}

// TestMarkdownLogger_Close_NilLogFile covers the ml.logFile == nil branch:
// when the logger has no file (e.g. handleMarkdownLoggerError / fallback mode),
// Close() should return nil.
func TestMarkdownLogger_Close_NilLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	ml, err := handleMarkdownLoggerError(os.ErrNotExist, tmpDir, "test.md")
	require.NoError(t, err)
	require.NotNil(t, ml)
	assert.Nil(t, ml.logFile, "fallback logger should have nil logFile")

	err = ml.Close()
	assert.NoError(t, err, "Close() on nil logFile should return nil")
}

// TestMarkdownLogger_InitializeFile_WriteError covers the error return path in
// initializeFile() when the underlying file has been closed before the first
// header write.
func TestMarkdownLogger_InitializeFile_WriteError(t *testing.T) {
	tmpDir := t.TempDir()
	f, err := os.CreateTemp(tmpDir, "test-*.md")
	require.NoError(t, err)

	ml, err := setupMarkdownLogger(f, tmpDir, filepath.Base(f.Name()))
	require.NoError(t, err)

	// Close the file to force the WriteString call to fail.
	require.NoError(t, f.Close())

	err = ml.initializeFile()
	assert.Error(t, err, "initializeFile() should return error when write fails")
	assert.False(t, ml.initialized, "ml.initialized should remain false after failed header write")
}

// TestMarkdownLogger_InitializeFile_AlreadyInitialized covers the fast-path
// return in initializeFile() when ml.initialized is already true.
func TestMarkdownLogger_InitializeFile_AlreadyInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	f, err := os.CreateTemp(tmpDir, "test-*.md")
	require.NoError(t, err)
	defer f.Close()

	ml, err := setupMarkdownLogger(f, tmpDir, filepath.Base(f.Name()))
	require.NoError(t, err)

	ml.initialized = true

	// Should be a no-op even with an open file.
	err = ml.initializeFile()
	assert.NoError(t, err, "initializeFile() should return nil when already initialized")
}

// TestMarkdownLogger_Log_WriteError covers the return-on-error branch inside Log()
// for the logFile.WriteString call.  We pre-initialize the header then close the
// file, so the body WriteString fails and Log silently returns.
func TestMarkdownLogger_Log_WriteError(t *testing.T) {
	tmpDir := t.TempDir()
	f, err := os.CreateTemp(tmpDir, "test-*.md")
	require.NoError(t, err)

	ml, err := setupMarkdownLogger(f, tmpDir, filepath.Base(f.Name()))
	require.NoError(t, err)

	// Write the header successfully so ml.initialized becomes true.
	err = ml.initializeFile()
	require.NoError(t, err)
	require.True(t, ml.initialized)

	// Now close the file so the subsequent logLine WriteString fails.
	require.NoError(t, f.Close())

	// Log() must not panic; it should silently return on write error.
	require.NotPanics(t, func() {
		ml.Log(LogLevelInfo, "test", "this write should fail silently")
	})
}

// TestMarkdownLogger_Log_NilLogFile covers the ml.logFile == nil branch inside
// Log(): message should be silently dropped.
func TestMarkdownLogger_Log_NilLogFile(t *testing.T) {
	ml := &MarkdownLogger{
		initialized: true,
		// logFile is nil — fallback / no-file mode
	}

	require.NotPanics(t, func() {
		ml.Log(LogLevelInfo, "test", "should be silently dropped")
	})
}

// TestFileLogger_Log_SyncError covers the sync-error warning branch in
// FileLogger.Log() by closing the underlying file before the Log call so that
// logFile.Sync() returns an error.
func TestFileLogger_Log_SyncError(t *testing.T) {
	tmpDir := t.TempDir()
	f, err := os.CreateTemp(tmpDir, "test-*.log")
	require.NoError(t, err)

	// Build a FileLogger directly (package-internal access).
	fl := &FileLogger{
		logFile: f,
		logger:  stdlog.New(f, "", 0),
	}

	// Close the file so Sync() will fail.
	require.NoError(t, f.Close())

	// Log() must not panic; it logs the sync warning to stderr.
	require.NotPanics(t, func() {
		fl.Log(LogLevelInfo, "test", "sync will fail")
	})
}

// TestLogger_Print_Disabled covers the early-return branch in Logger.Print()
// when the logger is disabled (l.enabled == false), ensuring no output is
// produced and the function returns without panicking.
func TestLogger_Print_Disabled(t *testing.T) {
	// Create a logger with no DEBUG pattern — logger is disabled.
	t.Setenv("DEBUG", "")
	lg := New("test:disabled-print")
	assert.False(t, lg.enabled, "logger should be disabled when DEBUG is empty")

	output := captureStderr(func() {
		lg.Print("this should not appear")
	})

	assert.Empty(t, output, "Print() on a disabled logger should produce no output")
}
