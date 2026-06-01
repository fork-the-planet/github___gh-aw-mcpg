package logger

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleFileLoggerError_UsesFallback(t *testing.T) {
	var logger *FileLogger
	var err error
	_ = captureStdLog(t, func() {
		logger, err = handleFileLoggerError(assert.AnError, "/tmp/logs", "test.log")
	})
	require.NoError(t, err)
	require.NotNil(t, logger)
	assert.True(t, logger.useFallback)
	assert.Nil(t, logger.logFile)
	require.NotNil(t, logger.logger)
	assert.Equal(t, os.Stderr, logger.logger.Writer())
}

func TestHandleMarkdownLoggerError_UsesFallback(t *testing.T) {
	var logger *MarkdownLogger
	var err error
	_ = captureStdLog(t, func() {
		logger, err = handleMarkdownLoggerError(assert.AnError, "/tmp/logs", "test.md")
	})
	require.NoError(t, err)
	require.NotNil(t, logger)
	assert.True(t, logger.useFallback)
	assert.Nil(t, logger.logFile)
	assert.False(t, logger.initialized)
}

func TestHandleToolsLoggerError_UsesFallback(t *testing.T) {
	var logger *ToolsLogger
	var err error
	_ = captureStdLog(t, func() {
		logger, err = handleToolsLoggerError(assert.AnError, "/tmp/logs", "tools.json")
	})
	require.NoError(t, err)
	require.NotNil(t, logger)
	assert.True(t, logger.useFallback)
	require.NotNil(t, logger.data)
	assert.Empty(t, logger.data.Servers)
}
