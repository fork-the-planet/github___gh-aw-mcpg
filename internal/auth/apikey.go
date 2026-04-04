package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateRandomAPIKey generates a cryptographically random API key.
// Per spec §7.3, the gateway SHOULD generate a random API key on startup
// if none is provided. Returns a 32-byte hex-encoded string (64 chars).
func GenerateRandomAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random API key: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
