package logger

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// FileLogger manages logging to a file with fallback to stdout
type FileLogger struct {
	logFile     *os.File
	logger      *log.Logger
	mu          sync.Mutex
	logDir      string
	fileName    string
	useFallback bool
}

var (
	globalFileLogger *FileLogger
	globalLoggerMu   sync.RWMutex
)

// setupFileLogger configures a FileLogger after the log file has been opened.
func setupFileLogger(file *os.File, logDir, fileName string) (*FileLogger, error) {
	fl := &FileLogger{
		logDir:   logDir,
		fileName: fileName,
		logFile:  file,
		logger:   log.New(file, "", 0),
	}
	log.Printf("Logging to file: %s", filepath.Join(logDir, fileName))
	return fl, nil
}

// handleFileLoggerError falls back to stdout when the log file cannot be opened.
func handleFileLoggerError(err error, logDir, fileName string) (*FileLogger, error) {
	log.Printf("WARNING: Failed to initialize log file: %v", err)
	log.Printf("WARNING: Falling back to stdout for logging")
	fl := &FileLogger{
		logDir:      logDir,
		fileName:    fileName,
		useFallback: true,
		logger:      log.New(os.Stdout, "", 0),
	}
	return fl, nil
}

// InitFileLogger initializes the global file logger
// If the log directory doesn't exist and can't be created, falls back to stdout
func InitFileLogger(logDir, fileName string) error {
	logger, err := initLogger(logDir, fileName, os.O_APPEND, setupFileLogger, handleFileLoggerError)
	initGlobalFileLogger(logger)
	return err
}

// withLock acquires fl.mu, executes fn, then releases fl.mu.
// Use this in methods that return an error to avoid repeating the lock/unlock preamble.
func (fl *FileLogger) withLock(fn func() error) error {
	return withMutexLock(&fl.mu, fn)
}

// Close closes the log file
func (fl *FileLogger) Close() error {
	return fl.withLock(func() error {
		return closeLogFile(fl.logFile, &fl.mu, "file")
	})
}

// LogLevel represents the severity of a log message
type LogLevel string

const (
	LogLevelInfo  LogLevel = "INFO"
	LogLevelWarn  LogLevel = "WARN"
	LogLevelError LogLevel = "ERROR"
	LogLevelDebug LogLevel = "DEBUG"
)

// Log writes a log message with the specified level and category
func (fl *FileLogger) Log(level LogLevel, category, format string, args ...interface{}) {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	logLine := formatLogLine(level, category, format, args...)
	fl.logger.Println(logLine)

	// Flush the log to disk immediately to ensure it's readable by other processes
	if fl.logFile != nil {
		if err := fl.logFile.Sync(); err != nil {
			// Log sync errors to stderr to avoid infinite recursion
			log.Printf("WARNING: Failed to sync log file: %v", err)
		}
	}
}

// GetWriter returns the underlying io.Writer for the file logger
func (fl *FileLogger) GetWriter() io.Writer {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	if fl.logFile != nil {
		return fl.logFile
	}
	return os.Stdout
}

// Global logging functions that use the global file logger

// logWithLevel is a helper that reduces code duplication for logging at different levels.
// It uses the withGlobalLogger helper from global_helpers.go to handle mutex locking and
// nil-checking, eliminating the need for repeated RWMutex lock/unlock patterns across
// LogInfo, LogWarn, LogError, and LogDebug.
func logWithLevel(level LogLevel, category, format string, args ...interface{}) {
	withGlobalLogger(&globalLoggerMu, &globalFileLogger, func(logger *FileLogger) {
		logger.Log(level, category, format, args...)
	})
}

// LogInfo logs an informational message
func LogInfo(category, format string, args ...interface{}) {
	logWithLevel(LogLevelInfo, category, format, args...)
}

// LogWarn logs a warning message
func LogWarn(category, format string, args ...interface{}) {
	logWithLevel(LogLevelWarn, category, format, args...)
}

// LogError logs an error message
func LogError(category, format string, args ...interface{}) {
	logWithLevel(LogLevelError, category, format, args...)
}

// LogDebug logs a debug message
func LogDebug(category, format string, args ...interface{}) {
	logWithLevel(LogLevelDebug, category, format, args...)
}

// logFuncs maps each LogLevel to its corresponding global log function.
// This eliminates repeated switch-on-level blocks in logWithMarkdownLevel
// (markdown_logger.go) and logWithLevelAndServer (server_file_logger.go).
// When adding a new LogLevel constant, add a corresponding entry here so
// that all dispatch sites automatically support the new level.
var logFuncs = map[LogLevel]func(string, string, ...interface{}){
	LogLevelInfo:  LogInfo,
	LogLevelWarn:  LogWarn,
	LogLevelError: LogError,
	LogLevelDebug: LogDebug,
}

// CloseGlobalLogger closes the global file logger
func CloseGlobalLogger() error {
	return closeGlobalFileLogger()
}
