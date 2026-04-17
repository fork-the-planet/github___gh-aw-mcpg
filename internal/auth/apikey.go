package auth

import (
	"fmt"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/strutil"
)

var logAPIKey = logger.New("auth:apikey")

// GenerateRandomAPIKey generates a cryptographically random API key.
// Per spec §7.3, the gateway SHOULD generate a random API key on startup
// if none is provided. Returns a 32-byte hex-encoded string (64 chars).
func GenerateRandomAPIKey() (string, error) {
	logAPIKey.Print("Generating random API key")
	key, err := strutil.RandomHex(32)
	if err != nil {
		logAPIKey.Printf("Random API key generation failed: %v", err)
		return "", fmt.Errorf("failed to generate random API key: %w", err)
	}
	logAPIKey.Print("Random API key generated successfully")
	return key, nil
}
