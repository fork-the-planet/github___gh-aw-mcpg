// Package logger provides structured logging for the MCP Gateway.
//
// This file implements logging of MCP server tools to a JSON file (tools.json).
// It maintains a mapping of server IDs to their available tools with names and descriptions.
package logger

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// ToolInfo represents information about a single tool
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ToolsData represents the structure of tools.json
type ToolsData struct {
	// Map of serverID to array of tools
	Servers map[string][]ToolInfo `json:"servers"`
}

// ToolsLogger manages logging of MCP server tools to a JSON file
type ToolsLogger struct {
	lockable
	logDir      string
	fileName    string
	data        *ToolsData
	useFallback bool
}

var (
	globalToolsLogger *ToolsLogger
	globalToolsMu     sync.RWMutex
)

// setupToolsLogger configures a ToolsLogger after the log file has been opened.
// The file is closed immediately because ToolsLogger writes atomically on each update.
func setupToolsLogger(file *os.File, logDir, fileName string) (*ToolsLogger, error) {
	// Close the file immediately - we'll write directly later
	if file != nil {
		file.Close()
	}

	tl := &ToolsLogger{
		logDir:   logDir,
		fileName: fileName,
		data: &ToolsData{
			Servers: make(map[string][]ToolInfo),
		},
	}
	log.Printf("Tools logging to file: %s", filepath.Join(logDir, fileName))
	return tl, nil
}

// handleToolsLoggerError falls back to a no-op logger when the file cannot be opened.
func handleToolsLoggerError(err error, logDir, fileName string) (*ToolsLogger, error) {
	logFallbackWarnings(err, "Failed to initialize tools log file", "Tools logging disabled")
	tl := &ToolsLogger{
		logDir:      logDir,
		fileName:    fileName,
		useFallback: true,
		data: &ToolsData{
			Servers: make(map[string][]ToolInfo),
		},
	}
	return tl, nil
}

// toolsLoggerFactory bundles the setup and error-handler for ToolsLogger.
var toolsLoggerFactory = loggerFactory[*ToolsLogger]{
	setup:   setupToolsLogger,
	onError: handleToolsLoggerError,
}

// InitToolsLogger initializes the global tools logger
// If the log directory doesn't exist and can't be created, falls back to no-op
func InitToolsLogger(logDir, fileName string) error {
	logger, err := initLogger(logDir, fileName, os.O_TRUNC, toolsLoggerFactory)
	initGlobalLogger(&globalToolsMu, &globalToolsLogger, logger)
	return err
}

// LogTools logs the tools for a specific server
func (tl *ToolsLogger) LogTools(serverID string, tools []ToolInfo) error {
	return tl.withLock(func() error {
		if tl.useFallback {
			return nil // Silently skip if in fallback mode
		}

		// Update the data structure
		tl.data.Servers[serverID] = tools

		// Write the updated data to file
		return tl.writeToFile()
	})
}

// writeToFile writes the current tools data to the JSON file
// Caller must hold tl.mu lock
func (tl *ToolsLogger) writeToFile() error {
	filePath := filepath.Join(tl.logDir, tl.fileName)

	// Marshal to JSON with indentation for readability
	jsonData, err := json.MarshalIndent(tl.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tools data: %w", err)
	}

	return atomicWriteFile(filePath, jsonData, 0644)
}

// Close is a no-op for ToolsLogger (implements closableLogger interface)
func (tl *ToolsLogger) Close() error {
	// No file handle to close since we write directly each time
	return nil
}

// Global logging function that uses the global tools logger

// LogToolsForServer logs the tools for a specific server.
// It uses the withGlobalLogger helper from global_helpers.go to handle mutex locking and nil-checking.
func LogToolsForServer(serverID string, tools []ToolInfo) {
	withGlobalLogger(&globalToolsMu, &globalToolsLogger, func(logger *ToolsLogger) {
		if err := logger.LogTools(serverID, tools); err != nil {
			// Log errors using the standard logger to avoid recursion
			log.Printf("WARNING: Failed to log tools for server %s: %v", serverID, err)
		}
	})
}

// CloseToolsLogger closes the global tools logger
func CloseToolsLogger() error {
	return closeGlobalLogger(&globalToolsMu, &globalToolsLogger)
}
