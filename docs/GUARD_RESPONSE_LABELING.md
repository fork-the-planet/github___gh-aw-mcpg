# Guard Response Labeling

This document describes how guards label responses for DIFC (Decentralized Information Flow Control) enforcement in the MCP Gateway.

## DIFC Label Rules

DIFC uses two types of labels to control information flow:

### Secrecy Labels

Secrecy labels prevent unauthorized writes ("no write down"):

| Operation | Rule | Example |
|-----------|------|---------|
| **Read** | Agent must have ≥ resource secrecy tags | Resource `S_r={'secret'}` requires agent to have `S_a={'secret'}` |
| **Write** | Resource must have ≥ agent secrecy tags | Agent with `S_a={'secret'}` can only write to resources with `S_r={'secret'}` |

**Intuition**: Secrecy tags track what sensitive data an agent has seen. Reading secret data "taints" the agent, and tainted agents cannot leak data to less-secret destinations.

### Integrity Labels

Integrity labels prevent untrusted reads ("no read down"):

| Operation | Rule | Example |
|-----------|------|---------|
| **Read** | Resource must have ≥ agent integrity tags | Agent with `I_a={'verified'}` can only read from resources with `I_r={'verified'}` |
| **Write** | Agent must have ≥ resource integrity tags | Resource `I_r={'trusted'}` requires agent to have `I_a={'trusted'}` |

**Intuition**: Integrity tags track trustworthiness. Reading untrusted data "degrades" the agent's integrity, and degraded agents cannot write to high-integrity destinations.

### Flow Rules Summary

```
Read:  resource.secrecy  ⊆ agent.secrecy    (agent has clearance)
       resource.integrity ⊇ agent.integrity  (agent trusts resource)

Write: agent.secrecy    ⊆ resource.secrecy  (no information leak)
       agent.integrity  ⊇ resource.integrity (agent is trustworthy)
```

## DIFC Modes

The gateway supports three enforcement modes:

1. **Strict**:

Agent labels are NEVER updated.

For each tool call, the gateway first calls `LabelResource()` to get resource labels and operation type (i.e., read, write, read-write).

If the operation is a read, the gateway makes the tool call and then calls `LabelResponse()` to get fine-grained labels for the response. The Reference Monitor then checks DIFC rules for each item and blocks the entire response if any item violates the rules.

If the operation is read-write or write, then the Reference Monitor checks DIFC rules based on resource labels before the tool call, and blocks if the rules are violated. For read-write and write operations, `LabelResponse()` is NOT called.

2. **Filter**:

Agent labels are NEVER updated.

For each tool call, the gateway first calls `LabelResource()` to get resource labels and operation type (i.e., read, write, read-write).

If the operation is a read, the gateway makes the tool call and then calls `LabelResponse()` to get fine-grained labels for the response. The Reference Monitor then checks DIFC rules for each item and removes any items that violate the rules from the response (instead of blocking the entire response). This allows agents to still get access to items they are authorized for, while filtering out unauthorized items.

If the operation is read-write or write, then the Reference Monitor checks DIFC rules based on resource labels before the tool call, and blocks if the rules are violated. If the rules are not violated, the tool call proceeds. For read-write operations, the Reference Monitor calls `LabelResponse()` to get fine-grained labels for the response. The Reference Monitor then checks DIFC rules for each item and removes any items that violate the rules from the response (instead of blocking the entire response). This allows agents to still get access to items they are authorized for, while filtering out unauthorized items. For write operations in filter mode, `LabelResponse()` is NOT called.

3. **Propagate**:

Agent labels are may be updated based on the labels of data they access. However, tool calls will only ever add tags to the agent's secrecy labels and remove tags from the agent's integrity labels, to ensure that agents can only become more restricted over time.

For each tool call, the gateway first calls `LabelResource()` to get resource labels and operation type (i.e., read, write, read-write).

If the operation is a read, the gateway makes the tool call and then calls `LabelResponse()` to get fine-grained labels for the response. For each item in the response, the Reference Monitor sets the agent's secrecy label to the union of the agent's current secrecy label and the item's secrecy label and sets the agent's integrity label to the intersection of the agent's current integrity label and the item's integrity label.

If the operation is read-write or write, then the Reference Monitor checks DIFC rules based on resource labels before the tool call, and blocks if the rules are violated. If the rules are not violated, the tool call proceeds. For read-write operations, the Reference Monitor calls `LabelResponse()` to get fine-grained labels for the response. For each item in the response, the Reference Monitor sets the agent's secrecy label to the union of the agent's current secrecy label and the item's secrecy label and sets the agent's integrity label to the intersection of the agent's current integrity label and the item's integrity label.  For write operations in propagate mode, `LabelResponse()` is NOT called.

## Overview

Guards implement three labeling methods:

1. **`LabelAgent()`** - Called ONCE per session/guard/policy combination to initialize agent state:
   - Validates and normalizes the guard policy (e.g., `AllowOnly` rules)
   - Returns effective agent secrecy/integrity labels for the session
   - Returns the DIFC enforcement mode (`strict`, `filter`, or `propagate`)
   - Returns a normalized policy for subsequent calls
   - Results are cached per session — subsequent tool calls skip re-initialization if the policy hash is unchanged

2. **`LabelResource()`** - Called BEFORE the backend request to determine:
   - Resource labels (secrecy/integrity requirements)
   - Operation type (read, write, read-write)

3. **`LabelResponse()`** - Called AFTER the backend request to provide:
   - Fine-grained per-item labels (for collections)
   - Or `nil` to use resource labels for entire response

## LabelAgent Details

`LabelAgent()` is the session initialization entry point. It is called by `ensureGuardInitialized()` in the server before any tool call is processed.

### Call Flow

```
Client Request → ensureGuardInitialized()
                    ├── resolveGuardPolicy() → load policy from config
                    ├── Check session cache (skip if already initialized with same policy hash)
                    └── guard.LabelAgent(ctx, policy, backendCaller, caps)
                           ├── Validate & normalize policy
                           └── Return LabelAgentResult {agent labels, difc_mode, normalized_policy}
                    └── Register agent labels in agent registry
```

### Interface

```go
LabelAgent(ctx context.Context, policy interface{}, backend BackendCaller, caps *difc.Capabilities) (*LabelAgentResult, error)
```

### LabelAgentResult

```go
type LabelAgentResult struct {
    Agent            AgentLabelsPayload     `json:"agent"`
    DIFCMode         string                 `json:"difc_mode"`
    NormalizedPolicy map[string]interface{} `json:"normalized_policy,omitempty"`
}

type AgentLabelsPayload struct {
    Secrecy   []string `json:"secrecy"`
    Integrity []string `json:"integrity"`
}
```

| Field | Description |
|-------|-------------|
| `Agent.Secrecy` | Initial secrecy tags for the agent session |
| `Agent.Integrity` | Initial integrity tags for the agent session |
| `DIFCMode` | Enforcement mode: `strict`, `filter`, or `propagate` |
| `NormalizedPolicy` | Policy in normalized form for use by `LabelResource`/`LabelResponse` |

### Session Caching

The server caches `LabelAgent` results per `(sessionID, serverID)` pair. A cached result is reused if the serialized policy JSON matches. This means `LabelAgent` is typically called only once per session, not on every tool call.

### WASM Guards

For WASM guards, the gateway:

1. Normalizes the policy payload (handles both raw JSON and Go map inputs)
2. Validates the policy structure via `buildStrictLabelAgentPayload()`:
   - Requires a top-level `allowonly` key with `repos` and `integrity` fields
   - `repos`: `"all"`, `"public"`, or an array of scoped repo strings
   - `integrity`: one of `"none"`, `"reader"`, `"writer"`, `"merged"`
   - Rejects legacy `policy` envelope keys
3. Calls the WASM module's exported `label_agent` function
4. Parses the response via `parseLabelAgentResponse()`, which validates:
   - No error/failure status in the response
   - `difc_mode` is present and valid

### NoopGuard

The `NoopGuard` returns empty labels and `strict` mode, imposing no restrictions:

```go
return &LabelAgentResult{
    Agent: AgentLabelsPayload{
        Secrecy:   []string{},
        Integrity: []string{},
    },
    DIFCMode: difc.ModeStrict,
}, nil
```

## Supported Response Labeling Formats

The gateway supports multiple formats for `LabelResponse()` return values.

### 1. Nil Response

Return `nil` to use the resource labels from `LabelResource()` for the entire response.

**Use when**: The coarse-grained resource labels are sufficient (single resource or uniform collection).

### 2. Path-Based Labeling (Preferred for Collections)

Apply different labels to specific items in a collection. Return JSON with this structure:

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

**Use when**: Labeling collections where items have different sensitivity levels.

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

### 3. SimpleLabeledData (Go Guards Only)

For native Go guards, return a `SimpleLabeledData` struct to override resource labels:

```go
return &difc.SimpleLabeledData{
    Data:   result,  // The response data
    Labels: &difc.LabeledResource{
        Description: "API response",
        Secrecy:     secrecyLabel,
        Integrity:   integrityLabel,
    },
}, nil
```

**Note**: This format is not available for WASM guards. Use `nil` with appropriate `LabelResource()` labels instead.

## Format Detection (WASM Guards)

For WASM guards, the gateway auto-detects the format based on `LabelResponse()` output:

1. If response contains `labeled_paths` key → Parse as **PathLabeledData**
2. If response contains `items` array → Parse as **CollectionLabeledData** (legacy)
3. Empty or other response → Treat as `nil` (use resource labels)

**Note**: SimpleLabeledData format detection is not currently implemented for WASM guards. Use `nil` response with appropriate `LabelResource()` labels, or use path-based labeling.

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

## Example: GitHub Guard — End-to-End Scoping

This example walks through how an `AllowOnly` policy flows through all three label functions for a GitHub MCP server.

### Policy Schema

The GitHub guard uses an `AllowOnly` policy with two fields:

```json
{
  "allowonly": {
    "repos": "<scope>",
    "integrity": "<level>"
  }
}
```

**`repos`** controls which repositories the agent can access:

| Value | Meaning | Example |
|-------|---------|---------|
| `"all"` | All repos (public + private) the token can see | `"repos": "all"` |
| `"public"` | Only public repos | `"repos": "public"` |
| Array of scopes | Specific repos/owners | `"repos": ["acme/*", "acme/web-app"]` |

Scoped array entries support three patterns (all lowercase):

| Pattern | Meaning | Example |
|---------|---------|---------|
| `owner/*` | All repos under owner | `"acme/*"` |
| `owner/repo` | Exact repo | `"acme/web-app"` |
| `owner/prefix*` | Repos matching prefix | `"acme/api-*"` |

**`integrity`** sets the minimum trust level for content the agent may read:

| Value | Meaning |
|-------|---------|
| `"none"` | No integrity requirements — agent can read anything |
| `"reader"` | Must be from a repo contributor (reader+) |
| `"writer"` | Must be from a repo writer/maintainer |
| `"merged"` | Only merged/reviewed content |

### Step 1: `LabelAgent` — Session Initialization

Given this policy in the gateway config:

```json
{
  "allowonly": {
    "repos": ["acme/web-app", "acme/api-*"],
    "integrity": "writer"
  }
}
```

The gateway calls `label_agent` once at session start. The guard validates the policy and returns:

```json
{
  "agent": {
    "secrecy": [],
    "integrity": []
  },
  "difc_mode": "filter",
  "normalized_policy": {
    "scope_kind": "scoped",
    "scope_values": ["acme/api-*", "acme/web-app"],
    "integrity": "writer"
  }
}
```

Key points:
- **`scope_kind`** is `"scoped"` because the policy uses an explicit repo list (vs. `"all"` or `"public"`)
- **`scope_values`** are sorted and lowercased
- **Agent starts with empty labels** — the guard will restrict access via resource labeling, not agent labels
- **`difc_mode`** is set by the guard (here `"filter"` so unauthorized items are removed rather than blocking the entire response)
- This result is **cached** for the session — subsequent tool calls skip `label_agent`

### Step 2: `LabelResource` — Pre-Request Scoping

When the agent calls a tool like `search_repositories`, the guard determines resource labels and the operation type **before** the backend call.

For `search_repositories(query="org:acme language:go")`:

```json
{
  "resource": {
    "description": "GitHub repository search: org:acme language:go",
    "secrecy": [],
    "integrity": []
  },
  "operation": "read"
}
```

For `get_file_contents(owner="acme", repo="web-app", path="README.md")`:

```json
{
  "resource": {
    "description": "acme/web-app/README.md",
    "secrecy": ["repo:acme/web-app"],
    "integrity": ["github_verified"]
  },
  "operation": "read"
}
```

For `create_issue(owner="acme", repo="web-app", title="Bug")`:

```json
{
  "resource": {
    "description": "acme/web-app issue",
    "secrecy": ["repo:acme/web-app"],
    "integrity": ["github_verified"]
  },
  "operation": "write"
}
```

The Reference Monitor uses these labels to decide whether to proceed:
- **Read**: The backend call executes, then `LabelResponse` provides fine-grained filtering
- **Write**: DIFC rules are checked **before** the call; blocked if agent labels don't satisfy resource labels

### Step 3: `LabelResponse` — Post-Request Fine-Grained Labeling

After a successful read, the guard labels individual items in the response. This is where scoping from the `AllowOnly` policy is enforced at the item level.

For a `search_repositories` response containing repos both inside and outside the allowed scope:

**Backend response:**
```json
{
  "items": [
    {"full_name": "acme/web-app", "private": false},
    {"full_name": "acme/api-server", "private": true},
    {"full_name": "acme/internal-tools", "private": true},
    {"full_name": "other-org/public-lib", "private": false}
  ]
}
```

**Guard returns (path-based labeling):**
```json
{
  "labeled_paths": [
    {
      "path": "/items/0",
      "labels": {
        "description": "acme/web-app",
        "secrecy": ["public"],
        "integrity": ["github_verified"]
      }
    },
    {
      "path": "/items/1",
      "labels": {
        "description": "acme/api-server",
        "secrecy": ["repo_private", "repo:acme/api-server"],
        "integrity": ["github_verified"]
      }
    },
    {
      "path": "/items/2",
      "labels": {
        "description": "acme/internal-tools",
        "secrecy": ["repo_private", "repo:acme/internal-tools"],
        "integrity": ["github_verified"]
      }
    },
    {
      "path": "/items/3",
      "labels": {
        "description": "other-org/public-lib",
        "secrecy": ["public"],
        "integrity": ["github_verified"]
      }
    }
  ],
  "items_path": "/items"
}
```

### Step 4: Reference Monitor Enforcement

The Reference Monitor checks each item's labels against the agent's labels using the DIFC flow rules. With the `"filter"` mode and the scoped policy `["acme/web-app", "acme/api-*"]`:

| Item | Match? | Reason |
|------|--------|--------|
| `acme/web-app` | **Yes** | Exact match on `acme/web-app` |
| `acme/api-server` | **Yes** | Matches prefix pattern `acme/api-*` |
| `acme/internal-tools` | **No** | Not in scope — agent lacks `repo:acme/internal-tools` secrecy tag |
| `other-org/public-lib` | **No** | Not in scope — different org |

**Filtered response returned to agent:**
```json
{
  "items": [
    {"full_name": "acme/web-app", "private": false},
    {"full_name": "acme/api-server", "private": true}
  ]
}
```

### Scoping Summary by `repos` Value

| `repos` value | `scope_kind` | Agent sees |
|---------------|-------------|------------|
| `"all"` | `"all"` | All repos the token can access (public + private) |
| `"public"` | `"public"` | Only public repos |
| `["acme/*"]` | `"scoped"` | All repos under `acme/` |
| `["acme/web-app"]` | `"scoped"` | Only `acme/web-app` |
| `["acme/api-*"]` | `"scoped"` | Repos like `acme/api-server`, `acme/api-client`, etc. |
| `["acme/*", "beta/tools"]` | `"scoped"` | All `acme/` repos + exactly `beta/tools` |

## Filtering Behavior

After `LabelResponse()`, the Reference Monitor applies fine-grained filtering based on the enforcement mode:

1. **Strict mode**: Read requests are blocked at the coarse-grained check (Phase 2) if agent labels don't satisfy resource labels. `LabelResponse()` is not called for blocked requests.

2. **Filter mode**: Coarse-grained check is skipped for reads. After backend call, `LabelResponse()` provides per-item labels, and inaccessible items are filtered out. Agent labels are NOT updated.

3. **Propagate mode**: Same as filter mode, but agent labels are updated to include the labels of data they accessed. This enables information flow tracking.

## Performance Considerations

| Format | Data Copying | Memory | Best For |
|--------|-------------|--------|----------|
| `nil` | None | Minimal | Uniform labels |
| `SimpleLabeledData` | None | Low | Single items or uniform collections |
| `PathLabeledData` | None | Low | **Collections with mixed labels** |

**Recommendation**: Use path-based labeling for collections where items have different sensitivity levels.
