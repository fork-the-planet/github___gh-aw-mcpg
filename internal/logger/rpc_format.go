// Package logger provides structured logging for the MCP Gateway.
//
// This file contains formatting and payload helper functions for RPC message logs.
//
// Text Format: Compact, single-line format optimized for grep and command-line tools
//
//	Example: "github→tools/list 1234b {...}"
//
// Markdown Format: Human-readable with syntax highlighting, suitable for documentation
//
//	Example: "**github**→`tools/list`\n\n```json\n{...}\n```"
//
// Both formats use directional arrows (→ for outbound, ← for inbound) and support
// special handling for tools/call methods by extracting and displaying tool names.
package logger

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/sanitize"
	"github.com/github/gh-aw-mcpg/internal/util"
)

// truncateAndSanitize truncates the payload to max length and sanitizes secrets.
func truncateAndSanitize(payload string, maxLength int) string {
	sanitized := sanitize.SanitizeString(payload)
	return util.Truncate(sanitized, maxLength)
}

// truncateSanitized truncates an already-sanitized payload string without running the
// regex sanitization pass again. Use this when the same payload is previewed at
// multiple lengths (e.g. text vs. markdown) so that sanitization is paid only once.
func truncateSanitized(sanitized string, maxLength int) string {
	return util.Truncate(sanitized, maxLength)
}

// LogMarshaledForDebug marshals value for debug logging and dispatches to the
// provided callbacks for success or marshal failure paths.
func LogMarshaledForDebug(value interface{}, onMarshalSuccess func(string), onMarshalFailure func(error)) {
	resultJSON, err := json.Marshal(value)
	if err != nil {
		onMarshalFailure(err)
		return
	}
	onMarshalSuccess(string(resultJSON))
}

// LogMarshaledForDebugf marshals value for debug logging and dispatches to
// formatted logging functions for success or marshal failure paths.
func LogMarshaledForDebugf(
	value interface{},
	onMarshalSuccessf func(string, ...interface{}),
	successFormat string,
	onMarshalFailuref func(string, ...interface{}),
	failureFormat string,
	args ...interface{},
) {
	formatArgs := func(extra interface{}) []interface{} {
		formattedArgs := make([]interface{}, len(args)+1)
		copy(formattedArgs, args)
		formattedArgs[len(args)] = extra
		return formattedArgs
	}

	LogMarshaledForDebug(
		value,
		func(resultJSON string) {
			onMarshalSuccessf(successFormat, formatArgs(resultJSON)...)
		},
		func(marshalErr error) {
			onMarshalFailuref(failureFormat, formatArgs(marshalErr)...)
		},
	)
}

// formatRPCMessage formats an RPC message for logging
func formatRPCMessage(info *RPCMessageInfo) string {
	// Short format: server→method (or server←resp) size payload
	dir := "←"
	if info.Direction == RPCDirectionOutbound {
		dir = "→"
	}

	var sb strings.Builder

	// Server and direction
	if info.ServerID != "" {
		sb.WriteString(info.ServerID)
		sb.WriteString(dir)
		if info.Method != "" {
			sb.WriteString(info.Method)
		} else {
			sb.WriteString("resp")
		}
	}

	// Size
	if sb.Len() > 0 {
		sb.WriteByte(' ')
	}
	sb.WriteString(strconv.Itoa(info.PayloadSize))
	sb.WriteByte('b')

	// Error (if present)
	if info.Error != "" {
		sb.WriteString(" err:")
		sb.WriteString(info.Error)
	}

	// Payload preview (if present)
	if info.Payload != "" {
		sb.WriteByte(' ')
		sb.WriteString(info.Payload)
	}

	return sb.String()
}

// isEffectivelyEmpty checks if the data is effectively empty (only contains params: null)
func isEffectivelyEmpty(data map[string]interface{}) bool {
	// If empty, it's empty
	if len(data) == 0 {
		return true
	}

	// If only one field and it's "params" with null value, it's empty
	if len(data) == 1 {
		if params, ok := data["params"]; ok && params == nil {
			return true
		}
	}

	return false
}

// formatJSONWithoutFields formats JSON by removing specified fields and compacting to single line
// Returns the formatted string, a boolean indicating if the JSON was valid, and a boolean indicating if empty
func formatJSONWithoutFields(jsonStr string, fieldsToRemove []string) (string, bool, bool) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		// If not valid JSON, return as-is with false
		return jsonStr, false, false
	}

	// Remove specified fields
	for _, field := range fieldsToRemove {
		delete(data, field)
	}

	// Check if only "params": null remains (or equivalent empty state)
	isEmpty := isEffectivelyEmpty(data)

	// Re-marshal as compact single line
	formatted, err := json.Marshal(data)
	if err != nil {
		return jsonStr, false, false
	}

	return string(formatted), true, isEmpty
}

// formatRPCMessageMarkdown formats an RPC message for markdown logging
func formatRPCMessageMarkdown(info *RPCMessageInfo) string {
	// Concise format: **server**→method \n```json \n{formatted json} \n```
	var dir string
	if info.Direction == RPCDirectionOutbound {
		dir = "→"
	} else {
		dir = "←"
	}

	var message string

	// Server, direction, and method/type
	if info.ServerID != "" {
		if info.Method != "" {
			message = fmt.Sprintf("**%s**%s`%s`", info.ServerID, dir, info.Method)

			// For tools/call, extract and display the tool name
			if info.Method == "tools/call" && info.Payload != "" {
				var data map[string]interface{}
				if err := json.Unmarshal([]byte(info.Payload), &data); err == nil {
					if params, ok := data["params"].(map[string]interface{}); ok {
						if toolName, ok := params["name"].(string); ok && toolName != "" {
							message += fmt.Sprintf(" `%s`", toolName)
						}
					}
				}
			}
		} else {
			message = fmt.Sprintf("**%s**%s`resp`", info.ServerID, dir)
		}
	}

	// Add formatted payload in code block
	if info.Payload != "" {
		// Remove jsonrpc and method fields, then format
		formatted, isValidJSON, isEmpty := formatJSONWithoutFields(info.Payload, []string{"jsonrpc", "method"})
		if isValidJSON {
			// Don't show JSON block if it's effectively empty (only params: null)
			if !isEmpty {
				// Valid JSON: use json code block for syntax highlighting (compact single line)
				// Empty line before code block per markdown convention
				// Code fences on their own lines with compact JSON content
				message += fmt.Sprintf("\n\n```json\n%s\n```", formatted)
			}
		} else {
			// Invalid JSON: use inline backticks to avoid malformed markdown
			message += fmt.Sprintf(" `%s`", formatted)
		}
	}

	// Error (if present)
	if info.Error != "" {
		message += fmt.Sprintf(" ⚠️`%s`", info.Error)
	}

	return message
}
