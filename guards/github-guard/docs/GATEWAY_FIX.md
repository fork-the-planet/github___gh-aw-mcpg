# Gateway Fix: LabelResponse Interface Specification

## Problem Summary

The gateway passes MCP-wrapped responses to `LabelResponse`, but when the guard returns labeled items using the legacy `{"items": [...]}` format, the gateway fails to convert the result back to MCP format. The error is:

```
failed to convert result: failed to parse backend result structure: 
json: cannot unmarshal array into Go value of type struct { Content []struct {...} }
```

## Root Cause

1. **LabelResponse receives MCP-wrapped data**:
   ```json
   {"content":[{"type":"text","text":"{\"items\":[...actual data...]}"}]}
   ```

2. **Guard extracts inner JSON, labels items, returns**:
   ```json
   {"items":[{"data":{...item...},"labels":{...}},...]}
   ```

3. **Gateway calls `CollectionLabeledData.ToResult()`** which returns the raw items array

4. **Gateway tries to parse this as MCP format** → fails

---

## Recommended Fix: Option A - Unwrap Before, Rewrap After

### Modified `LabelResponse` in `wasm.go`

```go
// LabelResponse calls the WASM module's label_response function
func (g *WasmGuard) LabelResponse(ctx context.Context, toolName string, result interface{}, backend BackendCaller, caps *difc.Capabilities) (difc.LabeledData, error) {
    logWasm.Printf("LabelResponse called: toolName=%s", toolName)

    g.mu.Lock()
    defer g.mu.Unlock()
    g.backend = backend

    // NEW: Extract the actual response from MCP wrapper if present
    unwrappedResult, wasMCPWrapped := unwrapMCPResponse(result)

    // Prepare input with unwrapped result
    input := map[string]interface{}{
        "tool_name":   toolName,
        "tool_result": unwrappedResult,  // Pass unwrapped data to guard
    }
    if caps != nil {
        input["capabilities"] = caps
    }

    inputJSON, err := json.Marshal(input)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal input: %w", err)
    }

    resultJSON, err := g.callWasmFunction("label_response", inputJSON)
    if err != nil {
        return nil, err
    }

    if len(resultJSON) == 0 {
        return nil, nil
    }

    var responseMap map[string]interface{}
    if err := json.Unmarshal(resultJSON, &responseMap); err != nil {
        return nil, fmt.Errorf("failed to unmarshal WASM response: %w", err)
    }

    // Check for path-based labeling format
    if _, hasLabeledPaths := responseMap["labeled_paths"]; hasLabeledPaths {
        return parsePathLabeledResponse(resultJSON, unwrappedResult)
    }

    // Legacy format with items
    if items, ok := responseMap["items"].([]interface{}); ok && len(items) > 0 {
        labeledData, err := parseCollectionLabeledData(items)
        if err != nil {
            return nil, err
        }
        
        // NEW: Store the original MCP wrapper info for later rewrapping
        if wasMCPWrapped {
            labeledData.SetMCPWrapper(result)
        }
        
        return labeledData, nil
    }

    return nil, nil
}

// unwrapMCPResponse extracts the actual JSON from MCP content wrapper
// Returns (unwrapped data, true) if MCP wrapped, or (original, false) if not
func unwrapMCPResponse(result interface{}) (interface{}, bool) {
    resultMap, ok := result.(map[string]interface{})
    if !ok {
        return result, false
    }

    content, ok := resultMap["content"].([]interface{})
    if !ok || len(content) == 0 {
        return result, false
    }

    firstContent, ok := content[0].(map[string]interface{})
    if !ok {
        return result, false
    }

    textStr, ok := firstContent["text"].(string)
    if !ok {
        return result, false
    }

    // Parse the JSON string inside text
    var parsed interface{}
    if err := json.Unmarshal([]byte(textStr), &parsed); err != nil {
        return result, false
    }

    return parsed, true
}
```

### Modified `CollectionLabeledData` in `difc/labeled_data.go`

```go
type CollectionLabeledData struct {
    Items []LabeledItem
    
    // NEW: Store original MCP wrapper for rewrapping
    originalMCPWrapper interface{}
}

// SetMCPWrapper stores the original MCP response structure for rewrapping
func (c *CollectionLabeledData) SetMCPWrapper(wrapper interface{}) {
    c.originalMCPWrapper = wrapper
}

// ToResult returns the filtered items, rewrapped in MCP format if originally wrapped
func (c *CollectionLabeledData) ToResult() (interface{}, error) {
    // Extract just the data from labeled items
    items := make([]interface{}, 0, len(c.Items))
    for _, item := range c.Items {
        items = append(items, item.Data)
    }

    // If we have an original MCP wrapper, rewrap the result
    if c.originalMCPWrapper != nil {
        return c.rewrapAsMCP(items)
    }

    return items, nil
}

// rewrapAsMCP wraps the items back in MCP content format
func (c *CollectionLabeledData) rewrapAsMCP(items []interface{}) (interface{}, error) {
    // Serialize the items to JSON string
    itemsJSON, err := json.Marshal(items)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal items: %w", err)
    }

    // Reconstruct MCP format
    return map[string]interface{}{
        "content": []interface{}{
            map[string]interface{}{
                "type": "text",
                "text": string(itemsJSON),
            },
        },
    }, nil
}
```

---

## Interface Schemas

### Guard Input Schema for `label_response`

```typescript
interface LabelResponseInput {
    tool_name: string;           // e.g., "search_repositories"
    tool_result: any;            // UNWRAPPED response data (not MCP wrapped)
    capabilities?: Capabilities; // Optional agent capabilities
}

// Example for search_repositories:
{
    "tool_name": "search_repositories",
    "tool_result": {
        "total_count": 611,
        "incomplete_results": false,
        "items": [
            {
                "id": 739284,
                "name": "guardian.github.com",
                "full_name": "guardian/guardian.github.com",
                "private": false,
                ...
            },
            ...
        ]
    }
}
```

### Guard Output Schema for `label_response`

**Option 1: Legacy Items Format (currently implemented)**
```typescript
interface LabelResponseOutput {
    items: LabeledItem[];
}

interface LabeledItem {
    data: any;              // The original item data
    labels: ResourceLabels; // Labels for this item
}

interface ResourceLabels {
    description: string;
    secrecy: string[];
    integrity: string[];
}

// Example:
{
    "items": [
        {
            "data": {
                "id": 739284,
                "name": "guardian.github.com",
                "full_name": "guardian/guardian.github.com",
                "private": false
            },
            "labels": {
                "description": "repo:guardian/guardian.github.com",
                "secrecy": [],
                "integrity": []
            }
        },
        {
            "data": {
                "id": 123456,
                "name": "my-private-repo",
                "full_name": "user/my-private-repo",
                "private": true
            },
            "labels": {
                "description": "repo:user/my-private-repo",
                "secrecy": ["private:user/my-private-repo"],
                "integrity": []
            }
        }
    ]
}
```

**Option 2: Path-Based Format (more efficient, requires fix)**
```typescript
interface PathLabelResponseOutput {
    labeled_paths: PathLabel[];
    items_path?: string;        // e.g., "/items" - where to find items
    default_labels?: ResourceLabels;
}

interface PathLabel {
    path: string;  // JSON Pointer (RFC 6901), e.g., "/items/0"
    labels: ResourceLabels;
}

// Example:
{
    "labeled_paths": [
        {"path": "/items/0", "labels": {"description": "repo:a/b", "secrecy": [], "integrity": []}},
        {"path": "/items/1", "labels": {"description": "repo:x/y", "secrecy": ["private:x/y"], "integrity": []}}
    ],
    "items_path": "/items"
}
```

---

## Alternative Fix: Option B - Fix Path-Based Labeling

If you prefer path-based labeling (more efficient, no data copying), the fix is in `path_labels.go`:

### The Issue

The `PathLabeledData.getItems()` function expects to find items at `items_path` within the `OriginalData`, but `OriginalData` is MCP-wrapped.

### The Fix

```go
// In NewPathLabeledData, unwrap MCP if needed
func NewPathLabeledData(originalData interface{}, pathLabels *PathLabels) (*PathLabeledData, error) {
    // NEW: Unwrap MCP response if present
    unwrappedData, wasMCPWrapped := unwrapMCPResponse(originalData)
    
    pld := &PathLabeledData{
        OriginalData:    unwrappedData,  // Use unwrapped data
        PathLabels:      pathLabels,
        mcpWrapper:      nil,
    }
    
    if wasMCPWrapped {
        pld.mcpWrapper = originalData
    }

    if err := pld.resolve(); err != nil {
        return nil, fmt.Errorf("failed to resolve path labels: %w", err)
    }

    return pld, nil
}

// ToResult rewraps if needed
func (p *PathLabeledData) ToResult() (interface{}, error) {
    if p.mcpWrapper != nil {
        return p.rewrapAsMCP()
    }
    return p.OriginalData, nil
}
```

---

## Testing the Fix

After implementing, the guard should be able to:

1. **Receive unwrapped data** in `tool_result`:
   ```json
   {"tool_name": "search_repositories", "tool_result": {"total_count": 611, "items": [...]}}
   ```

2. **Return labeled items** without copying data (path-based) OR with data (legacy):
   ```json
   {"items": [{"data": {...}, "labels": {...}}, ...]}
   ```

3. **Gateway converts result** back to MCP format for the client:
   ```json
   {"content": [{"type": "text", "text": "[...filtered items...]"}]}
   ```

---

## Quick Workaround (No Gateway Changes)

If you can't modify the gateway immediately, the guard can skip fine-grained labeling for now:

```rust
// In label_response, return 0 to skip fine-grained labeling
// This falls back to resource-level labels from label_resource
pub fn label_response_items(...) -> Vec<LabeledItem> {
    // For now, return empty to avoid gateway conversion issues
    // Fine-grained filtering won't work, but basic access control will
    vec![]
}
```

This means private repos won't be filtered out of search results, but access to explicitly private resources will still be blocked by `label_resource`.

---

## Files to Modify

| File | Change |
|------|--------|
| `internal/guard/wasm.go` | Add `unwrapMCPResponse()`, call before `LabelResponse` |
| `internal/difc/labeled_data.go` | Add `SetMCPWrapper()`, modify `ToResult()` to rewrap |
| `internal/difc/path_labels.go` | (Option B) Add MCP unwrap/rewrap logic |
