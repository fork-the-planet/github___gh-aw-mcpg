package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw-mcpg/internal/logger"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// resourceContents mirrors sdk.ResourceContents for JSON unmarshaling of
// embedded resource content items returned by backend MCP servers.
type resourceContents struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     []byte `json:"blob,omitempty"`
}

var logToolResult = logger.New("mcp:tool_result")

// ConvertToCallToolResult converts backend result data to SDK CallToolResult format.
// The backend returns a JSON object with a "content" field containing an array of content items.
func ConvertToCallToolResult(data interface{}) (*sdk.CallToolResult, error) {
	logToolResult.Print("Converting backend result to CallToolResult")
	// Try to marshal and unmarshal to get the structure
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal backend result: %w", err)
	}

	// First, try to detect if the response is an array (some backends return arrays directly)
	var rawArray []json.RawMessage
	if err := json.Unmarshal(dataBytes, &rawArray); err == nil {
		// It's an array - wrap it as a single text content item
		logToolResult.Printf("Backend returned array with %d items, wrapping as text", len(rawArray))
		return &sdk.CallToolResult{
			Content: []sdk.Content{
				&sdk.TextContent{
					Text: string(dataBytes),
				},
			},
			IsError: false,
		}, nil
	}

	// Check if response is an object with a "content" field (standard MCP format)
	// We need to distinguish between:
	// 1. {"content": []} - empty array, should preserve as 0 content items
	// 2. {"content": [...]} - has items, process normally
	// 3. {"some": "other"} - no content field, wrap as text
	var hasContentField struct {
		Content *json.RawMessage `json:"content"`
		IsError bool             `json:"isError,omitempty"`
	}

	if err := json.Unmarshal(dataBytes, &hasContentField); err != nil || hasContentField.Content == nil {
		// No "content" field or parse error - wrap raw response as text
		logToolResult.Printf("No content field found, wrapping raw response as text")
		return &sdk.CallToolResult{
			Content: []sdk.Content{
				&sdk.TextContent{
					Text: string(dataBytes),
				},
			},
			IsError: false,
		}, nil
	}

	// Parse the backend result structure (standard MCP CallToolResult format)
	var backendResult struct {
		Content []struct {
			Type     string            `json:"type"`
			Text     string            `json:"text,omitempty"`
			Data     []byte            `json:"data,omitempty"`     // image/audio binary data (automatically decoded from base64 JSON)
			MIMEType string            `json:"mimeType,omitempty"` // image/audio MIME type
			Resource *resourceContents `json:"resource,omitempty"` // embedded resource
		} `json:"content"`
		IsError bool `json:"isError,omitempty"`
	}

	if err := json.Unmarshal(dataBytes, &backendResult); err != nil {
		// If parsing fails, wrap the raw response as text content
		logToolResult.Printf("Failed to parse as CallToolResult, wrapping raw response: %v", err)
		return &sdk.CallToolResult{
			Content: []sdk.Content{
				&sdk.TextContent{
					Text: string(dataBytes),
				},
			},
			IsError: false,
		}, nil
	}

	// Convert content items to SDK Content format
	// Note: Empty content array is valid and should be preserved (0 items)
	content := make([]sdk.Content, 0, len(backendResult.Content))
	for _, item := range backendResult.Content {
		switch item.Type {
		case "text":
			content = append(content, &sdk.TextContent{
				Text: item.Text,
			})
		case "image":
			content = append(content, &sdk.ImageContent{
				Data:     item.Data,
				MIMEType: item.MIMEType,
			})
		case "audio":
			content = append(content, &sdk.AudioContent{
				Data:     item.Data,
				MIMEType: item.MIMEType,
			})
		case "resource":
			if item.Resource != nil {
				content = append(content, &sdk.EmbeddedResource{
					Resource: &sdk.ResourceContents{
						URI:      item.Resource.URI,
						MIMEType: item.Resource.MIMEType,
						Text:     item.Resource.Text,
						Blob:     item.Resource.Blob,
					},
				})
			} else {
				logToolResult.Printf("Resource content item missing 'resource' field, skipping")
			}
		default:
			// For unknown types, preserve as text with whatever text field is present
			logToolResult.Printf("Unknown content type '%s', treating as text", item.Type)
			content = append(content, &sdk.TextContent{
				Text: item.Text,
			})
		}
	}

	logToolResult.Printf("Converted result: content_items=%d, is_error=%v", len(content), backendResult.IsError)
	return &sdk.CallToolResult{
		Content: content,
		IsError: backendResult.IsError,
	}, nil
}

// ParseToolArguments extracts and unmarshals tool arguments from a CallToolRequest.
// Returns the parsed arguments as a map, or an error if parsing fails.
func ParseToolArguments(req *sdk.CallToolRequest) (map[string]interface{}, error) {
	var toolArgs map[string]interface{}
	if req.Params != nil && req.Params.Arguments != nil {
		logToolResult.Printf("Parsing arguments for tool: %s", req.Params.Name)
		if err := json.Unmarshal(req.Params.Arguments, &toolArgs); err != nil {
			return nil, fmt.Errorf("failed to parse arguments: %w", err)
		}
	} else {
		// No arguments provided, use empty map
		toolArgs = make(map[string]interface{})
	}
	logToolResult.Printf("Parsed %d arguments", len(toolArgs))
	return toolArgs, nil
}
