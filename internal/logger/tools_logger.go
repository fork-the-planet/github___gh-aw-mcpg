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
	logDir      string
	fileName    string
	data        *ToolsData
	mu          sync.Mutex
	useFallback bool
}

var (
	globalToolsLogger *ToolsLogger
	globalToolsMu     sync.RWMutex
)

// InitToolsLogger initializes the global tools logger
// If the log directory doesn't exist and can't be created, falls back to no-op
func InitToolsLogger(logDir, fileName string) error {
	logger, err := initLogger(
		logDir, fileName, os.O_TRUNC, // Truncate existing file to start fresh
		// Setup function: configure the logger after directory is ready
		func(file *os.File, logDir, fileName string) (*ToolsLogger, error) {
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
		},
		// Error handler: fallback to no-op on error
		func(err error, logDir, fileName string) (*ToolsLogger, error) {
			log.Printf("WARNING: Failed to initialize tools log file: %v", err)
			log.Printf("WARNING: Tools logging disabled")
			tl := &ToolsLogger{
				logDir:      logDir,
				fileName:    fileName,
				useFallback: true,
				data: &ToolsData{
					Servers: make(map[string][]ToolInfo),
				},
			}
			return tl, nil
		},
	)

	initGlobalToolsLogger(logger)
	return err
}

// LogTools logs the tools for a specific server
func (tl *ToolsLogger) LogTools(serverID string, tools []ToolInfo) error {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	if tl.useFallback {
		return nil // Silently skip if in fallback mode
	}

	// Update the data structure
	tl.data.Servers[serverID] = tools

	// Write the updated data to file
	return tl.writeToFile()
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

	// Write to file atomically using a temp file + rename
	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		// Clean up temp file on error
		os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// Close is a no-op for ToolsLogger (implements closableLogger interface)
func (tl *ToolsLogger) Close() error {
	// No file handle to close since we write directly each time
	return nil
}

// Global logging function that uses the global tools logger

// LogToolsForServer logs the tools for a specific server
func LogToolsForServer(serverID string, tools []ToolInfo) {
	globalToolsMu.RLock()
	defer globalToolsMu.RUnlock()

	if globalToolsLogger != nil {
		if err := globalToolsLogger.LogTools(serverID, tools); err != nil {
			// Log errors using the standard logger to avoid recursion
			log.Printf("WARNING: Failed to log tools for server %s: %v", serverID, err)
		}
	}
}

// CloseToolsLogger closes the global tools logger
func CloseToolsLogger() error {
	return closeGlobalToolsLogger()
}

// initGlobalToolsLogger initializes the global ToolsLogger using the generic helper.
func initGlobalToolsLogger(logger *ToolsLogger) {
	initGlobalLogger(&globalToolsMu, &globalToolsLogger, logger)
}

// closeGlobalToolsLogger closes the global ToolsLogger using the generic helper.
func closeGlobalToolsLogger() error {
	return closeGlobalLogger(&globalToolsMu, &globalToolsLogger)
}
