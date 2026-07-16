package guard

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/difc"
)

// parseLabelAgentResponse validates and decodes the raw JSON returned by the
// WASM label_agent function into a LabelAgentResult.
func parseLabelAgentResponse(resultJSON []byte) (*LabelAgentResult, error) {
	var raw map[string]any
	if err := json.Unmarshal(resultJSON, &raw); err != nil {
		logWasm.Printf("label_agent response parse error (invalid JSON): error=%v, raw=%s", err, string(resultJSON))
		return nil, fmt.Errorf("failed to unmarshal label_agent response: %w", err)
	}

	if err := checkBoolFailure(raw, resultJSON, "success"); err != nil {
		return nil, err
	}
	if err := checkBoolFailure(raw, resultJSON, "ok"); err != nil {
		return nil, err
	}
	if message, ok := raw["error"].(string); ok && strings.TrimSpace(message) != "" {
		logWasm.Printf("label_agent response contained error field: error=%s, response=%s", message, string(resultJSON))
		return nil, fmt.Errorf("label_agent returned error: %s", message)
	}

	var result LabelAgentResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		logWasm.Printf("label_agent response decode error: error=%v, response=%s", err, string(resultJSON))
		return nil, fmt.Errorf("failed to decode label_agent response: %w", err)
	}

	if strings.TrimSpace(result.DIFCMode) == "" {
		logWasm.Printf("label_agent response missing difc_mode: response=%s", string(resultJSON))
		return nil, fmt.Errorf("label_agent response missing difc_mode")
	}

	if _, err := difc.ParseEnforcementMode(result.DIFCMode); err != nil {
		logWasm.Printf("label_agent response invalid difc_mode=%q: error=%v, response=%s", result.DIFCMode, err, string(resultJSON))
		return nil, fmt.Errorf("invalid difc_mode from label_agent: %w", err)
	}

	return &result, nil
}

// parsePathLabeledResponse parses the path-based labeling format.
// This is more efficient as guards don't need to copy data, just return paths and labels.
func parsePathLabeledResponse(responseJSON []byte, originalData any) (difc.LabeledData, error) {
	logWasm.Printf("parsePathLabeledResponse: responseSize=%d", len(responseJSON))

	pathLabels, err := difc.ParsePathLabels(responseJSON)
	if err != nil {
		logWasm.Printf("parsePathLabeledResponse: failed to parse path labels: %v", err)
		return nil, fmt.Errorf("failed to parse path labels: %w", err)
	}
	logWasm.Printf("parsePathLabeledResponse: parsed %d path labels", len(pathLabels.LabeledPaths))

	pld, err := difc.NewPathLabeledData(originalData, pathLabels)
	if err != nil {
		logWasm.Printf("parsePathLabeledResponse: failed to apply path labels: %v", err)
		return nil, fmt.Errorf("failed to apply path labels: %w", err)
	}

	// Convert to CollectionLabeledData for compatibility with existing filtering
	result := pld.ToCollectionLabeledData()
	logWasm.Printf("parsePathLabeledResponse: converted to CollectionLabeledData successfully")
	return result, nil
}

// parseDIFCTagsFromAny converts a raw []any JSON tag list to []difc.Tag.
// Returns nil if raw is nil or not a []any.
func parseDIFCTagsFromAny(raw any) []difc.Tag {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	tags := make([]difc.Tag, 0, len(items))
	for _, item := range items {
		if tagStr, ok := item.(string); ok {
			tags = append(tags, difc.Tag(tagStr))
		}
	}
	return tags
}

// fillLabeledResourceFromMap populates description, secrecy, and integrity fields
// on the provided LabeledResource from a decoded JSON map.
func fillLabeledResourceFromMap(rawData map[string]any, resource *difc.LabeledResource) {
	if desc, ok := rawData["description"].(string); ok {
		resource.Description = desc
	}

	resource.Secrecy = *difc.NewSecrecyLabel(parseDIFCTagsFromAny(rawData["secrecy"])...)
	resource.Integrity = *difc.NewIntegrityLabel(parseDIFCTagsFromAny(rawData["integrity"])...)
}

// parseResourceResponse converts the guard label_resource response to a LabeledResource.
func parseResourceResponse(response map[string]any) (*difc.LabeledResource, difc.OperationType, error) {
	resourceData, ok := response["resource"].(map[string]any)
	if !ok {
		return nil, difc.OperationWrite, fmt.Errorf("invalid resource format in guard response")
	}

	resource := &difc.LabeledResource{}
	fillLabeledResourceFromMap(resourceData, resource)

	// Parse operation type
	operation := difc.OperationWrite // default to most restrictive
	if opStr, ok := response["operation"].(string); ok {
		switch opStr {
		case "read":
			operation = difc.OperationRead
		case "write":
			operation = difc.OperationWrite
		case "read-write":
			operation = difc.OperationReadWrite
		}
	}

	logWasm.Printf("Parsed resource response: description=%q, operation=%v", resource.Description, operation)
	return resource, operation, nil
}

// parseCollectionLabeledData converts an array of items to CollectionLabeledData.
func parseCollectionLabeledData(items []any) (*difc.CollectionLabeledData, error) {
	logWasm.Printf("parseCollectionLabeledData: itemCount=%d", len(items))
	collection := &difc.CollectionLabeledData{
		Items: make([]difc.LabeledItem, 0, len(items)),
	}

	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		labeledItem := difc.LabeledItem{
			Data: itemMap["data"],
		}

		// Parse labels
		if labelsData, ok := itemMap["labels"].(map[string]any); ok {
			labels := &difc.LabeledResource{}
			fillLabeledResourceFromMap(labelsData, labels)

			labeledItem.Labels = labels
		}

		collection.Items = append(collection.Items, labeledItem)
	}

	logWasm.Printf("parseCollectionLabeledData: parsed %d labeled items from %d input items", len(collection.Items), len(items))
	return collection, nil
}

// checkBoolFailure returns a non-nil error if the given raw response map
// contains field key set to false, extracting the "error" message if present.
func checkBoolFailure(raw map[string]interface{}, resultJSON []byte, key string) error {
	val, ok := raw[key].(bool)
	if !ok || val {
		return nil // field absent or true — not a failure
	}
	if message, msgOK := raw["error"].(string); msgOK && strings.TrimSpace(message) != "" {
		logWasm.Printf("label_agent response indicated failure: error=%s, response=%s", message, string(resultJSON))
		return fmt.Errorf("label_agent rejected policy: %s", message)
	}
	logWasm.Printf("label_agent response indicated non-success status: response=%s", string(resultJSON))
	return fmt.Errorf("label_agent returned non-success status")
}
