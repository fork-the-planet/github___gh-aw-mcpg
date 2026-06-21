package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewLevelLoggerFuncs_BindsExpectedLevels(t *testing.T) {
	var levels []LogLevel
	loggers := newLevelLoggerFuncs(func(level LogLevel, category, format string, args ...interface{}) {
		levels = append(levels, level)
	})

	loggers.info("cat", "msg")
	loggers.warn("cat", "msg")
	loggers.error("cat", "msg")
	loggers.debug("cat", "msg")

	assert.Equal(t, []LogLevel{
		LogLevelInfo,
		LogLevelWarn,
		LogLevelError,
		LogLevelDebug,
	}, levels)
}

func TestNewServerLevelLoggerFuncs_BindsExpectedLevels(t *testing.T) {
	var levels []LogLevel
	loggers := newServerLevelLoggerFuncs(func(serverID string, level LogLevel, category, format string, args ...interface{}) {
		levels = append(levels, level)
	})

	loggers.info("server", "cat", "msg")
	loggers.warn("server", "cat", "msg")
	loggers.error("server", "cat", "msg")
	loggers.debug("server", "cat", "msg")

	assert.Equal(t, []LogLevel{
		LogLevelInfo,
		LogLevelWarn,
		LogLevelError,
		LogLevelDebug,
	}, levels)
}

func TestLogLevelWrappers_CoverAllRegisteredLevels(t *testing.T) {
	fileWrappers := map[LogLevel]func(string, string, ...interface{}){
		LogLevelInfo:  LogInfo,
		LogLevelWarn:  LogWarn,
		LogLevelError: LogError,
		LogLevelDebug: LogDebug,
	}

	markdownWrappers := map[LogLevel]func(string, string, ...interface{}){
		LogLevelInfo:  LogInfoToMarkdown,
		LogLevelWarn:  LogWarnToMarkdown,
		LogLevelError: LogErrorToMarkdown,
		LogLevelDebug: LogDebugToMarkdown,
	}

	serverWrappers := map[LogLevel]func(string, string, string, ...interface{}){
		LogLevelInfo:  LogInfoToServer,
		LogLevelWarn:  LogWarnToServer,
		LogLevelError: LogErrorToServer,
		LogLevelDebug: LogDebugToServer,
	}

	expectedLevels := make([]LogLevel, 0, len(logFuncs))
	for level := range logFuncs {
		expectedLevels = append(expectedLevels, level)
	}

	fileLevels := make([]LogLevel, 0, len(fileWrappers))
	for level := range fileWrappers {
		fileLevels = append(fileLevels, level)
	}
	assert.ElementsMatch(t, expectedLevels, fileLevels)

	markdownLevels := make([]LogLevel, 0, len(markdownWrappers))
	for level := range markdownWrappers {
		markdownLevels = append(markdownLevels, level)
	}
	assert.ElementsMatch(t, expectedLevels, markdownLevels)

	serverLevels := make([]LogLevel, 0, len(serverWrappers))
	for level := range serverWrappers {
		serverLevels = append(serverLevels, level)
	}
	assert.ElementsMatch(t, expectedLevels, serverLevels)
}
