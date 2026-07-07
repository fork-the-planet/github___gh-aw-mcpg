package guard

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

// LabelAgent calls the WASM module's label_agent function.
func (g *WasmGuard) LabelAgent(ctx context.Context, policy any, backend BackendCaller, caps *difc.Capabilities) (*LabelAgentResult, error) {
	logWasm.Printf("LabelAgent called: guard=%s", g.name)

	// Normalisation and payload-build operate only on the caller-supplied `policy`
	// argument and do not access any g.* fields, so they are safe to run outside
	// the lock that callWasmGuardFunction acquires.
	normalizedPolicy, err := normalizePolicyPayload(policy)
	if err != nil {
		logWasm.Printf("LabelAgent normalizePolicyPayload failed: guard=%s, error=%v", g.name, err)
		return nil, err
	}
	logger.LogMarshaledForDebugf(
		normalizedPolicy,
		logWasm.Printf,
		"LabelAgent normalized policy: guard=%s, policy=%s",
		logWasm.Printf,
		"LabelAgent normalized policy (marshal failed): guard=%s, error=%v",
		g.name,
	)
	_ = caps

	input, err := buildStrictLabelAgentPayload(normalizedPolicy)
	if err != nil {
		logWasm.Printf("LabelAgent buildStrictLabelAgentPayload failed: guard=%s, error=%v", g.name, err)
		return nil, err
	}

	resultJSON, err := g.callWasmGuardFunction(ctx, "label_agent", backend, input)
	if err != nil {
		logWasm.Printf("LabelAgent callWasmFunction failed: guard=%s, error=%v", g.name, err)
		return nil, err
	}
	logWasm.Printf("LabelAgent raw response JSON (%d bytes): %s", len(resultJSON), string(resultJSON))

	if len(resultJSON) == 0 {
		logWasm.Printf("LabelAgent returned empty response: guard=%s", g.name)
		return nil, fmt.Errorf("label_agent returned empty response")
	}

	result, err := parseLabelAgentResponse(resultJSON)
	if err != nil {
		logWasm.Printf("LabelAgent response validation failed: guard=%s, error=%v", g.name, err)
		return nil, err
	}

	logger.LogMarshaledForDebugf(
		result,
		logWasm.Printf,
		"LabelAgent parsed response: guard=%s, response=%s",
		logWasm.Printf,
		"LabelAgent parsed response (marshal failed): guard=%s, error=%v",
		g.name,
	)

	return result, nil
}

// LabelResource calls the WASM module's label_resource function
func (g *WasmGuard) LabelResource(ctx context.Context, toolName string, args any, backend BackendCaller, caps *difc.Capabilities) (*difc.LabeledResource, difc.OperationType, error) {
	logWasm.Printf("LabelResource called: toolName=%s, args=%+v", toolName, args)

	// Prepare input
	input := map[string]any{
		"tool_name": toolName,
		"tool_args": args,
	}
	if caps != nil {
		input["capabilities"] = caps
	}

	const funcName = "label_resource"
	resultJSON, err := g.callWasmGuardFunction(ctx, funcName, backend, input)
	if err != nil {
		return nil, difc.OperationWrite, err
	}

	// Parse result
	response, err := unmarshalWasmResponse(funcName, resultJSON)
	if err != nil {
		return nil, difc.OperationWrite, err
	}

	return parseResourceResponse(response)
}

// LabelResponse calls the WASM module's label_response function
func (g *WasmGuard) LabelResponse(ctx context.Context, toolName string, result any, backend BackendCaller, caps *difc.Capabilities) (difc.LabeledData, error) {
	logWasm.Printf("LabelResponse called: toolName=%s", toolName)

	// Prepare input
	input := map[string]any{
		"tool_name":   toolName,
		"tool_result": result,
	}
	if state := GetRequestStateFromContext(ctx); state != nil {
		if stateMap, ok := state.(map[string]any); ok {
			if toolArgs, hasToolArgs := stateMap["tool_args"]; hasToolArgs {
				input["tool_args"] = toolArgs
			}
		}
	}
	if caps != nil {
		input["capabilities"] = caps
	}

	const funcName = "label_response"
	resultJSON, err := g.callWasmGuardFunction(ctx, funcName, backend, input)
	if err != nil {
		return nil, err
	}

	// If empty result, return nil (no fine-grained labeling)
	if len(resultJSON) == 0 {
		return nil, nil
	}

	// Parse result - check for new path-based format first
	responseMap, err := unmarshalWasmResponse(funcName, resultJSON)
	if err != nil {
		return nil, err
	}

	// Check for path-based labeling format (preferred, more efficient)
	if _, hasLabeledPaths := responseMap["labeled_paths"]; hasLabeledPaths {
		return parsePathLabeledResponse(resultJSON, result)
	}

	// Legacy format: check if it's a collection with "items"
	if items, ok := responseMap["items"].([]any); ok && len(items) > 0 {
		return parseCollectionLabeledData(items)
	}

	// No fine-grained labeling
	return nil, nil
}

// unmarshalWasmResponse decodes a JSON byte slice returned by a WASM guard
// function into a generic map. funcName is included in the error message to
// make it easier to identify which call failed in log output.
func unmarshalWasmResponse(funcName string, data []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s WASM response: %w", funcName, err)
	}
	return m, nil
}

// validateIntegrityField returns an error if raw is not a valid integrity-level
// string. fieldName is used in the error message (e.g. "disapproval-integrity").
// It delegates to config.ValidateAndNormalizeIntegrityField for validation.
func validateIntegrityField(fieldName string, raw interface{}) error {
	s, ok := raw.(string)
	if !ok {
		s = ""
	}
	_, err := config.ValidateAndNormalizeIntegrityField(fieldName, s, false)
	return err
}
