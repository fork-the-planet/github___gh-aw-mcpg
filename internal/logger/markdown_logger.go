package logger

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/github/gh-aw-mcpg/internal/logger/sanitize"
)

// MarkdownLogger manages logging to a markdown file for GitHub workflow previews
type MarkdownLogger struct {
	logFile     *os.File
	mu          sync.Mutex
	logDir      string
	fileName    string
	useFallback bool
	initialized bool
}

var (
	globalMarkdownLogger *MarkdownLogger
	globalMarkdownMu     sync.RWMutex
)

// setupMarkdownLogger configures a MarkdownLogger after the log file has been opened.
func setupMarkdownLogger(file *os.File, logDir, fileName string) (*MarkdownLogger, error) {
	ml := &MarkdownLogger{
		logDir:      logDir,
		fileName:    fileName,
		logFile:     file,
		initialized: false, // Will be initialized on first write
	}
	return ml, nil
}

// handleMarkdownLoggerError sets fallback mode (no stdout redirect) when the file cannot be opened.
func handleMarkdownLoggerError(_ error, logDir, fileName string) (*MarkdownLogger, error) {
	ml := &MarkdownLogger{
		logDir:      logDir,
		fileName:    fileName,
		useFallback: true,
	}
	return ml, nil
}

// InitMarkdownLogger initializes the global markdown logger
func InitMarkdownLogger(logDir, fileName string) error {
	logger, err := initLogger(logDir, fileName, os.O_TRUNC, setupMarkdownLogger, handleMarkdownLoggerError)
	initGlobalMarkdownLogger(logger)
	return err
}

// initializeFile writes the HTML details header on first write
func (ml *MarkdownLogger) initializeFile() error {
	if ml.initialized {
		return nil
	}

	if ml.logFile != nil {
		header := "<details>\n<summary>MCP Gateway</summary>\n\n"
		if _, err := ml.logFile.WriteString(header); err != nil {
			return err
		}
		ml.initialized = true
	}
	return nil
}

// withLock acquires ml.mu, executes fn, then releases ml.mu.
// Use this in methods that return an error to avoid repeating the lock/unlock preamble.
func (ml *MarkdownLogger) withLock(fn func() error) error {
	return withMutexLock(&ml.mu, fn)
}

// Close closes the log file and writes the closing details tag
func (ml *MarkdownLogger) Close() error {
	return ml.withLock(func() error {
		if ml.logFile != nil {
			// Write closing details tag before closing
			footer := "\n</details>\n"
			if _, err := ml.logFile.WriteString(footer); err != nil {
				// Even if footer write fails, try to close the file properly
				return closeLogFile(ml.logFile, &ml.mu, "markdown")
			}

			// Footer written successfully, now close
			return closeLogFile(ml.logFile, &ml.mu, "markdown")
		}
		return nil
	})
}

// getEmojiForLevel returns the appropriate emoji for the log level
func getEmojiForLevel(level LogLevel) string {
	switch level {
	case LogLevelInfo:
		return "✓"
	case LogLevelWarn:
		return "⚠️"
	case LogLevelError:
		return "✗"
	case LogLevelDebug:
		return "🔍"
	default:
		return "•"
	}
}

// Log writes a log message in markdown format with emoji bullet points
func (ml *MarkdownLogger) Log(level LogLevel, category, format string, args ...interface{}) {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	if ml.useFallback {
		return
	}

	// Initialize file with header on first write
	if err := ml.initializeFile(); err != nil {
		return
	}

	message := fmt.Sprintf(format, args...)

	// Sanitize potential secrets
	message = sanitize.SanitizeString(message)

	emoji := getEmojiForLevel(level)

	// Format as markdown bullet point with emoji
	// Use code blocks for multi-line content or technical details
	var logLine string

	// Check if message is already pre-formatted (RPC messages with markdown formatting)
	// RPC messages start with ** and contain → or ← arrows
	isPreformatted := strings.HasPrefix(message, "**") && (strings.Contains(message, "→") || strings.Contains(message, "←"))

	if isPreformatted {
		// Pre-formatted content (like RPC messages) - add bullet and emoji
		// If the message contains newlines (e.g., JSON code blocks), indent them properly
		if strings.Contains(message, "\n") {
			// Split the message into lines and indent continuation lines with 2 spaces
			lines := strings.Split(message, "\n")
			firstLine := lines[0]
			// Start with the first line
			logLine = fmt.Sprintf("- %s %s %s\n", emoji, category, firstLine)
			// Add remaining lines with proper indentation (2 spaces to nest under bullet)
			for i := 1; i < len(lines); i++ {
				logLine += "  " + lines[i] + "\n"
			}
		} else {
			// Single-line pre-formatted message
			logLine = fmt.Sprintf("- %s %s %s\n", emoji, category, message)
		}
	} else if strings.Contains(message, "\n") || strings.Contains(message, "command=") || strings.Contains(message, "args=") {
		// Multi-line or technical content - use code block
		logLine = fmt.Sprintf("- %s **%s**\n  ```\n  %s\n  ```\n", emoji, category, message)
	} else {
		// Simple single-line message
		logLine = fmt.Sprintf("- %s **%s** %s\n", emoji, category, message)
	}

	if ml.logFile != nil {
		if _, err := ml.logFile.WriteString(logLine); err != nil {
			return
		}
		// Flush immediately
		_ = ml.logFile.Sync() // Ignore sync errors
	}
}

// Global logging functions that also write to markdown logger

// logWithMarkdown is a helper that logs to both regular and markdown loggers.
// It uses the withGlobalLogger helper from global_helpers.go to handle mutex locking
// and nil-checking for the markdown logger access.
func logWithMarkdown(level LogLevel, regularLogFunc func(string, string, ...interface{}), category, format string, args ...interface{}) {
	// Log to regular logger
	regularLogFunc(category, format, args...)

	// Log to markdown logger using withGlobalLogger helper
	withGlobalLogger(&globalMarkdownMu, &globalMarkdownLogger, func(logger *MarkdownLogger) {
		logger.Log(level, category, format, args...)
	})
}

// logWithMarkdownLevel is a helper that reduces code duplication for markdown logging at different levels.
// It uses the logFuncs map (file_logger.go) to look up the regular log function for the given level,
// eliminating repeated switch-on-level patterns across LogInfoMd, LogWarnMd, LogErrorMd, and LogDebugMd.
func logWithMarkdownLevel(level LogLevel, category, format string, args ...interface{}) {
	logWithMarkdown(level, logFuncs[level], category, format, args...)
}

// LogInfoMd logs to both regular and markdown loggers
func LogInfoMd(category, format string, args ...interface{}) {
	logWithMarkdownLevel(LogLevelInfo, category, format, args...)
}

// LogWarnMd logs to both regular and markdown loggers
func LogWarnMd(category, format string, args ...interface{}) {
	logWithMarkdownLevel(LogLevelWarn, category, format, args...)
}

// LogErrorMd logs to both regular and markdown loggers
func LogErrorMd(category, format string, args ...interface{}) {
	logWithMarkdownLevel(LogLevelError, category, format, args...)
}

// LogDebugMd logs to both regular and markdown loggers
func LogDebugMd(category, format string, args ...interface{}) {
	logWithMarkdownLevel(LogLevelDebug, category, format, args...)
}

// CloseMarkdownLogger closes the global markdown logger
func CloseMarkdownLogger() error {
	return closeGlobalMarkdownLogger()
}
