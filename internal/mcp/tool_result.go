package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw-mcpg/internal/logger"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var logToolResult = logger.New("mcp:tool_result")

// ConvertToCallToolResult converts backend result data to SDK CallToolResult format.
// The backend returns a JSON object with a "content" field containing an array of content items.
//
// Fast path: when data is already a deserialized map[string]interface{} (the common case
// after json.Unmarshal(response.Result, &interface{})), the function skips the redundant
// marshal/unmarshal round-trip and works with the map directly.
func ConvertToCallToolResult(data interface{}) (*sdk.CallToolResult, error) {
	logToolResult.Print("Converting backend result to CallToolResult")

	// Fast path: map[string]interface{} — avoids a full marshal+3×unmarshal cycle.
	if m, ok := data.(map[string]interface{}); ok {
		return convertMapToCallToolResult(m)
	}

	// Fast path: []interface{} — some backends return arrays directly.
	if _, ok := data.([]interface{}); ok {
		dataBytes, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal backend result: %w", err)
		}
		logToolResult.Printf("Backend returned array, wrapping as text")
		return &sdk.CallToolResult{
			Content: []sdk.Content{&sdk.TextContent{Text: string(dataBytes)}},
		}, nil
	}

	// Slow path: scalar types (string, nil, etc.) and anything else.
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal backend result: %w", err)
	}
	logToolResult.Printf("No content field found (scalar type), wrapping raw response as text")
	return &sdk.CallToolResult{
		Content: []sdk.Content{&sdk.TextContent{Text: string(dataBytes)}},
	}, nil
}

// convertMapToCallToolResult is the fast path for map[string]interface{} input.
// It inspects the map directly without marshaling, saving one marshal + up to three
// unmarshal operations compared to the original JSON round-trip approach.
func convertMapToCallToolResult(m map[string]interface{}) (*sdk.CallToolResult, error) {
	isError, _ := m["isError"].(bool)

	contentVal, hasContent := m["content"]
	if !hasContent || contentVal == nil {
		dataBytes, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal backend result: %w", err)
		}
		logToolResult.Printf("No content field found, wrapping raw response as text")
		return &sdk.CallToolResult{
			Content: []sdk.Content{&sdk.TextContent{Text: string(dataBytes)}},
		}, nil
	}

	contentArr, ok := contentVal.([]interface{})
	if !ok {
		// content field exists but is not an array — wrap the whole map as text.
		dataBytes, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal backend result: %w", err)
		}
		return &sdk.CallToolResult{
			Content: []sdk.Content{&sdk.TextContent{Text: string(dataBytes)}},
		}, nil
	}

	// Note: empty content array is valid and should be preserved (0 items).
	content := make([]sdk.Content, 0, len(contentArr))
	for _, item := range contentArr {
		ci, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		c, err := convertContentItem(ci)
		if err != nil {
			return nil, err
		}
		if c != nil {
			content = append(content, c)
		}
	}

	logToolResult.Printf("Converted result: content_items=%d, is_error=%v", len(content), isError)
	return &sdk.CallToolResult{Content: content, IsError: isError}, nil
}

// convertContentItem converts a single deserialized content-item map to the SDK Content type.
func convertContentItem(ci map[string]interface{}) (sdk.Content, error) {
	itemType, _ := ci["type"].(string)
	switch itemType {
	case "text":
		text, _ := ci["text"].(string)
		return &sdk.TextContent{Text: text}, nil
	case "image":
		mimeType, _ := ci["mimeType"].(string)
		data, err := decodeContentData(ci)
		if err != nil {
			return nil, fmt.Errorf("failed to decode image data: %w", err)
		}
		return &sdk.ImageContent{Data: data, MIMEType: mimeType}, nil
	case "audio":
		mimeType, _ := ci["mimeType"].(string)
		data, err := decodeContentData(ci)
		if err != nil {
			return nil, fmt.Errorf("failed to decode audio data: %w", err)
		}
		return &sdk.AudioContent{Data: data, MIMEType: mimeType}, nil
	case "resource":
		resVal, hasRes := ci["resource"]
		if !hasRes || resVal == nil {
			logToolResult.Printf("Resource content item missing 'resource' field, skipping")
			return nil, nil
		}
		// sdk.ResourceContents is a complex nested struct; use a targeted JSON round-trip
		// only for this item rather than the whole result.
		resBytes, err := json.Marshal(resVal)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal resource: %w", err)
		}
		var res sdk.ResourceContents
		if err := json.Unmarshal(resBytes, &res); err != nil {
			return nil, fmt.Errorf("failed to parse resource: %w", err)
		}
		return &sdk.EmbeddedResource{Resource: &res}, nil
	default:
		text, _ := ci["text"].(string)
		logToolResult.Printf("Unknown content type '%s', treating as text", itemType)
		return &sdk.TextContent{Text: text}, nil
	}
}

// decodeContentData decodes the base64-encoded "data" field from a content item map.
// When data arrives via json.Unmarshal into interface{}, []byte fields are stored as
// base64 strings; this function handles both the string and pre-decoded []byte forms.
func decodeContentData(ci map[string]interface{}) ([]byte, error) {
	switch v := ci["data"].(type) {
	case []byte:
		return v, nil
	case string:
		return base64.StdEncoding.DecodeString(v)
	default:
		return nil, nil
	}
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

// NewErrorCallToolResult creates a standard error CallToolResult with the error
// message included as text content.
func NewErrorCallToolResult(err error) (*sdk.CallToolResult, interface{}, error) {
	if err == nil {
		err = fmt.Errorf("unknown error")
	}
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{
			&sdk.TextContent{Text: err.Error()},
		},
	}, nil, err
}

// BuildMCPTextResponse returns a raw MCP response map with a single text content item.
func BuildMCPTextResponse(text string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	}
}
