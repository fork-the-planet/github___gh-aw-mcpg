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
	lockable
	logFile     *os.File
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

// markdownLoggerFactory bundles the setup and error-handler for MarkdownLogger.
var markdownLoggerFactory = loggerFactory[*MarkdownLogger]{
	setup:   setupMarkdownLogger,
	onError: handleMarkdownLoggerError,
}

// InitMarkdownLogger initializes the global markdown logger
func InitMarkdownLogger(logDir, fileName string) error {
	logger, err := initLogger(logDir, fileName, os.O_TRUNC, markdownLoggerFactory)
	initGlobalLogger(&globalMarkdownMu, &globalMarkdownLogger, logger)
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
func logWithMarkdown(level LogLevel, category, format string, args ...interface{}) {
	// Log to regular logger
	logFuncs[level](category, format, args...)

	// Log to markdown logger using withGlobalLogger helper
	withGlobalLogger(&globalMarkdownMu, &globalMarkdownLogger, func(logger *MarkdownLogger) {
		logger.Log(level, category, format, args...)
	})
}

// The var block and exported wrappers below follow the Log-Level Quad-Function Pattern
// documented in common.go. The four-level set (Info/Warn/Error/Debug) is stable and
// intentionally repeated across file_logger.go, markdown_logger.go, and
// server_file_logger.go. See common.go for the rationale and update instructions.
var (
	logInfoToMarkdown  = makeLevelLogger(logWithMarkdown, LogLevelInfo)
	logWarnToMarkdown  = makeLevelLogger(logWithMarkdown, LogLevelWarn)
	logErrorToMarkdown = makeLevelLogger(logWithMarkdown, LogLevelError)
	logDebugToMarkdown = makeLevelLogger(logWithMarkdown, LogLevelDebug)
)

// LogInfoToMarkdown logs to both regular and markdown loggers.
func LogInfoToMarkdown(category, format string, args ...interface{}) {
	logInfoToMarkdown(category, format, args...)
}

// LogWarnToMarkdown logs to both regular and markdown loggers.
func LogWarnToMarkdown(category, format string, args ...interface{}) {
	logWarnToMarkdown(category, format, args...)
}

// LogErrorToMarkdown logs to both regular and markdown loggers.
func LogErrorToMarkdown(category, format string, args ...interface{}) {
	logErrorToMarkdown(category, format, args...)
}

// LogDebugToMarkdown logs to both regular and markdown loggers.
func LogDebugToMarkdown(category, format string, args ...interface{}) {
	logDebugToMarkdown(category, format, args...)
}

// CloseMarkdownLogger closes the global markdown logger
func CloseMarkdownLogger() error {
	return closeGlobalLogger(&globalMarkdownMu, &globalMarkdownLogger)
}
