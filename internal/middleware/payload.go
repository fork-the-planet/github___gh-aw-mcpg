package middleware

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

// savePayload saves the payload to disk and returns the file path
// The file is saved to {baseDir}/{sessionID}/{queryID}/payload.json
// The returned path uses pathPrefix if provided, otherwise returns the actual filesystem path
func savePayload(baseDir, pathPrefix, sessionID, queryID string, payload []byte) (string, error) {
	// Create directory structure: {baseDir}/{sessionID}/{queryID}
	dir := filepath.Join(baseDir, sessionID, queryID)

	logger.LogDebug("payload", "Creating payload directory: baseDir=%s, session=%s, query=%s, fullPath=%s",
		baseDir, sessionID, queryID, dir)

	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.LogError("payload", "Failed to create payload directory: path=%s, error=%v", dir, err)
		return "", fmt.Errorf("failed to create payload directory: %w", err)
	}

	logger.LogDebug("payload", "Successfully created payload directory: path=%s, permissions=0755", dir)

	// Save payload to file with restrictive permissions (owner read/write only)
	filePath := filepath.Join(dir, "payload.json")
	payloadSize := len(payload)

	logger.LogInfo("payload", "Writing large payload to filesystem: path=%s, size=%d bytes (%.2f KB, %.2f MB)",
		filePath, payloadSize, float64(payloadSize)/1024, float64(payloadSize)/(1024*1024))

	if err := os.WriteFile(filePath, payload, 0600); err != nil {
		logger.LogError("payload", "Failed to write payload file: path=%s, size=%d bytes, error=%v",
			filePath, payloadSize, err)
		return "", fmt.Errorf("failed to write payload file: %w", err)
	}

	// Enforce permissions even if the file already existed (WriteFile only sets mode on create)
	if err := os.Chmod(filePath, 0600); err != nil {
		logger.LogError("payload", "Failed to enforce payload file permissions: path=%s, size=%d bytes, error=%v",
			filePath, payloadSize, err)
		return "", fmt.Errorf("failed to set payload file permissions: %w", err)
	}

	// Log with the requested chmod mode rather than re-stating as a guarantee (Chmod may be
	// a no-op on some platforms/filesystems while still returning nil).
	logger.LogInfo("payload", "Successfully saved large payload to filesystem: path=%s, size=%d bytes, chmod=0600",
		filePath, payloadSize)

	// If pathPrefix is provided, use it to remap the path for the client
	// This allows the gateway to save files at one path (e.g., /tmp/jq-payloads)
	// while returning a different path to clients (e.g., /workspace/payloads)
	returnPath := filePath
	if pathPrefix != "" {
		// Replace baseDir with pathPrefix in the file path
		relPath := filepath.Join(sessionID, queryID, "payload.json")
		returnPath = filepath.Join(pathPrefix, relPath)
		logger.LogInfo("payload", "Remapped payload path for client: filesystem=%s, clientPath=%s, pathPrefix=%s",
			filePath, returnPath, pathPrefix)
	}

	return returnPath, nil
}
