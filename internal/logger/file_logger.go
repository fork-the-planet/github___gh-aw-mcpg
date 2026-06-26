package logger

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// FileLogger manages logging to a file with fallback to stderr
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

// handleFileLoggerError falls back to stderr when the log file cannot be opened.
// Stderr is used (not stdout) to avoid corrupting the stdout JSON channel that
// callers use to receive the gateway configuration output.
func handleFileLoggerError(err error, logDir, fileName string) (*FileLogger, error) {
	return fallbackLoggerOnInitError(err, "Failed to initialize log file", "Falling back to stderr for logging", &FileLogger{
		logDir:      logDir,
		fileName:    fileName,
		useFallback: true,
		logger:      log.New(os.Stderr, "", 0),
	})
}

// fileLoggerFactory bundles the setup and error-handler for FileLogger.
var fileLoggerFactory = loggerFactory[*FileLogger]{
	setup:   setupFileLogger,
	onError: handleFileLoggerError,
}

// InitFileLogger initializes the global file logger
// If the log directory doesn't exist and can't be created, falls back to stderr
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
	return os.Stderr
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

// The exported wrappers below follow the Log-Level Quad-Function Pattern
// documented in common.go, with shared per-level closure registration handled
// by newLevelLoggerFuncs.
var fileLevelLoggers = newLevelLoggerFuncs(logWithLevel)

// LogInfo logs an informational message to the unified file logger sink.
// The underlying filename depends on logger initialization. For
// destination-specific logging use LogInfoToMarkdown or LogInfoToServer.
func LogInfo(category, format string, args ...interface{}) {
	fileLevelLoggers.info(category, format, args...)
}

// LogWarn logs a warning message to the unified file logger sink.
// The underlying filename depends on logger initialization. For
// destination-specific logging use LogWarnToMarkdown or LogWarnToServer.
func LogWarn(category, format string, args ...interface{}) {
	fileLevelLoggers.warn(category, format, args...)
}

// LogError logs an error message to the unified file logger sink.
// The underlying filename depends on logger initialization. For
// destination-specific logging use LogErrorToMarkdown or LogErrorToServer.
func LogError(category, format string, args ...interface{}) {
	fileLevelLoggers.error(category, format, args...)
}

// LogDebug logs a debug message to the unified file logger sink.
// The underlying filename depends on logger initialization. For
// destination-specific logging use LogDebugToMarkdown or LogDebugToServer.
func LogDebug(category, format string, args ...interface{}) {
	fileLevelLoggers.debug(category, format, args...)
}

// CloseGlobalLogger closes the global file logger
func CloseGlobalLogger() error {
	return closeGlobalLogger(&globalLoggerMu, &globalFileLogger)
}
