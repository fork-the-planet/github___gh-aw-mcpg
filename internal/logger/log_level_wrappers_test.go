package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogLevelWrappers_CoverAllRegisteredLevels(t *testing.T) {
	fileWrappers := map[LogLevel]func(string, string, ...interface{}){
		LogLevelInfo:  LogInfo,
		LogLevelWarn:  LogWarn,
		LogLevelError: LogError,
		LogLevelDebug: LogDebug,
	}

	markdownWrappers := map[LogLevel]func(string, string, ...interface{}){
		LogLevelInfo:  LogInfoMd,
		LogLevelWarn:  LogWarnMd,
		LogLevelError: LogErrorMd,
		LogLevelDebug: LogDebugMd,
	}

	serverWrappers := map[LogLevel]func(string, string, string, ...interface{}){
		LogLevelInfo:  LogInfoWithServer,
		LogLevelWarn:  LogWarnWithServer,
		LogLevelError: LogErrorWithServer,
		LogLevelDebug: LogDebugWithServer,
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
