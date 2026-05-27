package logger

import (
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleFileLoggerError_UsesFallback(t *testing.T) {
	logger, err := handleFileLoggerError(assert.AnError, "/tmp/logs", "test.log")

	require.NoError(t, err)
	require.NotNil(t, logger)
	assert.True(t, logger.useFallback)
	assert.Nil(t, logger.logFile)
	require.NotNil(t, logger.logger)
	assert.Equal(t, log.New(os.Stderr, "", 0).Writer(), logger.logger.Writer())
}

func TestHandleMarkdownLoggerError_UsesFallback(t *testing.T) {
	logger, err := handleMarkdownLoggerError(assert.AnError, "/tmp/logs", "test.md")

	require.NoError(t, err)
	require.NotNil(t, logger)
	assert.True(t, logger.useFallback)
	assert.Nil(t, logger.logFile)
	assert.False(t, logger.initialized)
}

func TestHandleToolsLoggerError_UsesFallback(t *testing.T) {
	logger, err := handleToolsLoggerError(assert.AnError, "/tmp/logs", "tools.json")

	require.NoError(t, err)
	require.NotNil(t, logger)
	assert.True(t, logger.useFallback)
	require.NotNil(t, logger.data)
	assert.NotNil(t, logger.data.Servers)
	assert.Empty(t, logger.data.Servers)
}
