package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Close Pattern for Logger Types
//
// All logger types in this package implement their Close() method using the withLock
// helper to ensure consistent mutex handling:
//
//	func (l *Logger) Close() error {
//	    return l.withLock(func() error {
//	        // Optional: Perform cleanup before closing (e.g., write footer)
//	        return closeLogFile(l.logFile, &l.mu, "loggerName")
//	    })
//	}
//
// The withLock helper (defined on each logger type) acquires the mutex, executes the
// callback, then releases the mutex — ensuring the lock is always released via defer.
//
// Why this pattern?
//
//  1. Consistent locking: withLock enforces acquire-on-enter / release-on-exit
//  2. Deferred unlock: Implemented inside withLock using defer, so it's never forgotten
//  3. Optional cleanup: Logger-specific cleanup (like MarkdownLogger's footer) goes inside the callback
//  4. Shared helper: Always delegate to closeLogFile() for consistent sync and close behavior
//  5. Error handling: Return errors from closeLogFile to indicate serious issues
//
// Examples:
//
// Simple Close() with no cleanup (FileLogger, JSONLLogger):
//
//	func (fl *FileLogger) Close() error {
//	    return fl.withLock(func() error {
//	        return closeLogFile(fl.logFile, &fl.mu, "file")
//	    })
//	}
//
// Close() with custom cleanup (MarkdownLogger):
//
//	func (ml *MarkdownLogger) Close() error {
//	    return ml.withLock(func() error {
//	        if ml.logFile != nil {
//	            footer := "\n</details>\n"
//	            if _, err := ml.logFile.WriteString(footer); err != nil {
//	                return closeLogFile(ml.logFile, &ml.mu, "markdown")
//	            }
//	            return closeLogFile(ml.logFile, &ml.mu, "markdown")
//	        }
//	        return nil
//	    })
//	}
//
// When adding a new logger type, add a withLock helper and follow this pattern to ensure
// consistent, safe Close() behavior.

// Initialization Pattern for Logger Types
//
// The logger package follows a consistent initialization pattern across all logger types,
// with support for customizable fallback behavior. This section documents the pattern
// and explains the different fallback strategies used by each logger type.
//
// Standard Initialization Pattern:
//
// All logger types use the initLogger() generic helper function for initialization.
// The setup and error-handler callbacks are defined as named package-level functions
// (e.g., setupFileLogger, handleFileLoggerError) and bundled into a package-level
// loggerFactory[T] variable to aid readability and testability:
// The small per-logger Init*/setup*/handle* wrappers are intentional even though they
// look similar: each logger type exposes a stable public API and keeps fallback semantics
// explicit at the call site (stdout fallback, silent fallback, strict error, etc.).
//
//	var fileLoggerFactory = loggerFactory[*FileLogger]{
//	    setup:   setupFileLogger,
//	    onError: handleFileLoggerError,
//	}
//
//	func InitFileLogger(logDir, fileName string) error {
//	    logger, err := initLogger(logDir, fileName, os.O_APPEND, fileLoggerFactory)
//	    initGlobalLogger(&globalLoggerMu, &globalFileLogger, logger)
//	    return err
//	}
//
// The initLogger() helper:
//  1. Attempts to create the log directory (if needed)
//  2. Opens the log file with specified flags (os.O_APPEND, os.O_TRUNC, etc.)
//  3. Calls factory.setup to configure the logger instance
//  4. On error, calls factory.onError to implement fallback behavior
//  5. Returns the initialized logger and any error
//
// Fallback Behavior Strategies:
//
// Different logger types implement different fallback strategies based on their purpose:
//
// 1. FileLogger - Stderr Fallback:
//    - Purpose: Operational logs must always be visible
//    - Fallback: Redirects to stderr if log directory/file creation fails
//              (stderr is used, not stdout, to avoid corrupting the stdout
//               JSON channel that callers use to receive gateway config output)
//    - Error: Returns nil (never fails, always provides output)
//    - Use case: Critical operational messages that must be seen
//
//    Example error handler:
//    func(err error, logDir, fileName string) (*FileLogger, error) {
//        log.Printf("WARNING: Failed to initialize log file: %v", err)
//        log.Printf("WARNING: Falling back to stderr for logging")
//        return &FileLogger{
//            logDir:      logDir,
//            fileName:    fileName,
//            useFallback: true,
//            logger:      log.New(os.Stderr, "", 0),
//        }, nil
//    }
//
// 2. MarkdownLogger - Silent Fallback:
//    - Purpose: GitHub workflow preview logs (optional enhancement)
//    - Fallback: Sets useFallback flag, produces no output
//    - Error: Returns nil (never fails, silently disables)
//    - Use case: Nice-to-have logs that shouldn't block operations
//
//    Example error handler:
//    func(err error, logDir, fileName string) (*MarkdownLogger, error) {
//        return &MarkdownLogger{
//            logDir:      logDir,
//            fileName:    fileName,
//            useFallback: true,
//        }, nil
//    }
//
// 3. JSONLLogger - Strict Mode:
//    - Purpose: Machine-readable RPC message logs for analysis
//    - Fallback: None - returns error immediately
//    - Error: Returns error to caller
//    - Use case: Structured data that requires file storage
//
//    Example error handler:
//    func(err error, logDir, fileName string) (*JSONLLogger, error) {
//        return nil, err
//    }
//
// 4. ServerFileLogger - Unified Fallback:
//    - Purpose: Per-server log files for troubleshooting
//    - Fallback: Sets useFallback flag, logs to unified logger only
//    - Error: Returns nil (never fails, falls back to unified logging)
//    - Use case: Per-server logs are helpful but not required
//
//    Note: ServerFileLogger doesn't use initLogger() because it creates
//    files on-demand, but follows the same fallback philosophy.
//
// Global Logger Management:
//
// After initialization, all logger types register themselves as global loggers
// using the generic initGlobal*Logger() helpers from global_helpers.go:
//
//  - initGlobalFileLogger()
//  - initGlobalJSONLLogger()
//  - initGlobalMarkdownLogger()
//  - initGlobalServerFileLogger()
//  - initGlobalToolsLogger()
//
// These helpers ensure thread-safe initialization with proper cleanup of any
// existing logger instance.
//
// Gateway/proxy startup and CloseAllLoggers use shared registries in registry.go
// so that new logger types only need one centralized startup/cleanup entry per
// command path, while the per-logger Init*/Close* public APIs remain explicit.
//
// When to Use Each Logger Type:
//
// - FileLogger: Required operational logs (startup, errors, warnings)
// - MarkdownLogger: Optional GitHub workflow preview logs
// - JSONLLogger: Structured RPC message logs for tooling/analysis
// - ServerFileLogger: Per-server troubleshooting logs
//
// Adding a New Logger Type:
//
// When adding a new logger type:
//  1. Implement Close() method following the Close Pattern (above)
//  2. Add type to closableLogger constraint in global_helpers.go
//  3. Use initLogger() for initialization with appropriate fallback strategy
//  4. Add initGlobal*Logger() helper and inline the close closure in registry.go
//  5. Document the fallback strategy and use case
//
// This consistent pattern ensures:
//  - Predictable behavior across all loggers
//  - Easy to understand fallback strategies
//  - Minimal code duplication
//  - Type-safe global logger management
//
// See file_logger.go, jsonl_logger.go, markdown_logger.go, and server_file_logger.go
// for complete implementation examples.

// Log-Level Quad-Function Pattern
//
// Three sets of four public functions — one set per logger variant — share an
// identical structure where each exported one-liner delegates to an unexported
// per-level closure registered by helpers in this file:
//
//	func Log<Level>(category, format string, args ...interface{}) {
//	    log<level>(category, format, args...)
//	}
//
// The three sets and their internal dispatch helpers are:
//
//	file_logger.go       LogInfo / LogWarn / LogError / LogDebug          → logWithLevel
//	markdown_logger.go   LogInfoToMarkdown / ... / LogDebugToMarkdown     → logWithMarkdown
//	server_file_logger.go LogInfoToServer / ... / LogDebugToServer        → logWithLevelAndServer
//
// This pattern keeps exported APIs immutable (`func` declarations) while moving
// the repetitive per-level closure setup into shared helpers. Each logger file
// still exposes its own stable public API surface, but the registration of the
// Info/Warn/Error/Debug closures is centralized here.
//
// The shared logFuncs map below centralises the LogLevel → log-function
// mapping so that the internal helpers (logWithMarkdown, logWithLevelAndServer)
// do not need their own switch-on-level blocks.
//
// If a new LogLevel constant is ever added (e.g., LogLevelTrace), update all
// required locations to keep the public API consistent:
//  1. Add a new entry to the logFuncs map in this file.
//  2. Update newLogFuncSet in this file.
//  3. In file_logger.go: add an exported wrapper (see LogInfo pattern).
//  4. In markdown_logger.go: add an exported wrapper (see LogInfoToMarkdown pattern).
//  5. In server_file_logger.go: add an exported wrapper (see LogInfoToServer pattern).
//  6. Update TestLogLevelWrappers_CoverAllRegisteredLevels in log_level_wrappers_test.go.
//
// logFuncs maps each LogLevel to its corresponding global log function.
// This eliminates repeated switch-on-level blocks in logWithMarkdown
// (markdown_logger.go) and logWithLevelAndServer (server_file_logger.go).
// When adding a new LogLevel constant, add a corresponding entry here so
// that all dispatch sites automatically support the new level.

// logFuncSet is a generic bundle of per-level logging closures all sharing the
// same function signature F. It is the single source of truth for the
// info/warn/error/debug quad used by every logger variant.
type logFuncSet[F any] struct {
	info  F
	warn  F
	error F
	debug F
}

// newLogFuncSet builds a logFuncSet by calling makeFunc once per log level.
// This eliminates the structural duplication between newLevelLoggerFuncs and
// newServerLevelLoggerFuncs — both now delegate here with their own closure.
func newLogFuncSet[F any](makeFunc func(LogLevel) F) logFuncSet[F] {
	return logFuncSet[F]{
		info:  makeFunc(LogLevelInfo),
		warn:  makeFunc(LogLevelWarn),
		error: makeFunc(LogLevelError),
		debug: makeFunc(LogLevelDebug),
	}
}

// levelLoggerFuncs holds per-level closures for non-server loggers.
type levelLoggerFuncs = logFuncSet[func(string, string, ...interface{})]

func newLevelLoggerFuncs(
	dispatch func(level LogLevel, category, format string, args ...interface{}),
) levelLoggerFuncs {
	return newLogFuncSet(func(level LogLevel) func(string, string, ...interface{}) {
		return func(category, format string, args ...interface{}) {
			dispatch(level, category, format, args...)
		}
	})
}

// serverLevelLoggerFuncs holds per-level closures for server-scoped loggers.
type serverLevelLoggerFuncs = logFuncSet[func(string, string, string, ...interface{})]

func newServerLevelLoggerFuncs(
	dispatch func(serverID string, level LogLevel, category, format string, args ...interface{}),
) serverLevelLoggerFuncs {
	return newLogFuncSet(func(level LogLevel) func(string, string, string, ...interface{}) {
		return func(serverID, category, format string, args ...interface{}) {
			dispatch(serverID, level, category, format, args...)
		}
	})
}

var logFuncs = map[LogLevel]func(string, string, ...interface{}){
	LogLevelInfo:  LogInfo,
	LogLevelWarn:  LogWarn,
	LogLevelError: LogError,
	LogLevelDebug: LogDebug,
}

// Global Logger RWMutex Access Pattern
//
// All access to global logger instances uses the withGlobalLogger helper function
// (defined in global_helpers.go) to eliminate duplicated RWMutex locking patterns.
//
// Before refactoring (duplicated pattern):
//
//	globalLoggerMu.RLock()
//	defer globalLoggerMu.RUnlock()
//
//	if globalLogger != nil {
//	    globalLogger.DoSomething(args...)
//	}
//
// After refactoring (unified pattern):
//
//	withGlobalLogger(&globalLoggerMu, &globalLogger, func(logger *Logger) {
//	    logger.DoSomething(args...)
//	})
//
// The withGlobalLogger helper is used in:
//   - file_logger.go: logWithLevel (for FileLogger)
//   - markdown_logger.go: logWithMarkdown (for MarkdownLogger)
//   - jsonl_logger.go: LogRPCMessageJSONLWithTags (for JSONLLogger)
//   - server_file_logger.go: logWithLevelAndServer (for ServerFileLogger)
//   - tools_logger.go: LogToolsForServer (for ToolsLogger)
//   - rpc_logger.go: logRPCMessageToAll and LogRPCMessage (for MarkdownLogger)
//
// Benefits:
//   - Eliminates ~40 lines of duplicated mutex code
//   - Provides a single point of control for the locking pattern
//   - Type-safe through generics (enforces closableLogger constraint)
//   - Easier to modify the locking strategy globally (e.g., add timeouts)
//
// When adding a new logger access point:
//  1. Use withGlobalLogger instead of manual RLock/RUnlock
//  2. Pass the appropriate mutex, logger pointer, and callback function

// formatLogLine builds the standard log line used by FileLogger and ServerFileLogger.
// Centralizing the format ensures consistency across all file-based loggers.
func formatLogLine(level LogLevel, category, format string, args ...interface{}) string {
	timestamp := time.Now().UTC().Format(jsonTimestampLayout)
	message := fmt.Sprintf(format, args...)
	return fmt.Sprintf("[%s] [%s] [%s] %s", timestamp, level, category, message)
}

// It syncs buffered data before closing and handles errors appropriately.
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

// loggerSetupFunc is a function type that sets up a logger instance after the log file is opened.
// It receives the opened file, logDir, and fileName, and returns the configured logger.
type loggerSetupFunc[T closableLogger] func(file *os.File, logDir, fileName string) (T, error)

// loggerErrorHandlerFunc is a function type that handles errors during logger initialization.
// It receives the error and returns a configured logger (possibly a fallback) or an error.
//
// Fallback-capable handlers must return a degraded logger with useFallback=true and a nil
// error so initLogger can proceed in degraded mode. Strict handlers such as JSONLLogger
// should return the zero logger value together with the initialization error instead.
type loggerErrorHandlerFunc[T closableLogger] func(err error, logDir, fileName string) (T, error)

// loggerFactory bundles the setup and error-handler function pair for a logger type.
// It groups the two callbacks that control success and failure behavior so they can
// be passed to initLogger as a single value instead of two separate arguments.
//
// Define one package-level factory variable per concrete logger type:
//
//	var fileLoggerFactory = loggerFactory[*FileLogger]{
//	    setup:   setupFileLogger,
//	    onError: handleFileLoggerError,
//	}
//
// Then call initLogger with the factory:
//
//	logger, err := initLogger(logDir, fileName, os.O_APPEND, fileLoggerFactory)
type loggerFactory[T closableLogger] struct {
	setup   loggerSetupFunc[T]
	onError loggerErrorHandlerFunc[T]
}

// initLogger is a generic function that handles common logger initialization logic.
// It reduces code duplication across FileLogger, JSONLLogger, and MarkdownLogger initialization.
//
// Type parameters:
//   - T: Any type that satisfies the closableLogger constraint
//
// Parameters:
//   - logDir: Directory where the log file should be created
//   - fileName: Name of the log file
//   - flags: File opening flags (e.g., os.O_APPEND, os.O_TRUNC)
//   - factory: Bundles the setup and error-handler functions for the logger type
//
// Returns:
//   - T: The initialized logger instance
//   - error: Any error that occurred during initialization
//
// This function:
//  1. Validates that factory.setup and factory.onError are non-nil
//  2. Attempts to open the log file with the specified flags
//  3. If successful, calls factory.setup to configure the logger
//  4. If unsuccessful, calls factory.onError to implement the logger's fallback strategy
//  5. Returns an error if factory.setup returns a nil logger without an error (would leak the file)
//
// The factory.onError handler determines the fallback behavior. Fallback-capable handlers
// return a logger with useFallback=true and a nil error; strict handlers return the error.
// See "Initialization Pattern for Logger Types" documentation above for details on
// fallback strategies:
//   - FileLogger: Falls back to stderr
//   - MarkdownLogger: Silent fallback (no output)
//   - ToolsLogger: Silent fallback (no output)
//   - JSONLLogger: Returns error (no fallback)
func initLogger[T closableLogger](
	logDir, fileName string,
	flags int,
	factory loggerFactory[T],
) (T, error) {
	var zero T
	if factory.setup == nil || factory.onError == nil {
		return zero, fmt.Errorf("loggerFactory.setup and loggerFactory.onError must both be non-nil")
	}

	file, err := initLogFile(logDir, fileName, flags)
	if err != nil {
		return factory.onError(err, logDir, fileName)
	}

	logger, err := factory.setup(file, logDir, fileName)
	if err != nil {
		// If setup fails, close the file and return the error
		file.Close()
		return zero, err
	}

	// Guard against a setup function that returns (nil, nil), which would leak the
	// opened file descriptor. Treat this as a setup failure.
	if logger == zero {
		file.Close()
		return zero, fmt.Errorf("loggerFactory.setup returned a nil logger without an error")
	}

	return logger, nil
}
