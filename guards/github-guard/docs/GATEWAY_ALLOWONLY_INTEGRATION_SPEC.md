# Gateway Integration Spec: `AllowOnly` Policy + `none` Integrity

## Purpose

This document defines the gateway changes required to support:

- Policy-driven DIFC input filtering via `AllowOnly`
- New required policy field `Integrity`
- New integrity hierarchy with baseline `none`
- Guard-side policy initialization via a new `label_agent` interface

It is written as an implementation checklist for gateway and guard integration.

---

## Feature Summary

### Policy object

Gateway must accept and pass this policy shape:

```json
{
  "AllowOnly": {
    "Repos": "Public",
    "min-integrity": "none"
  }
}
```

`AllowOnly.Repos` supports:
- `"Public"`
- `{ "owner": "<owner>" }`
- `{ "owner": "<owner>", "repo": "<repo>" }`

`AllowOnly.min-integrity` is **required** and must be one of:
- `None`
- `Unapproved`
- `Approved`
- `Merged`

### Integrity order

`Merged > Approveduted > Unapproveduted > none`

For resource/response labels, `none` is always the baseline:
- public scope: `none`
- owner scope: `none:<owner>`
- repo scope: `none:<owner>/<repo>`

Objects with stronger integrity must include `none` in the same scope.

---

## Required Gateway Changes

## 1) Add a `label_agent` guard lifecycle call

Before first guarded tool execution in a session (or whenever policy changes), gateway must call:

- export: `label_agent`
- ABI: same pointer/length pattern as existing guard exports

Call order per session:
1. Resolve effective policy (from config/env/flags)
2. Validate/normalize policy (gateway-side precheck optional, guard validation required)
3. Call `label_agent(policy)`
4. Cache returned normalized policy + mode in gateway session context
5. Continue existing `label_resource` and `label_response` calls

Failure behavior:
- If `label_agent` fails validation: fail closed for guarded session initialization.

## 2) Extend guard interface contract

### `label_agent` input payload

```json
{
  "AllowOnly": {
    "Repos": "Public|{owner[,repo]}",
    "Integrity": "None|Unapproved|Approved|Merged"
  }
}
```

### `label_agent` output payload

```json
{
  "agent": {
    "secrecy": [],
    "integrity": []
  },
  "difc_mode": "strict|filter|propagate",
  "normalized_policy": {
    "scope_kind": "public|owner|repo",
    "integrity": "None|Unapproved|Approved|Merged"
  }
}
```

Gateway must:
- Use returned `difc_mode` as session DIFC mode (unless explicitly overridden by gateway admin policy)
- Persist `normalized_policy` in session state for observability/debugging
- Use returned `agent` labels as effective session labels

## 3) Preserve existing `label_resource` / `label_response` wire format

No breaking wire-format change is required for existing exports. Gateway must continue calling them as today.

Behavioral expectation after `label_agent` adoption:
- `label_resource` and `label_response` should no longer rely on ad hoc session env labels for policy semantics
- Their scoped formatting must align with cached `scope_kind`
- Integrity outputs should never be empty for scoped labeling; baseline should be `none` scoped/unscoped as applicable

---

## Policy Input Surfaces (Gateway)

Gateway should support `AllowOnly` policy input from config, environment, and CLI.

Precedence (recommended):
1. CLI flags
2. Environment variables
3. Static config file
4. Legacy session label defaults (only if no policy provided)

## A) Config file additions

Add optional top-level guard policy section:

```json
{
  "guards": {
    "github-guard": {
      "type": "wasm",
      "path": "./github-guard-rust.wasm",
      "policy": {
        "AllowOnly": {
          "Repos": { "owner": "lpcox", "repo": "github-guard" },
            "min-integrity": "unapproved"
        }
      }
    }
  }
}
```

## B) Environment variables

Add the following environment variables:

- `MCP_GATEWAY_GUARD_POLICY_JSON`
  - Full JSON object for policy input
  - Example:
    - `{"AllowOnly":{"Repos":"Public","min-integrity": "none"}}`

Optional convenience env vars (gateway builds policy if JSON var not set):
- `MCP_GATEWAY_ALLOW_ONLY_SCOPE_PUBLIC=1`
- `MCP_GATEWAY_ALLOW_ONLY_SCOPE_OWNER=<owner>`
- `MCP_GATEWAY_ALLOW_ONLY_SCOPE_REPO=<repo>` (repo name only; requires owner)
- `MCP_GATEWAY_ALLOW_ONLY_MIN_INTEGRITY=None|Unapproved|Approved|Merged`

Validation rules:
- Exactly one scope variant must be selected:
  - public, or owner, or owner+repo
- `SCOPE_REPO` requires `SCOPE_OWNER`
- `Integrity` required in all cases

## C) CLI flags

Add CLI flags with 1:1 semantics to env vars:

- `--guard-policy-json <json>`
- `--allow-only-scope-public`
- `--allow-only-scope-owner <owner>`
- `--allow-only-scope-repo <repo>`
- `--allow-only-min-integrity <value>`

CLI validation mirrors env/config validation.

---

## Backward Compatibility Plan

## Legacy mode support

If no `AllowOnly` policy is provided:
- Gateway may continue legacy behavior using existing session labels/env
- Emit warning log: policy not configured; legacy DIFC session labels in use

## Migration recommendation

1. Introduce policy plumbing + `label_agent` behind a feature flag
2. Run dual-mode telemetry (legacy labels + policy-derived labels comparison)
3. Enable policy path by default after parity confidence
4. Deprecate direct `MCP_GATEWAY_SESSION_INTEGRITY`/`MCP_GATEWAY_SESSION_SECRECY` for this guard workflow

---

## Validation Requirements

Gateway-side validation should reject invalid policy before guard call when possible.

Minimum checks:
- `AllowOnly` exists
- `Repos` matches allowed shape
- owner/repo naming rules (as documented in policy spec)
- `Integrity` exists and in enum

Guard remains source of truth for final validation.

---

## Observability and Debugging

Gateway logs should include:
- Effective policy source (config/env/CLI)
- Normalized policy from `label_agent`
- Chosen DIFC mode
- Rejection reason for invalid policy

Recommended structured log fields:
- `guard_policy.scope_kind`
- `guard_policy.scope_owner`
- `guard_policy.scope_repo`
- `guard_policy.min_integrity`
- `guard_policy.source`

---

## Test Matrix (Gateway)

Minimum integration tests:

1. `Repos="Public"`, `Integrity=None`
   - public data allowed
  - baseline integrity contains `none`
2. `Repos={owner}`, `Integrity=Unapproved`
   - owner-scoped secrecy/integrity formatting
  - none baseline scoped as `none:<owner>`
3. `Repos={owner,repo}`, `Integrity=Approved`
   - repo-scoped secrecy/integrity formatting
  - baseline `none:<owner>/<repo>`
4. invalid policy payloads
  - missing `Integrity`
   - repo without owner
   - invalid enum values
5. compatibility path
   - no policy configured falls back to legacy session labels

---

## PR Breakdown (Recommended)

This sequence minimizes risk while keeping each PR independently reviewable.

### PR 1: Schema and docs lock

Scope:
- Finalize `AllowOnly.Repos` + `AllowOnly.min-integrity` naming and examples.
- Freeze gateway-facing contract text.

Files (this repo):
- `docs/AGENTIC_WORKFLOW_POLICY_FRONTMATTER.md`
- `docs/LABEL_AGENT_IMPLEMENTATION_PLAN.md`
- `docs/GATEWAY_ALLOW_ONLY_INTEGRATION_SPEC.md`

Acceptance criteria:
- All docs consistently use `Integrity`.
- Policy examples validate against documented schema.
- No contradictory references to legacy naming.

### PR 2: Guard policy model + `label_agent` API

Scope:
- Add typed policy structs/enums and strict validation.
- Add new guard export `label_agent` with response contract.

Files (guard):
- `rust-guard/src/lib.rs`
- `rust-guard/src/labels/helpers.rs` (or new `rust-guard/src/policy.rs`)
- `rust-guard/src/labels/constants.rs` (if new integrity constant added)

Acceptance criteria:
- `label_agent` accepts valid `AllowOnly` policy and rejects invalid payloads.
- Output includes `agent`, `difc_mode`, and `normalized_policy`.
- Unit tests cover schema acceptance/rejection paths.

### PR 3: none hierarchy in labeling

Scope:
- Implement integrity expansion: `Merged > Approveduted > Unapproveduted > none`.
- Ensure baseline `none` is always emitted (scoped/unscoped by `scope_kind`).

Files (guard):
- `rust-guard/src/labels/helpers.rs`
- `rust-guard/src/labels/tool_rules.rs`
- `rust-guard/src/labels/response_items.rs`
- `rust-guard/src/labels/response_paths.rs`
- `rust-guard/src/lib.rs`

Acceptance criteria:
- No scoped label output uses empty integrity where `none` should apply.
- Stronger integrity labels always include matching-scope `none`.
- Existing labeling behavior remains otherwise stable.

### PR 4: Gateway policy ingestion (config/env/flags)

Scope:
- Add policy input parsing from config, env, and CLI with precedence.
- Build effective `AllowOnly` policy object for session init.

Files (gateway repo):
- config schema definitions
- env/flag parser
- session/bootstrap config resolution path

Acceptance criteria:
- `--guard-policy-json`/`MCP_GATEWAY_GUARD_POLICY_JSON` work end-to-end.
- Convenience scope/min-integrity flags/env vars map to same normalized policy.
- Invalid combinations fail with actionable errors.

### PR 5: Gateway-guard session initialization flow

Scope:
- Invoke `label_agent` once per session (or on policy change).
- Persist `normalized_policy` and use returned mode/session labels.

Files (gateway repo):
- wasm guard adapter / bridge
- DIFC session initialization path
- logging/telemetry path

Acceptance criteria:
- Guard receives policy before first tool labeling call.
- Gateway stores and logs normalized policy fields.
- Failure in `label_agent` fails closed when policy mode is enabled.

### PR 6: Compatibility fallback + migration controls

Scope:
- Keep legacy env label path behind explicit fallback behavior.
- Add migration warnings and toggles.

Files (gateway repo):
- session mode selection logic
- runtime config/feature-flag definitions

Acceptance criteria:
- No-policy sessions continue to run via legacy path.
- Policy-enabled sessions prioritize `label_agent` path.
- Warning logs emitted when using legacy path.

### PR 7: Integration tests and rollout hardening

Scope:
- Add policy-mode integration tests and regression coverage.
- Add telemetry assertions for mode/policy resolution.

Files (gateway + guard test suites):
- gateway integration tests
- guard unit/integration tests
- CI workflow/test scripts

Acceptance criteria:
- Test matrix in this doc is automated.
- Public/owner/repo scope behavior validated.
- Invalid policy cases fail deterministically.

---

## Files to Update in Gateway (Typical)

- guard adapter / wasm bridge (`LabelResource`, `LabelResponse`, new `LabelAgent`)
- gateway config schema definitions
- env/flag parsing layer
- session initialization path (policy resolution + guard init)
- DIFC mode/session label initialization logic
- integration tests and docs

---

## Cross-Reference

- Policy schema and semantics: `docs/AGENTIC_WORKFLOW_POLICY_FRONTMATTER.md`
- Guard implementation plan: `docs/LABEL_AGENT_IMPLEMENTATION_PLAN.md`
- Existing response interface details: `docs/GATEWAY_FIX.md`
