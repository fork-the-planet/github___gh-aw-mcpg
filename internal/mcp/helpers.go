package mcp

import (
	"encoding/json"
	"fmt"
)

// marshalToResponse marshals an SDK result into a Response object.
// This helper reduces code duplication across all MCP method wrappers.
//
// The ID field is set to a static placeholder (1) because this Response is only
// constructed after the SDK's session.XXX() call has already resolved the
// request–response correlation internally. The gateway never uses this ID for
// matching; it is present solely to satisfy the JSON-RPC 2.0 structure.
func marshalToResponse(result interface{}) (*Response, error) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      1, // Placeholder – see function comment for safety rationale
		Result:  resultJSON,
	}, nil
}

// unmarshalParams converts generic interface{} params to a specific struct type.
// This helper reduces code duplication across MCP method wrappers and ensures
// consistent error handling for parameter conversion. It uses marshal/unmarshal
// to maintain JSON schema validation benefits.
func unmarshalParams(params interface{}, target interface{}) error {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal params: %w", err)
	}
	if err := json.Unmarshal(paramsJSON, target); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

// callParamMethod is a generic helper for SDK operations that require typed parameters.
// It handles the common pattern of: requireSDKSession → unmarshalParams → fn(params) → marshalToResponse.
// P is the type of the parameter struct to unmarshal into.
func callParamMethod[P any](c *Connection, rawParams interface{}, fn func(P) (interface{}, error)) (*Response, error) {
	if err := c.requireSDKSession(); err != nil {
		return nil, err
	}
	var params P
	if err := unmarshalParams(rawParams, &params); err != nil {
		return nil, err
	}
	result, err := fn(params)
	if err != nil {
		return nil, err
	}
	return marshalToResponse(result)
}
