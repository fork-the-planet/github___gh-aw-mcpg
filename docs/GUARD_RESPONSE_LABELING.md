# Guard Response Labeling

This document describes how guards label responses for DIFC (Decentralized Information Flow Control) enforcement in the MCP Gateway.

## Overview

Guards implement two labeling methods:

1. **`LabelResource()`** - Called BEFORE the backend request to determine:
   - Resource labels (secrecy/integrity requirements)
   - Operation type (read, write, read-write)

2. **`LabelResponse()`** - Called AFTER the backend request to provide:
   - Fine-grained per-item labels (for collections)
   - Or `nil` to use resource labels for entire response

## Supported Response Labeling Formats

The gateway supports multiple formats for `LabelResponse()` return values:

### 1. No Fine-Grained Labeling (`nil`)

```go
func (g *MyGuard) LabelResponse(...) (difc.LabeledData, error) {
    return nil, nil
}
```

**Behavior**: The entire response inherits labels from `LabelResource()`.

**Use when**: 
- The response is a single item
- All items in a collection have identical labels
- Fine-grained filtering is not needed

### 2. Simple Labeled Data

```go
return &difc.SimpleLabeledData{
    Data:   backendResult,
    Labels: &difc.LabeledResource{
        Description: "API response",
        Secrecy:     secrecyLabel,
        Integrity:   integrityLabel,
    },
}, nil
```

**Behavior**: Single item with specific labels.

**Use when**: Response is a single resource with uniform labels different from coarse-grained resource labels.

### 3. Collection Labeled Data (Legacy)

```go
return &difc.CollectionLabeledData{
    Items: []difc.LabeledItem{
        {
            Data: item1,
            Labels: &difc.LabeledResource{
                Description: "Public repo",
                Secrecy:     publicSecrecy,
                Integrity:   verifiedIntegrity,
            },
        },
        {
            Data: item2,
            Labels: &difc.LabeledResource{
                Description: "Private repo user/secret",
                Secrecy:     privateSecrecy,
                Integrity:   verifiedIntegrity,
            },
        },
    },
}, nil
```

**Behavior**: Each item in the collection has its own labels.

**Use when**: You need to build the labeled items list programmatically.

**Note**: This requires copying/reconstructing the data structure.

### 4. Path-Based Labeling (Preferred)

```json
{
  "labeled_paths": [
    {
      "path": "/items/0",
      "labels": {
        "description": "Public repository",
        "secrecy": ["public"],
        "integrity": ["github_verified"]
      }
    },
    {
      "path": "/items/1", 
      "labels": {
        "description": "Private repository user/secret-project",
        "secrecy": ["repo_private", "private:user/secret-project"],
        "integrity": ["github_verified"]
      }
    }
  ],
  "default_labels": {
    "secrecy": ["public"],
    "integrity": ["untrusted"]
  },
  "items_path": "/items"
}
```

**Behavior**: Labels are associated with JSON Pointer paths (RFC 6901) rather than copying data.

**Use when**: Labeling collections efficiently without data copying.

**Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `labeled_paths` | array | Path → labels mappings |
| `labeled_paths[].path` | string | JSON Pointer (RFC 6901) to the item |
| `labeled_paths[].labels` | object | Labels for this path |
| `labeled_paths[].labels.description` | string | Human-readable description (optional) |
| `labeled_paths[].labels.secrecy` | string[] | Secrecy tags |
| `labeled_paths[].labels.integrity` | string[] | Integrity tags |
| `default_labels` | object | Labels for items not explicitly listed (optional) |
| `items_path` | string | JSON Pointer to the collection (e.g., `/items`, `""` for root array) |

## Format Detection (WASM Guards)

For WASM guards, the gateway auto-detects the format:

1. If response contains `labeled_paths` key → Parse as **PathLabeledData**
2. If response contains `items` array → Parse as **CollectionLabeledData** (legacy)
3. Otherwise → Return `nil` (use resource labels)

## JSON Pointer Syntax (RFC 6901)

Path-based labeling uses JSON Pointer syntax:

| Pointer | Targets |
|---------|---------|
| `""` or `/` | Root document |
| `/items` | The `items` property |
| `/items/0` | First element of `items` array |
| `/items/5` | Sixth element of `items` array |
| `/data/users/0` | First user in nested structure |

**Escaping**:
- `~0` represents `~`
- `~1` represents `/`

## Example: GitHub Repository Search

For a `search_repositories` response:

```json
{
  "items": [
    {"full_name": "user/public-repo", "private": false},
    {"full_name": "user/private-repo", "private": true}
  ]
}
```

Guard returns:

```json
{
  "labeled_paths": [
    {
      "path": "/items/0",
      "labels": {
        "description": "user/public-repo",
        "secrecy": ["public"],
        "integrity": ["github_verified"]
      }
    },
    {
      "path": "/items/1",
      "labels": {
        "description": "user/private-repo",
        "secrecy": ["repo_private", "private:user/private-repo"],
        "integrity": ["github_verified"]
      }
    }
  ],
  "items_path": "/items"
}
```

## Filtering Behavior

After `LabelResponse()`, the Reference Monitor:

1. **Strict mode**: Blocks if any item violates DIFC policy
2. **Filter mode**: Removes inaccessible items from the response
3. **Propagate mode**: Allows access, updates agent labels based on what was read

## Performance Considerations

| Format | Data Copying | Memory | Best For |
|--------|-------------|--------|----------|
| `nil` | None | Minimal | Uniform labels |
| `SimpleLabeledData` | Reference only | Low | Single items |
| `CollectionLabeledData` | Full copy | High | Complex transformations |
| `PathLabeledData` | None | Low | **Large collections** |

**Recommendation**: Use path-based labeling for collections to avoid copying response data.
