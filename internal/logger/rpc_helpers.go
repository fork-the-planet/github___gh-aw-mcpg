// Package logger provides structured logging for the MCP Gateway.
//
// This file contains helper functions for processing RPC message payloads.
//
// Functions in this file:
//
// - truncateAndSanitize: Combines secret sanitization with length truncation
// - LogMarshaledForDebug: Marshals a value and dispatches to success/failure callbacks
//
// These helpers are used by the RPC logging system to safely and efficiently
// process message payloads before logging them.
package logger

import (
	"encoding/json"

	"github.com/github/gh-aw-mcpg/internal/sanitize"
	"github.com/github/gh-aw-mcpg/internal/strutil"
)

// truncateAndSanitize truncates the payload to max length and sanitizes secrets
func truncateAndSanitize(payload string, maxLength int) string {
	// First sanitize secrets
	sanitized := sanitize.SanitizeString(payload)

	// Then truncate if needed
	return strutil.Truncate(sanitized, maxLength)
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
