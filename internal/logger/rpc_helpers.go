// Package logger provides structured logging for the MCP Gateway.
//
// This file contains helper functions for processing RPC message payloads.
//
// Functions in this file:
//
// - truncateAndSanitize: Combines secret sanitization with length truncation
// - extractEssentialFields: Extracts key JSON-RPC fields for compact logging
//
// These helpers are used by the RPC logging system to safely and efficiently
// process message payloads before logging them.
package logger

import (
	"encoding/json"

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

// extractEssentialFields extracts key fields from the payload for logging
func extractEssentialFields(payload []byte) map[string]interface{} {
	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil
	}

	// Extract only essential fields
	essential := make(map[string]interface{})

	// Common JSON-RPC fields
	if method, ok := data["method"].(string); ok {
		essential["method"] = method
	}
	if id, ok := data["id"]; ok {
		essential["id"] = id
	}
	if jsonrpc, ok := data["jsonrpc"].(string); ok {
		essential["jsonrpc"] = jsonrpc
	}

	// For responses, include error info
	if errData, ok := data["error"]; ok {
		essential["error"] = errData
	}

	// For requests, include params summary (but not full params)
	if params, ok := data["params"]; ok {
		if paramsMap, ok := params.(map[string]interface{}); ok {
			// Include param count and keys, but not values
			keys := make([]string, 0, len(paramsMap))
			for k := range paramsMap {
				keys = append(keys, k)
			}
			essential["params_keys"] = keys
		}
	}

	return essential
}
