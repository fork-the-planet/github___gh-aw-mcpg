// Package logger provides structured logging for the MCP Gateway.
//
// This file contains helper functions for processing RPC message payloads.
//
// Functions in this file:
//
// - truncateAndSanitize: Combines secret sanitization with length truncation
//
// These helpers are used by the RPC logging system to safely and efficiently
// process message payloads before logging them.
package logger

import (
	"github.com/github/gh-aw-mcpg/internal/logger/sanitize"
	"github.com/github/gh-aw-mcpg/internal/strutil"
)

// truncateAndSanitize truncates the payload to max length and sanitizes secrets
func truncateAndSanitize(payload string, maxLength int) string {
	// First sanitize secrets
	sanitized := sanitize.SanitizeString(payload)

	// Then truncate if needed
	return strutil.Truncate(sanitized, maxLength)
}
