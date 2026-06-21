// Package logger provides structured logging for the MCP Gateway.
//
// This file contains generic helper functions for managing global logger state with proper
// mutex handling. These helpers encapsulate common patterns for initializing and
// closing global loggers (FileLogger, JSONLLogger, MarkdownLogger) to reduce code
// duplication while maintaining thread safety.
//
// Functions in this file follow a consistent pattern:
//
// - init*: Initialize a global logger with proper locking and cleanup of any existing logger
// - close*: Close and clear a global logger with proper locking
//
// The unexported helpers (withMutexLock, withGlobalLogger, initGlobalLogger,
// closeGlobalLogger) are used internally by the logger package and should not be
// called directly by external code. Use the public Init* and Close* functions instead.
// CloseAllLoggers is the public entry point for closing all global loggers at once.
package logger

import (
	"log"
	"sync"
)

// lockable provides a mutex and a withLock helper method. Embed this struct
// in logger types that need a sync.Mutex plus a withLock convenience method
// to eliminate the repeated per-type withLock boilerplate.
//
// Usage:
//
//	type MyLogger struct {
//	    lockable
//	    // other fields...
//	}
//
// The embedded withLock method is promoted, so code in this package can write
// myLogger.withLock(fn) directly. The embedded mu field is also promoted, but
// because it is unexported it is only directly accessible within the logger package.
type lockable struct {
	mu sync.Mutex
}

// withLock acquires l.mu, executes fn, then releases l.mu.
func (l *lockable) withLock(fn func() error) error {
	return withMutexLock(&l.mu, fn)
}

// withMutexLock acquires mu, calls fn, and releases mu.
func withMutexLock(mu *sync.Mutex, fn func() error) error {
	mu.Lock()
	defer mu.Unlock()
	return fn()
}

// closableLogger is a constraint for types that have a Close method.
// This is satisfied by *FileLogger, *JSONLLogger, *MarkdownLogger, *ServerFileLogger, and *ToolsLogger.
type closableLogger interface {
	*FileLogger | *JSONLLogger | *MarkdownLogger | *ServerFileLogger | *ToolsLogger
	Close() error
}

// withGlobalLogger is a generic helper that encapsulates the common pattern for
// accessing a global logger with proper RWMutex locking and nil-checking.
//
// This eliminates the repeated pattern of:
//
//	globalLoggerMu.RLock()
//	defer globalLoggerMu.RUnlock()
//	if globalLogger != nil {
//	    globalLogger.DoSomething(args...)
//	}
//
// Type parameters:
//   - T: Any pointer type that satisfies closableLogger constraint
//
// Parameters:
//   - mu: RWMutex to protect access to the global logger
//   - logger: Pointer to the global logger instance
//   - fn: Function to execute with the logger if it's not nil
//
// Example usage:
//
//	withGlobalLogger(&globalLoggerMu, &globalFileLogger, func(l *FileLogger) {
//	    l.Log(level, category, format, args...)
//	})
func withGlobalLogger[T closableLogger](mu *sync.RWMutex, logger *T, fn func(T)) {
	mu.RLock()
	defer mu.RUnlock()

	if *logger != nil {
		fn(*logger)
	}
}

// initGlobalLogger is a generic helper that encapsulates the common pattern for
// initializing a global logger with proper mutex handling.
//
// Type parameters:
//   - T: Any pointer type that satisfies closableLogger constraint
//
// Parameters:
//   - mu: Mutex to protect access to the global logger
//   - current: Pointer to the current global logger instance
//   - newLogger: New logger instance to set as the global logger
//
// This function:
//  1. Acquires the mutex lock
//  2. Closes any existing logger if present
//  3. Sets the new logger as the global instance
//  4. Releases the mutex lock
func initGlobalLogger[T closableLogger](mu *sync.RWMutex, current *T, newLogger T) {
	mu.Lock()
	defer mu.Unlock()

	if *current != nil {
		(*current).Close()
	}
	*current = newLogger
}

// closeGlobalLogger is a generic helper that encapsulates the common pattern for
// closing and clearing a global logger with proper mutex handling.
//
// Type parameters:
//   - T: Any pointer type that satisfies closableLogger constraint
//
// Parameters:
//   - mu: Mutex to protect access to the global logger
//   - logger: Pointer to the global logger instance to close
//
// Returns:
//   - error: Any error returned by the logger's Close() method
//
// This function:
//  1. Acquires the mutex lock
//  2. Closes the logger if it exists
//  3. Sets the logger pointer to nil
//  4. Releases the mutex lock
//  5. Returns any error from the Close() operation
func closeGlobalLogger[T closableLogger](mu *sync.RWMutex, logger *T) error {
	mu.Lock()
	defer mu.Unlock()

	if *logger != nil {
		err := (*logger).Close()
		var zero T
		*logger = zero
		return err
	}
	return nil
}

// CloseAllLoggers closes all global loggers in a single call.
// Returns the first error encountered, but attempts to close every logger.
func CloseAllLoggers() error {
	return closeLoggerSet(globalLoggerClosers)
}

// StartupInfo logs a startup informational message to stderr (via log.Printf)
// and to the startup markdown/file log sink (via LogInfoToMarkdown with "startup" category).
// This eliminates the need to call log.Printf and LogInfoToMarkdown separately for the same message.
func StartupInfo(format string, args ...interface{}) {
	log.Printf(format, args...)
	LogInfoToMarkdown("startup", format, args...)
}

// StartupWarn logs a startup warning message to stderr (via log.Printf with
// "Warning: " prefix) and to the startup warning log sink (via LogWarn with
// "startup" category).
// This eliminates the need to call log.Printf and LogWarn separately for the same message.
func StartupWarn(format string, args ...interface{}) {
	log.Printf("Warning: "+format, args...)
	LogWarn("startup", format, args...)
}

// initWithWarning calls the Init* function result and prints a warning when it returns an error.
// It is used by InitGatewayLoggers and InitProxyLoggers to report non-fatal initialization
// failures without aborting startup.
func initWithWarning(err error, name string) {
	if err != nil {
		log.Printf("Warning: Failed to initialize %s: %v", name, err)
	}
}

// logFallbackWarnings prints two WARNING lines for logger initialization failure with fallback:
// the first includes the underlying error, the second describes the fallback behavior.
func logFallbackWarnings(err error, errMsg, fallbackMsg string) {
	log.Printf("WARNING: %s: %v", errMsg, err)
	log.Printf("WARNING: %s", fallbackMsg)
}
