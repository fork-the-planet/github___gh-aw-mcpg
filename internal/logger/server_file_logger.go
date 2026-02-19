package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ServerFileLogger manages per-serverID log files
type ServerFileLogger struct {
	logDir      string
	loggers     map[string]*log.Logger
	files       map[string]*os.File
	mu          sync.RWMutex
	useFallback bool
}

var (
	globalServerFileLogger *ServerFileLogger
	globalServerLoggerMu   sync.RWMutex
)

// InitServerFileLogger initializes the global server file logger
func InitServerFileLogger(logDir string) error {
	// Create log directory if it doesn't exist
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("WARNING: Failed to create log directory for server logs: %v", err)
		log.Printf("WARNING: Falling back to unified logging only")
		// Create a fallback logger that won't create files
		sfl := &ServerFileLogger{
			logDir:      logDir,
			loggers:     make(map[string]*log.Logger),
			files:       make(map[string]*os.File),
			useFallback: true,
		}
		initGlobalServerFileLogger(sfl)
		return nil
	}

	sfl := &ServerFileLogger{
		logDir:      logDir,
		loggers:     make(map[string]*log.Logger),
		files:       make(map[string]*os.File),
		useFallback: false,
	}

	log.Printf("Initialized per-serverID logging in directory: %s", logDir)
	initGlobalServerFileLogger(sfl)
	return nil
}

// getOrCreateLogger returns a logger for the given serverID, creating it if necessary
func (sfl *ServerFileLogger) getOrCreateLogger(serverID string) (*log.Logger, error) {
	// Fast path: check if logger already exists (read lock)
	sfl.mu.RLock()
	if logger, exists := sfl.loggers[serverID]; exists {
		sfl.mu.RUnlock()
		return logger, nil
	}
	sfl.mu.RUnlock()

	// Slow path: create new logger (write lock)
	sfl.mu.Lock()
	defer sfl.mu.Unlock()

	// Double-check in case another goroutine created it while we waited for the lock
	if logger, exists := sfl.loggers[serverID]; exists {
		return logger, nil
	}

	// If in fallback mode, return nil to indicate no per-server logging
	if sfl.useFallback {
		return nil, fmt.Errorf("server file logger in fallback mode")
	}

	// Create log file for this serverID
	fileName := fmt.Sprintf("%s.log", serverID)
	logPath := filepath.Join(sfl.logDir, fileName)
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file for server %s: %w", serverID, err)
	}

	// Create logger
	logger := log.New(file, "", 0)

	// Store in maps
	sfl.loggers[serverID] = logger
	sfl.files[serverID] = file

	return logger, nil
}

// Log writes a log message to the server-specific log file
func (sfl *ServerFileLogger) Log(serverID string, level LogLevel, category, format string, args ...interface{}) {
	if sfl == nil {
		return
	}

	logger, err := sfl.getOrCreateLogger(serverID)
	if err != nil {
		// If we can't create a logger, fall back to the global logger
		// but include the serverID in the message
		LogDebug(category, "[%s] "+format, append([]interface{}{serverID}, args...)...)
		return
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	message := fmt.Sprintf(format, args...)
	logLine := fmt.Sprintf("[%s] [%s] [%s] %s", timestamp, level, category, message)

	logger.Println(logLine)

	// Flush to disk immediately
	sfl.mu.RLock()
	if file, exists := sfl.files[serverID]; exists {
		if err := file.Sync(); err != nil {
			log.Printf("WARNING: Failed to sync log file for server %s: %v", serverID, err)
		}
	}
	sfl.mu.RUnlock()
}

// Close closes all server log files
func (sfl *ServerFileLogger) Close() error {
	if sfl == nil {
		return nil
	}

	sfl.mu.Lock()
	defer sfl.mu.Unlock()

	var firstErr error
	for serverID, file := range sfl.files {
		if err := file.Sync(); err != nil {
			log.Printf("WARNING: Failed to sync log file for server %s: %v", serverID, err)
			if firstErr == nil {
				firstErr = err
			}
		}
		if err := file.Close(); err != nil {
			log.Printf("WARNING: Failed to close log file for server %s: %v", serverID, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// Clear maps
	sfl.loggers = make(map[string]*log.Logger)
	sfl.files = make(map[string]*os.File)

	return firstErr
}

// Global logging functions that use the global server file logger

// logWithLevelAndServer is a helper that reduces code duplication for per-server logging at different levels.
// It handles mutex locking, nil-checking, and dual logging (server-specific + unified) in a single place,
// eliminating repeated patterns across LogInfoWithServer, LogWarnWithServer, LogErrorWithServer, and LogDebugWithServer.
// It uses the logFuncs map (file_logger.go) for the unified log dispatch, avoiding a repeated switch-on-level block.
func logWithLevelAndServer(serverID string, level LogLevel, category, format string, args ...interface{}) {
	globalServerLoggerMu.RLock()
	defer globalServerLoggerMu.RUnlock()

	if globalServerFileLogger != nil {
		globalServerFileLogger.Log(serverID, level, category, format, args...)
	}

	// Also log to the main log file for unified view
	// Use logFuncs to dispatch to the appropriate level function
	formattedMessage := fmt.Sprintf(format, args...)
	if logFunc := logFuncs[level]; logFunc != nil {
		logFunc(category, "[%s] %s", serverID, formattedMessage)
	}
}

// LogInfoWithServer logs an informational message to the server-specific log file
func LogInfoWithServer(serverID, category, format string, args ...interface{}) {
	logWithLevelAndServer(serverID, LogLevelInfo, category, format, args...)
}

// LogWarnWithServer logs a warning message to the server-specific log file
func LogWarnWithServer(serverID, category, format string, args ...interface{}) {
	logWithLevelAndServer(serverID, LogLevelWarn, category, format, args...)
}

// LogErrorWithServer logs an error message to the server-specific log file
func LogErrorWithServer(serverID, category, format string, args ...interface{}) {
	logWithLevelAndServer(serverID, LogLevelError, category, format, args...)
}

// LogDebugWithServer logs a debug message to the server-specific log file
func LogDebugWithServer(serverID, category, format string, args ...interface{}) {
	logWithLevelAndServer(serverID, LogLevelDebug, category, format, args...)
}

// CloseServerFileLogger closes the global server file logger
func CloseServerFileLogger() error {
	return closeGlobalServerFileLogger()
}
