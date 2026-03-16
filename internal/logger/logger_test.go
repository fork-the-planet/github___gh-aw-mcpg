package logger

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStderr captures stderr output during test execution
func captureStderr(f func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	f()

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func TestNew(t *testing.T) {
	tests := []struct {
		name      string
		debugEnv  string
		namespace string
		enabled   bool
	}{
		{
			name:      "empty DEBUG disables all loggers",
			debugEnv:  "",
			namespace: "test:logger",
			enabled:   false,
		},
		{
			name:      "wildcard enables all loggers",
			debugEnv:  "*",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "exact match enables logger",
			debugEnv:  "test:logger",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "exact match different namespace disabled",
			debugEnv:  "test:logger",
			namespace: "other:logger",
			enabled:   false,
		},
		{
			name:      "namespace wildcard enables matching loggers",
			debugEnv:  "test:*",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "namespace wildcard matches deeply nested",
			debugEnv:  "test:*",
			namespace: "test:sub:logger",
			enabled:   true,
		},
		{
			name:      "namespace wildcard does not match different prefix",
			debugEnv:  "test:*",
			namespace: "other:logger",
			enabled:   false,
		},
		{
			name:      "multiple patterns with comma",
			debugEnv:  "test:*,other:*",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "multiple patterns second matches",
			debugEnv:  "test:*,other:*",
			namespace: "other:logger",
			enabled:   true,
		},
		{
			name:      "exclusion pattern disables specific logger",
			debugEnv:  "test:*,-test:skip",
			namespace: "test:skip",
			enabled:   false,
		},
		{
			name:      "exclusion does not affect other loggers",
			debugEnv:  "test:*,-test:skip",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "exclusion with wildcard",
			debugEnv:  "*,-test:*",
			namespace: "test:logger",
			enabled:   false,
		},
		{
			name:      "exclusion with wildcard allows others",
			debugEnv:  "*,-test:*",
			namespace: "other:logger",
			enabled:   true,
		},
		{
			name:      "suffix wildcard",
			debugEnv:  "*:logger",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "suffix wildcard no match",
			debugEnv:  "*:logger",
			namespace: "test:other",
			enabled:   false,
		},
		{
			name:      "middle wildcard",
			debugEnv:  "test:*:end",
			namespace: "test:middle:end",
			enabled:   true,
		},
		{
			name:      "middle wildcard no match prefix",
			debugEnv:  "test:*:end",
			namespace: "other:middle:end",
			enabled:   false,
		},
		{
			name:      "middle wildcard no match suffix",
			debugEnv:  "test:*:end",
			namespace: "test:middle:other",
			enabled:   false,
		},
		{
			name:      "spaces in patterns are trimmed",
			debugEnv:  "test:* , other:*",
			namespace: "other:logger",
			enabled:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use t.Setenv to set environment variable for this test
			t.Setenv("DEBUG", tt.debugEnv)

			logger := New(tt.namespace)
			assert.Equal(t, tt.enabled, logger.Enabled(),
				"New(%q) with DEBUG=%q: wrong enabled state", tt.namespace, tt.debugEnv)
		})
	}
}

func TestLogger_Printf(t *testing.T) {
	tests := []struct {
		name      string
		debugEnv  string
		namespace string
		format    string
		args      []any
		wantLog   bool
	}{
		{
			name:      "enabled logger prints",
			debugEnv:  "*",
			namespace: "test:logger",
			format:    "hello %s",
			args:      []any{"world"},
			wantLog:   true,
		},
		{
			name:      "disabled logger does not print",
			debugEnv:  "",
			namespace: "test:logger",
			format:    "hello %s",
			args:      []any{"world"},
			wantLog:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use t.Setenv to set environment variable for this test
			t.Setenv("DEBUG", tt.debugEnv)

			logger := New(tt.namespace)

			output := captureStderr(func() {
				logger.Printf(tt.format, tt.args...)
			})

			if tt.wantLog {
				assert.NotEmpty(t, output, "Printf() should have logged but got empty output")
				assert.Contains(t, output, tt.namespace,
					"Printf() output should contain namespace %q", tt.namespace)
				assert.Contains(t, output, "hello world",
					"Printf() output should contain message")
			} else {
				assert.Empty(t, output, "Printf() should not have logged but got output")
			}
		})
	}
}

func TestLogger_Print(t *testing.T) {
	// Use t.Setenv to set environment variable for this test
	t.Setenv("DEBUG", "*")

	logger := New("test:print")

	output := captureStderr(func() {
		logger.Print("hello", " ", "world")
	})

	assert.Contains(t, output, "test:print", "Print() output should contain namespace")
	assert.Contains(t, output, "hello world", "Print() output should contain message")
	// Check that time diff is included
	assert.Contains(t, output, "+", "Print() output should contain time diff")
}

func TestLogger_TimeDiff(t *testing.T) {
	// Use t.Setenv to set environment variable for this test
	t.Setenv("DEBUG", "*")

	logger := New("test:timediff")

	// First log
	output1 := captureStderr(func() {
		logger.Printf("first message")
	})

	// Small delay
	time.Sleep(10 * time.Millisecond)

	// Second log
	output2 := captureStderr(func() {
		logger.Printf("second message")
	})

	// Both should have time diff
	assert.Contains(t, output1, "+", "First log should contain time diff")
	assert.Contains(t, output2, "+", "Second log should contain time diff")

	// Second log should show time diff with a time unit
	assert.Regexp(t, `\+\d+(\.\d+)?(ns|µs|ms|s|m|h)`, output2,
		"Second log should show time diff with unit")
}

func TestColorSelection(t *testing.T) {
	// Test that selectColor returns consistent colors for the same namespace
	color1 := selectColor("test:namespace")
	color2 := selectColor("test:namespace")
	assert.Equal(t, color1, color2, "selectColor should return same color for same namespace")

	// Test that different namespaces can get different colors
	// (not guaranteed but likely with our hash function)
	color3 := selectColor("other:namespace")
	// Verify it's a valid color from palette or empty
	isValidColor := color3 == ""
	for _, c := range colorPalette {
		if color3 == c {
			isValidColor = true
			break
		}
	}
	assert.True(t, isValidColor, "selectColor returned invalid color: %q", color3)
}

func TestColorDisabling(t *testing.T) {
	// Save original values
	origDebugColors := debugColors
	origIsTTY := isTTY
	defer func() {
		debugColors = origDebugColors
		isTTY = origIsTTY
	}()

	// Test with colors disabled via DEBUG_COLORS
	debugColors = false
	isTTY = true
	color := selectColor("test:namespace")
	assert.Empty(t, color, "selectColor should return empty when debugColors=false")

	// Test with TTY disabled
	debugColors = true
	isTTY = false
	color = selectColor("test:namespace")
	assert.Empty(t, color, "selectColor should return empty when isTTY=false")

	// Test with both enabled
	debugColors = true
	isTTY = true
	color = selectColor("test:namespace")
	assert.NotEmpty(t, color, "selectColor should return color when both enabled")
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		pattern   string
		want      bool
	}{
		{"exact match", "test:logger", "test:logger", true},
		{"no match", "test:logger", "other:logger", false},
		{"wildcard all", "test:logger", "*", true},
		{"prefix wildcard", "test:logger", "test:*", true},
		{"prefix wildcard no match", "test:logger", "other:*", false},
		{"suffix wildcard", "test:logger", "*:logger", true},
		{"suffix wildcard no match", "test:logger", "*:other", false},
		{"middle wildcard", "test:middle:logger", "test:*:logger", true},
		{"middle wildcard no match prefix", "other:middle:logger", "test:*:logger", false},
		{"middle wildcard no match suffix", "test:middle:other", "test:*:logger", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPattern(tt.namespace, tt.pattern)
			assert.Equal(t, tt.want, got, "matchPattern(%q, %q)", tt.namespace, tt.pattern)
		})
	}
}

func TestComputeEnabled(t *testing.T) {
	tests := []struct {
		name      string
		debugEnv  string
		namespace string
		want      bool
	}{
		{"single pattern match", "test:*", "test:logger", true},
		{"single pattern no match", "test:*", "other:logger", false},
		{"multiple patterns first match", "test:*,other:*", "test:logger", true},
		{"multiple patterns second match", "test:*,other:*", "other:logger", true},
		{"multiple patterns no match", "test:*,other:*", "third:logger", false},
		{"exclusion disables", "test:*,-test:skip", "test:skip", false},
		{"exclusion allows others", "test:*,-test:skip", "test:logger", true},
		{"exclusion wildcard", "*,-test:*", "test:logger", false},
		{"exclusion wildcard allows", "*,-test:*", "other:logger", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use t.Setenv to set DEBUG for this test
			t.Setenv("DEBUG", tt.debugEnv)
			got := computeEnabled(tt.namespace)
			assert.Equal(t, tt.want, got,
				"computeEnabled(%q) with DEBUG=%q", tt.namespace, tt.debugEnv)
		})
	}
}

func TestDebugLoggerWritesToFile(t *testing.T) {
	// Create a temporary directory for the file logger
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")
	fileName := "debug-test.log"

	// Initialize the file logger
	err := InitFileLogger(logDir, fileName)
	require.NoError(t, err, "InitFileLogger failed")
	defer CloseGlobalLogger()

	// Use t.Setenv to enable all debug loggers
	t.Setenv("DEBUG", "*")

	// Create a debug logger
	log := New("test:debug")

	// Capture stderr to verify stderr output
	stderrOutput := captureStderr(func() {
		log.Printf("Test message %d", 42)
		log.Print("Another test message")
	})

	// Verify stderr output contains the messages
	assert.Contains(t, stderrOutput, "Test message 42", "Stderr should contain debug message")
	assert.Contains(t, stderrOutput, "Another test message", "Stderr should contain debug message")

	// Close the file logger to flush all data
	CloseGlobalLogger()

	// Read the log file
	logPath := filepath.Join(logDir, fileName)
	content, err := os.ReadFile(logPath)
	require.NoError(t, err, "Failed to read log file")

	logContent := string(content)

	// Verify the file logger contains the same messages (text-only, no colors)
	assert.Contains(t, logContent, "Test message 42", "Log file should contain debug message")
	assert.Contains(t, logContent, "Another test message", "Log file should contain debug message")

	// Verify the file logger has DEBUG level
	assert.Contains(t, logContent, "[DEBUG]", "Log file should contain [DEBUG] level")

	// Verify the file logger has the namespace as category
	assert.Contains(t, logContent, "[test:debug]", "Log file should contain [test:debug] category")

	// Verify no color codes in file output
	assert.NotContains(t, logContent, "\033[", "Log file should not contain ANSI color codes")
}

func TestDebugLoggerDisabledNoFileWrite(t *testing.T) {
	// Create a temporary directory for the file logger
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")
	fileName := "debug-disabled-test.log"

	// Initialize the file logger
	err := InitFileLogger(logDir, fileName)
	require.NoError(t, err, "InitFileLogger failed")
	defer CloseGlobalLogger()

	// Use t.Setenv to disable all debug loggers
	t.Setenv("DEBUG", "")

	// Create a debug logger (should be disabled)
	log := New("test:disabled")

	// Verify logger is disabled
	require.False(t, log.Enabled(), "Logger should be disabled when DEBUG is empty")

	// Try to log (should not write anywhere)
	log.Printf("This should not appear")

	// Close the file logger to flush all data
	CloseGlobalLogger()

	// Read the log file
	logPath := filepath.Join(logDir, fileName)
	content, err := os.ReadFile(logPath)
	require.NoError(t, err, "Failed to read log file")

	logContent := string(content)

	// Verify the message is NOT in the file (logger was disabled)
	assert.NotContains(t, logContent, "This should not appear",
		"Disabled logger should not write to file")
}

// TestNew_WithDebugEnv tests logger creation with various DEBUG environment patterns
func TestNew_WithDebugEnv(t *testing.T) {
	tests := []struct {
		name      string
		debugEnv  string
		namespace string
		enabled   bool
	}{
		{
			name:      "wildcard enables all loggers",
			debugEnv:  "*",
			namespace: "app:feature",
			enabled:   true,
		},
		{
			name:      "exact match enables logger",
			debugEnv:  "app:feature",
			namespace: "app:feature",
			enabled:   true,
		},
		{
			name:      "namespace wildcard enables matching loggers",
			debugEnv:  "app:*",
			namespace: "app:feature",
			enabled:   true,
		},
		{
			name:      "namespace wildcard does not match different prefix",
			debugEnv:  "app:*",
			namespace: "other:feature",
			enabled:   false,
		},
		{
			name:      "multiple patterns with comma",
			debugEnv:  "app:*,other:*",
			namespace: "app:feature",
			enabled:   true,
		},
		{
			name:      "exclusion pattern disables specific logger",
			debugEnv:  "app:*,-app:skip",
			namespace: "app:skip",
			enabled:   false,
		},
		{
			name:      "exclusion does not affect other loggers",
			debugEnv:  "app:*,-app:skip",
			namespace: "app:feature",
			enabled:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use t.Setenv to set environment variable for this test
			t.Setenv("DEBUG", tt.debugEnv)

			log := New(tt.namespace)
			assert.Equal(t, tt.enabled, log.Enabled(),
				"New(%q) with DEBUG=%q: wrong enabled state", tt.namespace, tt.debugEnv)
		})
	}
}

// TestLogger_Printf_WithDebug tests Printf functionality with DEBUG enabled
func TestLogger_Printf_WithDebug(t *testing.T) {
	// Set DEBUG to enable all loggers
	t.Setenv("DEBUG", "*")

	log := New("test:feature")
	assert.True(t, log.Enabled(), "Logger should be enabled with DEBUG=*")

	// Note: Printf writes to stderr, so we can't easily capture the output
	// in an example test. This test just verifies it doesn't panic.
	log.Printf("Processing %d items", 42)
}

// TestLogger_Print_WithDebug tests Print functionality with DEBUG enabled
func TestLogger_Print_WithDebug(t *testing.T) {
	// Set DEBUG to enable all loggers
	t.Setenv("DEBUG", "*")

	log := New("test:feature")
	assert.True(t, log.Enabled(), "Logger should be enabled with DEBUG=*")

	// Note: Print writes to stderr, so we can't easily capture the output
	// in an example test. This test just verifies it doesn't panic.
	log.Print("Processing", " ", "items")
}

// TestDebugPatterns tests various DEBUG pattern matching scenarios
func TestDebugPatterns(t *testing.T) {
	tests := []struct {
		name      string
		debugEnv  string
		namespace string
		enabled   bool
	}{
		{
			name:      "empty DEBUG disables all loggers",
			debugEnv:  "",
			namespace: "test:logger",
			enabled:   false,
		},
		{
			name:      "wildcard-all pattern",
			debugEnv:  "*",
			namespace: "any:namespace",
			enabled:   true,
		},
		{
			name:      "suffix wildcard",
			debugEnv:  "*:logger",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "suffix wildcard no match",
			debugEnv:  "*:logger",
			namespace: "test:other",
			enabled:   false,
		},
		{
			name:      "middle wildcard",
			debugEnv:  "test:*:end",
			namespace: "test:middle:end",
			enabled:   true,
		},
		{
			name:      "exclusion with wildcard",
			debugEnv:  "*,-test:*",
			namespace: "test:logger",
			enabled:   false,
		},
		{
			name:      "exclusion with wildcard allows others",
			debugEnv:  "*,-test:*",
			namespace: "other:logger",
			enabled:   true,
		},
		{
			name:      "spaces in patterns are trimmed",
			debugEnv:  "test:* , other:*",
			namespace: "other:logger",
			enabled:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DEBUG", tt.debugEnv)

			log := New(tt.namespace)
			assert.Equal(t, tt.enabled, log.Enabled(),
				"New(%q) with DEBUG=%q: wrong enabled state", tt.namespace, tt.debugEnv)
		})
	}
}

// TestLogger_Printf_WithColors verifies that Printf includes color codes when colors are enabled.
func TestLogger_Printf_WithColors(t *testing.T) {
	origDebugColors := debugColors
	origIsTTY := isTTY
	t.Cleanup(func() {
		debugColors = origDebugColors
		isTTY = origIsTTY
	})

	debugColors = true
	isTTY = true

	t.Setenv("DEBUG", "*")
	log := New("test:colors")
	require.NotEmpty(t, log.color, "Logger should have a color assigned when TTY and colors are enabled")

	output := captureStderr(func() {
		log.Printf("colored message")
	})

	assert.Contains(t, output, "colored message", "Printf() output should contain the message")
	assert.Contains(t, output, "\033[", "Printf() output should contain ANSI color codes when colors enabled")
	assert.Contains(t, output, colorReset, "Printf() output should contain color reset code")
}

// TestLogger_Print_WithColors verifies that Print includes color codes when colors are enabled.
func TestLogger_Print_WithColors(t *testing.T) {
	origDebugColors := debugColors
	origIsTTY := isTTY
	t.Cleanup(func() {
		debugColors = origDebugColors
		isTTY = origIsTTY
	})

	debugColors = true
	isTTY = true

	t.Setenv("DEBUG", "*")
	log := New("test:colors-print")
	require.NotEmpty(t, log.color, "Logger should have a color assigned when TTY and colors are enabled")

	output := captureStderr(func() {
		log.Print("colored", " ", "print")
	})

	assert.Contains(t, output, "colored print", "Print() output should contain the message")
	assert.Contains(t, output, "\033[", "Print() output should contain ANSI color codes when colors enabled")
	assert.Contains(t, output, colorReset, "Print() output should contain color reset code")
}
