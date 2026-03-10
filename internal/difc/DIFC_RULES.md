# Decentralized Information Flow Control (DIFC) Rules

This document explains the DIFC labeling system used in this package. All implementations and tests MUST follow these rules.

## Overview

DIFC uses two types of labels to control information flow:

- **Secrecy Labels**: Control who can read confidential information
- **Integrity Labels**: Control who can modify trusted resources

Both agents and resources have secrecy and integrity labels. Labels are sets of tags (strings).

## Core Rules

### Notation

- `A.secrecy` = Agent's secrecy label (set of tags)
- `A.integrity` = Agent's integrity label (set of tags)
- `R.secrecy` = Resource's secrecy label (set of tags)
- `R.integrity` = Resource's integrity label (set of tags)
- `⊇` means "is a superset of" (contains all elements of)

### Read Access Rules

For an agent to **READ** a resource:

1. **Secrecy Check**: `A.secrecy ⊇ R.secrecy`
   - Agent must have clearance for all secrecy tags on the resource
   - *Example*: To read a `{secret, confidential}` document, agent must have at least `{secret, confidential}` in its secrecy label

2. **Integrity Check**: `R.integrity ⊇ A.integrity`
   - Resource must be at least as trustworthy as the agent requires
   - *Example*: If agent requires `{verified}` integrity, resource must have at least `{verified}`

### Write Access Rules

For an agent to **WRITE** to a resource:

1. **Secrecy Check**: `R.secrecy ⊇ A.secrecy`
   - Resource must accept all agent's secrecy tags (no information leak)
   - *Example*: Agent with `{secret}` cannot write to a `{}` (public) resource

2. **Integrity Check**: `A.integrity ⊇ R.integrity`
   - Agent must be at least as trustworthy as resource requires
   - *Example*: To write to a `{production}` resource, agent must have at least `{production}` integrity

### Read-Write Access

For read-write operations, BOTH read AND write rules must be satisfied.

## Key Examples

### Example 1: Secret Agent Cannot Write to Public Resource

```
Agent:    secrecy={secret}, integrity={}
Resource: secrecy={}, integrity={}

Write Check:
  Secrecy: R.secrecy ⊇ A.secrecy → {} ⊇ {secret} → FALSE
  Result: DENIED (would leak secret information to public)
```

### Example 2: High-Integrity Agent Cannot Read Low-Integrity Resource

```
Agent:    secrecy={}, integrity={trusted, verified}
Resource: secrecy={}, integrity={}

Read Check:
  Integrity: R.integrity ⊇ A.integrity → {} ⊇ {trusted, verified} → FALSE
  Result: DENIED (resource is not trustworthy enough for agent)
```

### Example 3: Successful Read of Secret Document

```
Agent:    secrecy={secret, confidential}, integrity={}
Resource: secrecy={secret}, integrity={}

Read Check:
  Secrecy: A.secrecy ⊇ R.secrecy → {secret, confidential} ⊇ {secret} → TRUE
  Integrity: R.integrity ⊇ A.integrity → {} ⊇ {} → TRUE
  Result: ALLOWED
```

### Example 4: Successful Write to Production Database

```
Agent:    secrecy={}, integrity={production, verified}
Resource: secrecy={}, integrity={production}

Write Check:
  Secrecy: R.secrecy ⊇ A.secrecy → {} ⊇ {} → TRUE
  Integrity: A.integrity ⊇ R.integrity → {production, verified} ⊇ {production} → TRUE
  Result: ALLOWED
```

## Public Internet Analogy

The **public internet** has empty labels: `secrecy={}, integrity={}`.

- An agent with `secrecy={secret}` **CANNOT write** to the public internet
  - Because: `{} ⊇ {secret}` is FALSE (would leak secrets)

- An agent with `integrity={trusted}` **CANNOT read** from the public internet
  - Because: `{} ⊇ {trusted}` is FALSE (source not trusted enough)

## Enforcement Modes

The DIFC evaluator supports three **mutually exclusive** enforcement modes. You can only enable ONE mode at a time.

### CLI Configuration

Use the `--guards-mode` flag to specify the enforcement mode:

```bash
# Guards are auto-enabled when a policy is detected
./awmg --guards-mode strict

# Use filter mode
./awmg --guards-mode filter

# Use propagate mode
./awmg --guards-mode propagate
```

### CLI Flags Reference

| Flag | Description | Default |
|------|-------------|---------|
| `--guards-mode` | Enforcement mode: `strict`, `filter`, or `propagate` | `strict` |

**Note:** Guards are automatically enabled when an allow-only policy is detected in the configuration. No explicit enable flag is needed.

### Environment Variables Reference

| Environment Variable | Description | Equivalent Flag |
|---------------------|-------------|-----------------|
| `MCP_GATEWAY_GUARDS_MODE` | Enforcement mode: `strict`, `filter`, `propagate` | `--guards-mode` |

**Example:**
```bash
export MCP_GATEWAY_GUARDS_MODE=filter

./awmg --config-stdin < config.json
```

### TOML Configuration

```toml
[gateway]
guards_mode = "propagate"  # strict, filter, or propagate
```

### JSON Configuration

The JSON config schema supports guard and policy fields:

```json
{
  "mcpServers": {
    "github": {
      "type": "stdio",
      "container": "ghcr.io/github/github-mcp-server:latest",
      "guard": "github-guard",
      "guard-policies": {
        "allow-only": {
          "repos": ["owner/repo"],
          "min-integrity": "none"
        }
      }
    },
    "filesystem": {
      "type": "stdio", 
      "container": "mcp/filesystem:latest",
      "guard": "fs-guard"
    }
  },
  "guards": {
    "github-guard": {
      "type": "wasm",
      "path": "/guards/github.wasm",
      "config": {}
    },
    "fs-guard": {
      "type": "noop"
    }
  },
  "gateway": {
    "port": 3000,
    "domain": "localhost",
    "apiKey": "your-api-key"
  }
}
```

**Extension Fields:**
- `mcpServers.<server>.guard` - Name of the guard to use for this server
- `mcpServers.<server>.guard-policies` - Per-server allow-only policies
- `guards` - Map of guard name to guard configuration
- `guards.<name>.type` - Guard type (`wasm`, `noop`, etc.)
- `guards.<name>.path` - Path to guard implementation (for `wasm` type)
- `guards.<name>.config` - Guard-specific configuration object

### Mode Configuration

**Important**: Filter and propagate modes are mutually exclusive. Attempting to use both will result in an error:
```
Error: invalid --guards-mode flag: invalid guards mode "both": must be one of: strict, filter, propagate
```

The DIFC evaluator supports three enforcement modes:

### Strict Mode (default)

In **strict** mode, any access that would violate DIFC rules is blocked. This provides the strongest security guarantees.

### Filter Mode

In **filter** mode:
- **Reads**: Allowed, but inaccessible items are filtered out of collections
- **Writes**: Blocked if they violate DIFC rules (same as strict mode)

### Propagate Mode

In **propagate** mode:
- **Reads**: Always allowed, but the agent's labels are automatically adjusted:
  - If the agent reads a resource with secrecy tags not in the agent's secrecy label, those missing tags are **added** to the agent's secrecy label (agent becomes "tainted" with secret data)
  - If the agent reads a resource missing integrity tags that the agent has, those missing tags are **removed** from the agent's integrity label (agent is "influenced" by untrusted data)
- **Writes**: Blocked if they violate DIFC rules (same as strict mode)

**Key point**: Propagate mode has NO effect on writes. Write violations are always blocked.

#### Propagate Mode Examples

**Example 1: Reading Secret Data (Secrecy Propagation)**
```
Before:
  Agent:    secrecy={}, integrity={}
  Resource: secrecy={secret}, integrity={}

Read Check (propagate mode):
  Secrecy: A.secrecy ⊇ R.secrecy → {} ⊇ {secret} → FALSE
  Action: Add {secret} to agent's secrecy label
  Result: ALLOWED (with propagation)

After:
  Agent:    secrecy={secret}, integrity={}
  
Implication: Agent can no longer write to public resources (secrecy leak protection)
```

**Example 2: Reading Untrusted Data (Integrity Propagation)**
```
Before:
  Agent:    secrecy={}, integrity={trusted, verified}
  Resource: secrecy={}, integrity={}

Read Check (propagate mode):
  Integrity: R.integrity ⊇ A.integrity → {} ⊇ {trusted, verified} → FALSE
  Action: Remove {trusted, verified} from agent's integrity label
  Result: ALLOWED (with propagation)

After:
  Agent:    secrecy={}, integrity={}
  
Implication: Agent can no longer write to high-integrity resources
```

**Example 3: Write Still Blocked in Propagate Mode**
```
Agent:    secrecy={secret}, integrity={}
Resource: secrecy={}, integrity={}

Write Check (propagate mode):
  Secrecy: R.secrecy ⊇ A.secrecy → {} ⊇ {secret} → FALSE
  Result: DENIED (propagate mode does not affect writes)
```

## Implementation Notes

### CheckFlow Function

The `CheckFlow(target)` method checks if `source ⊆ target` (source has no tags that target doesn't have):

```go
// SecrecyLabel.CheckFlow(target) returns true if all tags in source are also in target
// i.e., source ⊆ target (source is a subset of target)
func (source *SecrecyLabel) CheckFlow(target *SecrecyLabel) (bool, []Tag)

// IntegrityLabel.CheckFlow(target) returns true if all tags in source are also in target
// i.e., source ⊆ target (source is a subset of target)
func (source *IntegrityLabel) CheckFlow(target *IntegrityLabel) (bool, []Tag)
```

**CRITICAL**: To check `A ⊇ B` (A contains all of B), call `B.CheckFlow(A)`.

### Evaluator Functions

The evaluator uses these `CheckFlow` calls to implement the DIFC rules:

```go
// For READ access:
//   Secrecy:   A.secrecy ⊇ R.secrecy   → resource.Secrecy.CheckFlow(agentSecrecy)
//   Integrity: R.integrity ⊇ A.integrity → resource.Integrity.CheckFlow(agentIntegrity)

// For WRITE access:
//   Secrecy:   R.secrecy ⊇ A.secrecy   → agentSecrecy.CheckFlow(&resource.Secrecy)
//   Integrity: A.integrity ⊇ R.integrity → agentIntegrity.CheckFlow(&resource.Integrity)
```

**Remember**: `X.CheckFlow(Y)` returns true when `X ⊆ Y` (all tags in X are in Y).
So to check `A ⊇ B`, call `B.CheckFlow(A)`.

### Using Propagate Mode

```go
// Create evaluator with propagate mode
evaluator := difc.NewEvaluatorWithMode(difc.EnforcementPropagate)

// Evaluate read access
result := evaluator.Evaluate(agentSecrecy, agentIntegrity, resource, difc.OperationRead)

if result.IsAllowed() {
    // Apply label propagation if needed
    if result.RequiresPropagation() {
        agentLabels.ApplyPropagation(result)
        // Agent's labels have been updated
    }
    // Proceed with read
}
```

## Testing Guidelines

When writing tests:

1. Empty labels `{}` represent public/untrusted resources
2. To test secrecy violations, give the agent secrecy tags the resource lacks
3. To test integrity violations, give the agent integrity tags the resource lacks
4. For reads: agent needs clearance (secrecy), resource needs trust (integrity)
5. For writes: resource needs to accept secrets (secrecy), agent needs trust (integrity)
6. For propagate mode: verify that labels are correctly added/removed after reads
