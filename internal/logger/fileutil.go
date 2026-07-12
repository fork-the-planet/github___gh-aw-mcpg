package logger

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// closeLogFile syncs buffered data before closing and handles errors appropriately.
// The mutex should already be held by the caller.
//
// Error handling strategy:
// - Sync errors are logged but don't prevent closing (ensures resources are released)
// - Close errors are returned to the caller
//
// This ensures consistent behavior across all logger types:
// - Resources are always released (no file descriptor leaks)
// - Sync errors are logged for debugging but don't block cleanup
// - Close errors are propagated to indicate serious issues
func closeLogFile(file *os.File, mu *sync.Mutex, loggerName string) error {
	if file == nil {
		return nil
	}

	// Sync any remaining buffered data before closing
	// Log errors but continue with close to avoid resource leaks
	if err := file.Sync(); err != nil {
		log.Printf("WARNING: Failed to sync %s log file before close: %v", loggerName, err)
	}

	// Always close the file, even if sync failed
	return file.Close()
}

// initLogFile handles the common logic for initializing a log file.
// It creates the log directory if needed and opens the log file with the specified flags.
//
// Parameters:
//   - logDir: Directory where the log file should be created
//   - fileName: Name of the log file
//   - flags: File opening flags (e.g., os.O_APPEND, os.O_TRUNC)
//
// Returns:
//   - *os.File: The opened log file handle
//   - error: Any error that occurred during directory creation or file opening
//
// This function does not implement any fallback behavior - it returns errors to the caller.
// Callers can decide whether to fall back to stdout or propagate the error.
func initLogFile(logDir, fileName string, flags int) (*os.File, error) {
	// Try to create the log directory if it doesn't exist
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Try to open the log file with the specified flags
	logPath := filepath.Join(logDir, fileName)
	file, err := os.OpenFile(logPath, flags|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	return file, nil
}

// atomicWriteFile writes data to filePath atomically using a temp-file + rename strategy.
// On rename failure the temp file is removed; a removal error that is not os.IsNotExist
// is logged as a warning but does not mask the primary rename error.
//
// TODO: export as internal/util.AtomicWriteFile (or internal/fileutil) when a
// second consumer outside the logger package emerges.
func atomicWriteFile(filePath string, data []byte, perm os.FileMode) error {
	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, data, perm); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := os.Rename(tempPath, filePath); err != nil {
		if removeErr := os.Remove(tempPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("WARNING: Failed to cleanup temp file %s: %v", tempPath, removeErr)
		}
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	return nil
}

// writeJSONToFile marshals data as indented JSON and atomically writes it to
// filepath.Join(logDir, fileName) with the given file permissions.
// It is a shared helper used by logger types that periodically persist state
// to a JSON file (e.g. ToolsLogger, ObservedURLDomainsLogger).
func writeJSONToFile(logDir, fileName string, data any, perm os.FileMode) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON data for %s: %w", filepath.Join(logDir, fileName), err)
	}
	return atomicWriteFile(filepath.Join(logDir, fileName), jsonData, perm)
}
