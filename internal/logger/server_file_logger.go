package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/github/gh-aw-mcpg/internal/syncutil"
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

func newServerFileLogger(logDir string, useFallback bool) *ServerFileLogger {
	return &ServerFileLogger{
		logDir:      logDir,
		loggers:     make(map[string]*log.Logger),
		files:       make(map[string]*os.File),
		useFallback: useFallback,
	}
}

// InitServerFileLogger initializes the global server file logger
func InitServerFileLogger(logDir string) error {
	// Create log directory if it doesn't exist
	if err := os.MkdirAll(logDir, 0755); err != nil {
		logFallbackWarnings(err, "Failed to create log directory for server logs", "Falling back to unified logging only")
		sfl := newServerFileLogger(logDir, true)
		initGlobalLogger(&globalServerLoggerMu, &globalServerFileLogger, sfl)
		return nil
	}

	sfl := newServerFileLogger(logDir, false)

	log.Printf("Initialized per-serverID logging in directory: %s", logDir)
	initGlobalLogger(&globalServerLoggerMu, &globalServerFileLogger, sfl)
	return nil
}

// getOrCreateLogger returns a logger for the given serverID, creating it if necessary
func (sfl *ServerFileLogger) getOrCreateLogger(serverID string) (*log.Logger, error) {
	return syncutil.MapGetOrCreate(&sfl.mu, sfl.loggers, serverID, func() (*log.Logger, error) {
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

		// Create logger and record the file handle (write lock is held by GetOrCreate)
		logger := log.New(file, "", 0)
		sfl.files[serverID] = file

		return logger, nil
	})
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

	logLine := formatLogLine(level, category, format, args...)
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
		if err := closeLogFile(file, nil, "server "+serverID); err != nil {
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
// It uses the withGlobalLogger helper from global_helpers.go to handle mutex locking and nil-checking,
// eliminating repeated patterns across LogInfoToServer, LogWarnToServer, LogErrorToServer, and LogDebugToServer.
// It uses the logFuncs map (common.go) for the unified log dispatch, avoiding a repeated switch-on-level block.
func logWithLevelAndServer(serverID string, level LogLevel, category, format string, args ...interface{}) {
	withGlobalLogger(&globalServerLoggerMu, &globalServerFileLogger, func(logger *ServerFileLogger) {
		logger.Log(serverID, level, category, format, args...)
	})

	// Also log to the main log file for unified view
	// Use logFuncs to dispatch to the appropriate level function
	formattedMessage := fmt.Sprintf(format, args...)
	if logFunc := logFuncs[level]; logFunc != nil {
		logFunc(category, "[%s] %s", serverID, formattedMessage)
	}
}

// The exported vars below follow the Log-Level Quad-Var Pattern
// documented in global_state.go. Each var is a direct alias of the
// corresponding per-level closure in serverLevelLoggers, eliminating
// the four boilerplate wrapper functions.
var serverLevelLoggers = newServerLevelLoggerFuncs(logWithLevelAndServer)

var (
	// LogInfoToServer logs an informational message to the server-specific log file.
	LogInfoToServer = serverLevelLoggers.info

	// LogWarnToServer logs a warning message to the server-specific log file.
	LogWarnToServer = serverLevelLoggers.warn

	// LogErrorToServer logs an error message to the server-specific log file.
	LogErrorToServer = serverLevelLoggers.error

	// LogDebugToServer logs a debug message to the server-specific log file.
	LogDebugToServer = serverLevelLoggers.debug
)
