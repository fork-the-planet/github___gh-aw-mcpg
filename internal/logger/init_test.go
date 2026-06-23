package logger

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggerRegistries(t *testing.T) {
	t.Run("gateway logger registry includes expected entries", func(t *testing.T) {
		names := make([]string, 0, len(gatewayLoggerInitializers))
		for _, entry := range gatewayLoggerInitializers {
			names = append(names, entry.name)
		}

		assert.Equal(t, []string{
			"file logger",
			"server file logger",
			"markdown logger",
			"JSONL logger",
			"tools logger",
			"observed URL domains logger",
		}, names)
	})

	t.Run("proxy logger registry includes expected entries", func(t *testing.T) {
		names := make([]string, 0, len(proxyLoggerInitializers))
		for _, entry := range proxyLoggerInitializers {
			names = append(names, entry.name)
		}

		assert.Equal(t, []string{
			"file logger",
			"markdown logger",
			"JSONL logger",
		}, names)
	})

	t.Run("global logger closer registry includes expected entries", func(t *testing.T) {
		names := make([]string, 0, len(globalLoggerClosers))
		for _, entry := range globalLoggerClosers {
			names = append(names, entry.name)
		}

		assert.Equal(t, []string{
			"file logger",
			"JSONL logger",
			"markdown logger",
			"tools logger",
			"server file logger",
			"observed URL domains logger",
		}, names)
	})
}

// TestInitWithWarning tests the initWithWarning helper.
func TestInitWithWarning(t *testing.T) {
	t.Run("nil error does not log anything", func(t *testing.T) {
		// Capture log output
		var buf bytes.Buffer
		log.SetOutput(&buf)
		t.Cleanup(func() { log.SetOutput(os.Stderr) })

		initWithWarning(nil, "test component")

		assert.Empty(t, buf.String(), "No warning should be logged for nil error")
	})

	t.Run("non-nil error logs a warning with component name", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)
		t.Cleanup(func() { log.SetOutput(os.Stderr) })

		err := assert.AnError
		initWithWarning(err, "my-component")

		output := buf.String()
		assert.Contains(t, output, "Warning", "Warning message should be logged")
		assert.Contains(t, output, "my-component", "Component name should appear in warning")
	})

	t.Run("error message is included in warning", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)
		t.Cleanup(func() { log.SetOutput(os.Stderr) })

		err := assert.AnError
		initWithWarning(err, "file logger")

		output := buf.String()
		assert.Contains(t, output, err.Error(), "Error message should appear in warning")
	})
}

// TestInitGatewayLoggers tests that InitGatewayLoggers creates all required log files.
func TestInitGatewayLoggers(t *testing.T) {
	t.Run("valid log directory creates all log files", func(t *testing.T) {
		tmpDir := t.TempDir()
		logDir := filepath.Join(tmpDir, "gateway-logs")

		// Should not panic, failures are logged as warnings
		InitGatewayLoggers(logDir)

		// Verify that the log directory was created
		_, err := os.Stat(logDir)
		require.NoError(t, err, "Log directory should have been created")

		// Verify that expected log files were created
		expectedFiles := []string{
			"mcp-gateway.log",
			"gateway.md",
			"rpc-messages.jsonl",
			"tools.json",
			"observed-url-domains.json",
		}
		for _, f := range expectedFiles {
			path := filepath.Join(logDir, f)
			_, statErr := os.Stat(path)
			assert.NoError(t, statErr, "Expected log file to exist: %s", f)
		}
	})

	t.Run("invalid log directory logs warnings but does not panic", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)
		t.Cleanup(func() { log.SetOutput(os.Stderr) })

		// A path that cannot be written to (no permissions)
		logDir := "/proc/nonexistent-directory-that-cannot-exist"

		// Should not panic even if directory creation fails
		assert.NotPanics(t, func() {
			InitGatewayLoggers(logDir)
		}, "InitGatewayLoggers should not panic on bad directory")
	})
}

// TestInitProxyLoggers tests that InitProxyLoggers creates the proxy log files.
func TestInitProxyLoggers(t *testing.T) {
	t.Run("valid log directory creates proxy log files", func(t *testing.T) {
		tmpDir := t.TempDir()
		logDir := filepath.Join(tmpDir, "proxy-logs")

		// Should not panic, failures are logged as warnings
		InitProxyLoggers(logDir)

		// Verify that the log directory was created
		_, err := os.Stat(logDir)
		require.NoError(t, err, "Log directory should have been created")

		// Verify that expected proxy log files were created
		expectedFiles := []string{
			"proxy.log",
			"gateway.md",
			"rpc-messages.jsonl",
		}
		for _, f := range expectedFiles {
			path := filepath.Join(logDir, f)
			_, statErr := os.Stat(path)
			assert.NoError(t, statErr, "Expected proxy log file to exist: %s", f)
		}
	})

	t.Run("invalid log directory logs warnings but does not panic", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)
		t.Cleanup(func() { log.SetOutput(os.Stderr) })

		logDir := "/proc/nonexistent-directory-that-cannot-exist"

		assert.NotPanics(t, func() {
			InitProxyLoggers(logDir)
		}, "InitProxyLoggers should not panic on bad directory")
	})

	t.Run("proxy loggers do not create tools log", func(t *testing.T) {
		tmpDir := t.TempDir()
		logDir := filepath.Join(tmpDir, "proxy-only-logs")

		InitProxyLoggers(logDir)

		// tools.json remains gateway-only.
		toolsPath := filepath.Join(logDir, "tools.json")
		_, err := os.Stat(toolsPath)
		assert.Error(t, err, "tools.json should NOT be created by InitProxyLoggers")
	})
}
