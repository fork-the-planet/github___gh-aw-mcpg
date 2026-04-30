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
	lockable
	logFile     *os.File
	logger      *log.Logger
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
	logFallbackWarnings(err, "Failed to initialize log file", "Falling back to stdout for logging")
	fl := &FileLogger{
		logDir:      logDir,
		fileName:    fileName,
		useFallback: true,
		logger:      log.New(os.Stdout, "", 0),
	}
	return fl, nil
}

// fileLoggerFactory bundles the setup and error-handler for FileLogger.
var fileLoggerFactory = loggerFactory[*FileLogger]{
	setup:   setupFileLogger,
	onError: handleFileLoggerError,
}

// InitFileLogger initializes the global file logger
// If the log directory doesn't exist and can't be created, falls back to stdout
func InitFileLogger(logDir, fileName string) error {
	logger, err := initLogger(logDir, fileName, os.O_APPEND, fileLoggerFactory)
	initGlobalLogger(&globalLoggerMu, &globalFileLogger, logger)
	return err
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

// The var block and exported wrappers below follow the Log-Level Quad-Function Pattern
// documented in common.go. The four-level set (Info/Warn/Error/Debug) is stable and
// intentionally repeated across file_logger.go, markdown_logger.go, and
// server_file_logger.go. See common.go for the rationale and update instructions.
var (
	logInfo  = makeLevelLogger(logWithLevel, LogLevelInfo)
	logWarn  = makeLevelLogger(logWithLevel, LogLevelWarn)
	logError = makeLevelLogger(logWithLevel, LogLevelError)
	logDebug = makeLevelLogger(logWithLevel, LogLevelDebug)
)

// LogInfo logs an informational message.
func LogInfo(category, format string, args ...interface{}) {
	logInfo(category, format, args...)
}

// LogWarn logs a warning message.
func LogWarn(category, format string, args ...interface{}) {
	logWarn(category, format, args...)
}

// LogError logs an error message.
func LogError(category, format string, args ...interface{}) {
	logError(category, format, args...)
}

// LogDebug logs a debug message.
func LogDebug(category, format string, args ...interface{}) {
	logDebug(category, format, args...)
}

// CloseGlobalLogger closes the global file logger
func CloseGlobalLogger() error {
	return closeGlobalLogger(&globalLoggerMu, &globalFileLogger)
}
