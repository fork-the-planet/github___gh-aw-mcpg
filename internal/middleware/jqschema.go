package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/strutil"
	"github.com/itchyny/gojq"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var logMiddleware = logger.New("middleware:jqschema")

// DefaultJqTimeout is the default timeout for jq query execution (5 seconds)
// This prevents malformed queries or large payloads from causing hangs
const DefaultJqTimeout = 5 * time.Second

// PayloadPreviewSize is the maximum number of characters to include in the payload preview
// This controls how much of the original payload is returned inline when a payload is stored to disk
const PayloadPreviewSize = 500

// PayloadTruncatedInstructions is the message returned to clients when a payload
// has been truncated and saved to the filesystem
const PayloadTruncatedInstructions = "The payload was too large for an MCP response. The complete original response data is saved as a JSON file at payloadPath. The file contains valid JSON that can be parsed directly. The payloadSchema shows the structure and types of fields in the full response, but not the actual values. To access the full data with all values, read and parse the JSON file at payloadPath."

// PayloadMetadata represents the metadata response returned when a payload is too large
// and has been saved to the filesystem
type PayloadMetadata struct {
	AgentInstructions string      `json:"agentInstructions"`
	PayloadPath       string      `json:"payloadPath"`
	PayloadPreview    string      `json:"payloadPreview"`
	PayloadSchema     interface{} `json:"payloadSchema"`
	OriginalSize      int         `json:"originalSize"`
	QueryID           string      `json:"-"` // Internal use only, not serialized to clients
}

// jqSchemaFilter is the jq entry point that invokes the native Go walk_schema function.
// The recursive schema-walk logic is implemented in inferSchema (see below) and registered
// via gojq.WithFunction, so the filter itself is a single call.
//
// The transformation replaces every leaf value with its jq type name:
//
//	Input:  {"name": "test", "count": 42, "items": [{"id": 1}]}
//	Output: {"name": "string", "count": "number", "items": [{"id": "number"}]}
//
// For arrays, only the first element's schema is retained to represent the array structure.
// Empty arrays are preserved as [].
const jqSchemaFilter = `walk_schema`

// Pre-compiled jq query code for performance.
// This is compiled once at package initialization and reused for all requests.
var (
	jqSchemaCode       *gojq.Code
	jqSchemaCompileErr error
)

// inferSchema recursively walks a JSON-compatible Go value and replaces every leaf
// with its jq type name ("null", "boolean", "number", "string"). Objects are
// traversed key-by-key; arrays are collapsed to a single representative element (or
// [] when empty). The output mirrors what the previous pure-jq walk_schema filter
// produced, but runs entirely in Go, bypassing jq interpreter overhead for recursion.
//
// Type mapping (matches jq's built-in type function):
//   - nil                                          → "null"
//   - bool                                         → "boolean"
//   - any integer or floating-point numeric type   → "number"
//     (float32/64, int/8/16/32/64, uint/8/16/32/64, json.Number)
//   - string                                       → "string"
//   - map[string]interface{}                       → recursed object
//   - []interface{}                                → recursed array (first element only)
func inferSchema(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, child := range val {
			result[k] = inferSchema(child)
		}
		return result
	case []interface{}:
		if len(val) == 0 {
			return []interface{}{}
		}
		return []interface{}{inferSchema(val[0])}
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64, float32,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		json.Number:
		return "number"
	case string:
		return "string"
	default:
		// Defensive fallback: classify any remaining numeric reflect.Kind as "number"
		// and everything else as "string" to keep the schema output valid.
		switch reflect.TypeOf(v).Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			return "number"
		default:
			return "string"
		}
	}
}

// init compiles the jq schema filter at startup for better performance and validation.
// Following gojq best practices: compile once, run many times.
//
// The walk_schema function is registered as a native Go implementation via
// gojq.WithFunction so that the recursive schema walk runs entirely in Go,
// avoiding jq interpreter overhead for deeply-nested payloads.
//
// This provides fail-fast behavior - if the jq query is invalid, the application
// will fail at startup rather than at runtime during a tool call.
func init() {
	query, err := gojq.Parse(jqSchemaFilter)
	if err != nil {
		jqSchemaCompileErr = fmt.Errorf("failed to parse jq schema filter: %w", err)
		log.Printf("FATAL: Failed to parse jq schema filter at init (application will not start): %v", err)
		return
	}

	jqSchemaCode, jqSchemaCompileErr = gojq.Compile(query,
		gojq.WithFunction("walk_schema", 0, 0, func(v interface{}, _ []interface{}) interface{} {
			return inferSchema(v)
		}),
	)
	if jqSchemaCompileErr != nil {
		log.Printf("FATAL: Failed to compile jq schema filter at init (application will not start): %v", jqSchemaCompileErr)
		return
	}

	logger.LogInfo("startup", "jq schema filter compiled successfully - native Go walk_schema, array limit: 2^29 elements, timeout: %v", DefaultJqTimeout)
}

// generateRandomID generates a random ID for payload storage
func generateRandomID() string {
	return strutil.RandomHexWithFallback(16)
}

// applyJqSchema applies the jq schema transformation to JSON data
// Uses pre-compiled query code for better performance (3-10x faster than parsing on each request)
//
// Accepts a context for timeout and cancellation support. If the context does not have a deadline,
// a default timeout of DefaultJqTimeout (5 seconds) is enforced to prevent hangs from:
// - Malformed jq queries
// - Extremely large or deeply nested payloads
// - Infinite loops in query logic
//
// Returns the schema as an interface{} object (not a JSON string)
//
// Error handling:
// - Returns compilation errors if init() failed
// - Returns context.DeadlineExceeded if query times out
// - Returns enhanced gojq type error messages when available
// - Properly handles gojq.HaltError for clean halt conditions
func applyJqSchema(ctx context.Context, jsonData interface{}) (interface{}, error) {
	// Check if compilation succeeded at init time
	if jqSchemaCompileErr != nil {
		return nil, fmt.Errorf("jq schema filter not compiled (check startup logs): %w", jqSchemaCompileErr)
	}

	logMiddleware.Printf("applyJqSchema: starting schema inference, dataType=%T", jsonData)

	// Ensure context has a timeout - add default if none exists
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultJqTimeout)
		defer cancel()
		logMiddleware.Printf("Applied default timeout of %v to jq query execution", DefaultJqTimeout)
	}

	// Run the pre-compiled query with context support (much faster than Parse+Run)
	// The iterator is consumed only once because the walk_schema filter produces exactly
	// one output value (the fully-transformed schema). No further drain is needed because
	// gojq iterators are synchronous and do not leave background goroutines running.
	iter := jqSchemaCode.RunWithContext(ctx, jsonData)
	v, ok := iter.Next()
	if !ok {
		return nil, fmt.Errorf("jq schema filter returned no results")
	}

	// Check for errors with type-specific handling
	if err, ok := v.(error); ok {
		// Check for context errors first (timeout or cancellation)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("jq query execution failed: %w", ctx.Err())
		}

		// Check for HaltError - a clean halt with exit code
		if haltErr, ok := err.(*gojq.HaltError); ok {
			// HaltError with nil value means clean halt (not an error)
			if haltErr.Value() == nil {
				return nil, fmt.Errorf("jq schema filter halted cleanly with no output")
			}
			// HaltError with non-nil value is an actual error
			return nil, fmt.Errorf("jq schema filter halted with error (exit code %d): %w", haltErr.ExitCode(), err)
		}

		// Generic error case (includes enhanced gojq type error messages when available)
		return nil, fmt.Errorf("jq schema filter error: %w", err)
	}

	// Return the schema object directly (no JSON marshaling needed here)
	logMiddleware.Printf("applyJqSchema: schema inference completed, resultType=%T", v)
	return v, nil
}

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

// WrapToolHandler wraps a tool handler with jqschema middleware
// This middleware:
// 1. Generates a random ID for the query
// 2. Extracts session ID from context (or uses "default")
// 3. If payload size > sizeThreshold: saves to {baseDir}/{sessionID}/{queryID}/payload.json and returns metadata
// 4. If payload size <= sizeThreshold: returns original response directly (no file storage)
// 5. For large payloads: returns first PayloadPreviewSize chars of payload + jq inferred schema
// 6. Uses pathPrefix to remap returned payloadPath for clients (if configured)
func WrapToolHandler(
	handler func(context.Context, *sdk.CallToolRequest, interface{}) (*sdk.CallToolResult, interface{}, error),
	toolName string,
	baseDir string,
	pathPrefix string,
	sizeThreshold int,
	getSessionID func(context.Context) string,
) func(context.Context, *sdk.CallToolRequest, interface{}) (*sdk.CallToolResult, interface{}, error) {
	logMiddleware.Printf("WrapToolHandler: wrapping tool=%s, sizeThreshold=%d bytes, baseDir=%s, pathPrefixSet=%v",
		toolName, sizeThreshold, baseDir, pathPrefix != "")
	return func(ctx context.Context, req *sdk.CallToolRequest, args interface{}) (*sdk.CallToolResult, interface{}, error) {
		// Generate random query ID
		queryID := generateRandomID()

		// Get session ID from context
		sessionID := getSessionID(ctx)
		if sessionID == "" {
			sessionID = "default"
		}

		logger.LogDebug("payload", "Middleware processing tool call: tool=%s, queryID=%s, session=%s, baseDir=%s",
			toolName, queryID, sessionID, baseDir)

		// Call the original handler
		result, data, err := handler(ctx, req, args)
		if err != nil {
			logger.LogDebug("payload", "Tool call failed, skipping payload storage: tool=%s, queryID=%s, error=%v",
				toolName, queryID, err)
			return result, data, err
		}

		// Only process successful results with data
		if result == nil || result.IsError || data == nil {
			logger.LogDebug("payload", "Skipping payload storage: tool=%s, queryID=%s, reason=%s",
				toolName, queryID,
				func() string {
					if result == nil {
						return "result is nil"
					} else if result.IsError {
						return "result indicates error"
					} else {
						return "no data returned"
					}
				}())
			return result, data, err
		}

		// Marshal the response data to JSON
		payloadJSON, marshalErr := json.Marshal(data)
		if marshalErr != nil {
			logger.LogError("payload", "Failed to marshal response data to JSON: tool=%s, queryID=%s, error=%v",
				toolName, queryID, marshalErr)
			return result, data, err
		}

		payloadSize := len(payloadJSON)
		logger.LogInfo("payload", "Response data marshaled to JSON: tool=%s, queryID=%s, size=%d bytes (%.2f KB, %.2f MB), threshold=%d bytes",
			toolName, queryID, payloadSize, float64(payloadSize)/1024, float64(payloadSize)/(1024*1024), sizeThreshold)

		// Check if payload size is within threshold - if so, return original response directly
		if payloadSize <= sizeThreshold {
			logger.LogInfo("payload", "Payload size (%d bytes) is within threshold (%d bytes), returning inline without file storage: tool=%s, queryID=%s",
				payloadSize, sizeThreshold, toolName, queryID)
			// Return the original result without modification
			return result, data, err
		}

		// Payload is larger than threshold - save to filesystem
		logger.LogInfo("payload", "Payload size (%d bytes) exceeds threshold (%d bytes), saving to filesystem: tool=%s, queryID=%s, session=%s, baseDir=%s",
			payloadSize, sizeThreshold, toolName, queryID, sessionID, baseDir)

		filePath, saveErr := savePayload(baseDir, pathPrefix, sessionID, queryID, payloadJSON)
		if saveErr != nil {
			logger.LogError("payload", "Failed to save payload to filesystem: tool=%s, queryID=%s, session=%s, error=%v",
				toolName, queryID, sessionID, saveErr)
			// Continue even if save fails - don't break the tool call
		} else {
			logger.LogInfo("payload", "Payload storage completed successfully: tool=%s, queryID=%s, session=%s, path=%s, size=%d bytes",
				toolName, queryID, sessionID, filePath, len(payloadJSON))
		}

		// Apply jq schema transformation
		logger.LogDebug("payload", "Applying jq schema transformation: tool=%s, queryID=%s", toolName, queryID)
		var schemaObj interface{}
		if schemaErr := func() error {
			// Prepare data for jq processing. If data is already a native JSON-compatible
			// Go type, use it directly to avoid a redundant JSON round-trip.
			var jsonData interface{}
			switch v := data.(type) {
			case map[string]interface{}, []interface{}, string, float64, bool:
				jsonData = data
			case nil:
				// nil data produces a nil schema; nothing meaningful to infer.
				return nil
			case json.Number:
				// json.Number is emitted by decoders using UseNumber(); convert to
				// float64 so jq sees a plain number value rather than an opaque type.
				f, convErr := v.Float64()
				if convErr != nil {
					return fmt.Errorf("failed to convert json.Number to float64: %w", convErr)
				}
				jsonData = f
			default:
				if err := json.Unmarshal(payloadJSON, &jsonData); err != nil {
					return fmt.Errorf("failed to unmarshal for schema: %w", err)
				}
			}

			schema, err := applyJqSchema(ctx, jsonData)
			if err != nil {
				return err
			}
			schemaObj = schema
			return nil
		}(); schemaErr != nil {
			logger.LogWarn("payload", "Failed to generate schema for payload: tool=%s, queryID=%s, error=%v",
				toolName, queryID, schemaErr)
			// Continue with original response if schema extraction fails
			return result, data, err
		}

		logger.LogDebug("payload", "Schema transformation completed: tool=%s, queryID=%s", toolName, queryID)

		// Build the transformed response: first PayloadPreviewSize bytes + schema.
		// Slice the bytes before converting to string to avoid allocating a full copy of the
		// (potentially multi-MB) payload when only a short preview is needed.
		//
		// json.Marshal emits raw UTF-8 for non-ASCII runes, so a naive byte slice could
		// split a multi-byte sequence. We adjust the cut point backward to the nearest
		// valid rune boundary to guarantee the preview is valid UTF-8.
		payloadLen := len(payloadJSON)
		var preview string
		truncated := payloadLen > PayloadPreviewSize
		if truncated {
			cutPoint := PayloadPreviewSize
			for cutPoint > 0 && !utf8.RuneStart(payloadJSON[cutPoint]) {
				cutPoint--
			}
			preview = string(payloadJSON[:cutPoint]) + "..."
			logger.LogInfo("payload", "Payload truncated for preview: tool=%s, queryID=%s, originalSize=%d bytes, previewSize=%d bytes",
				toolName, queryID, payloadLen, PayloadPreviewSize)
		} else {
			preview = string(payloadJSON)
			logger.LogDebug("payload", "Payload small enough for full preview: tool=%s, queryID=%s, size=%d bytes",
				toolName, queryID, payloadLen)
		}

		// Create rewritten response using the PayloadMetadata struct
		rewrittenResponse := PayloadMetadata{
			AgentInstructions: PayloadTruncatedInstructions,
			PayloadPath:       filePath,
			PayloadPreview:    preview,
			PayloadSchema:     schemaObj,
			OriginalSize:      payloadLen,
			QueryID:           queryID,
		}

		logger.LogInfo("payload", "Created metadata response for client: tool=%s, queryID=%s, session=%s, payloadPath=%s, originalSize=%d bytes, truncated=%v",
			toolName, queryID, sessionID, filePath, payloadLen, truncated)

		// Marshal the rewritten response to JSON for the Content field
		rewrittenJSON, marshalErr := json.Marshal(rewrittenResponse)
		if marshalErr != nil {
			logger.LogError("payload", "Failed to marshal metadata response: tool=%s, queryID=%s, error=%v",
				toolName, queryID, marshalErr)
			// Fall back to original result if we can't marshal
			return result, rewrittenResponse, nil
		}

		logger.LogDebug("payload", "Metadata response marshaled: tool=%s, queryID=%s, metadataSize=%d bytes",
			toolName, queryID, len(rewrittenJSON))

		// Create a new CallToolResult with the transformed content
		// Replace the original content with our rewritten response
		transformedResult := &sdk.CallToolResult{
			Content: []sdk.Content{
				&sdk.TextContent{
					Text: string(rewrittenJSON),
				},
			},
			IsError: result.IsError,
			Meta:    result.Meta,
		}

		logger.LogInfo("payload", "Returning transformed response to client: tool=%s, queryID=%s, session=%s, payloadPath=%s, clientReceivesMetadata=true",
			toolName, queryID, sessionID, filePath)
		logger.LogInfo("payload", "Client can access full payload at: %s (inside container: /workspace/mcp-payloads/%s/%s/payload.json)",
			filePath, sessionID, queryID)

		return transformedResult, rewrittenResponse, nil
	}
}

// ShouldApplyMiddleware determines if the middleware should be applied to a tool
// Currently applies to all tools, but can be configured to filter specific tools
func ShouldApplyMiddleware(toolName string) bool {
	// Apply to all tools except sys tools
	applies := !strings.HasPrefix(toolName, "sys___")
	logMiddleware.Printf("ShouldApplyMiddleware: tool=%s, applies=%v", toolName, applies)
	return applies
}
