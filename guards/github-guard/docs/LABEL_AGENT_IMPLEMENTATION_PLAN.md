# `label_agent` Implementation Plan

## Objective

Add a new exported guard function, `label_agent`, that the gateway calls before tool/resource labeling.

`label_agent` will:

1. Parse a policy JSON payload.
2. Validate the policy against the strict frontmatter schema.
3. Derive and return:
   - initial agent secrecy labels,
   - initial agent integrity labels,
   - DIFC mode (`strict` | `filter` | `propagate`).

This allows agent policy to be centrally translated into concrete DIFC session state.

Important runtime simplification:
- For `label_resource` and `label_response`, the guard only needs cached `scope_kind`.
- Owner/repo suffixes are derived from each request/response object being labeled (not from cached scope value).

---

## Scope

In scope:
- New WASM export and request/response structs.
- Policy validation and normalization.
- Deterministic mapping from policy -> session labels + mode.
- Caching normalized agent scope in guard state.
- Scope-aware adaptation of `label_resource` and `label_response` labeling.
- Unit tests for schema validation and mapping.
- Documentation updates.

Out of scope (phase 1):
- Multi-policy composition or per-tool overrides.
- Backward-incompatible gateway behavior changes.

---

## API Contract

## Exported Function

Add a function mirroring existing export style:

- Name: `label_agent`
- ABI: `extern "C"` with `(input_ptr, input_len, output_ptr, output_size) -> i32`
- Return: byte length on success, `-1` on hard error (consistent with `label_resource`), optionally `0` for soft/skip depending on gateway expectation.

## Request Payload (JSON)

Input JSON should contain a policy object conforming to the strict schema already documented in [docs/AGENTIC_WORKFLOW_POLICY_FRONTMATTER.md](docs/AGENTIC_WORKFLOW_POLICY_FRONTMATTER.md).

Expected logical shape:

```json
{
  "AllowOnly": {
    "Repos": "Public",
    "min-integrity": "none"
  }
}
```

`Repos` may also be `{ "owner": "foo" }` or `{ "owner": "foo", "repo": "bar" }`.
Here, `owner` must conform to GitHub owner naming rules, and `repo` must be repository name only (no `/`).

Constraint:
- `Integrity` is required for all policies.

## Response Payload (JSON)

Proposed response shape:

```json
{
  "agent": {
    "secrecy": [],
    "integrity": []
  },
  "difc_mode": "strict",
  "normalized_policy": {
    "scope_kind": "public|owner|repo",
    "integrity": "None|Unapproved|Approved|Merged"
  }
}
```

Notes:
- `normalized_policy` is optional but strongly recommended for observability/debugging.
- `integrity` is always present because `AllowOnly.min-integrity` is required.
- `scope_value` (owner/repo literal from policy) may be retained for observability, but is not required by runtime labeling.

---

## Validation Strategy

## Structural validation

Implement strict validation equivalent to the JSON Schema:
- top-level must contain `AllowOnly`.
- no unknown properties.
- `AllowOnly.Repos` one of:
  - `"Public"`,
  - `{ "owner": "<owner>" }`,
  - `{ "owner": "<owner>", "repo": "<repo>" }`.
- `AllowOnly.min-integrity` enum: `None|Unapproved|Approved|Merged`.
- `Integrity` required for all policies.

## Semantic validation

- `owner` must conform to GitHub owner naming rules and satisfy all of:
  - may contain alphanumeric characters and single hyphens (`-`)
  - cannot contain consecutive hyphens (`--`)
  - cannot begin or end with `-`
  - max length is 39 characters
  - cannot contain `/`
- `repo` must conform to GitHub repository-name rules, be valid only when `owner` is present, and satisfy all of:
  - may contain alphanumeric characters, `-`, `_`, `.`
  - cannot contain spaces
  - cannot begin or end with `.`
  - cannot contain consecutive periods (`..`)
  - max length is 100 characters
  - cannot contain `/`
- For canonical valid/invalid owner and repo examples, see `Owner name examples` and `Repository name examples` in `docs/AGENTIC_WORKFLOW_POLICY_FRONTMATTER.md`.
- normalize casing and whitespace conservatively (trim only; do not silently rewrite invalid values).

## Error handling

Return machine-readable validation errors, e.g.:

```json
{
  "error": {
    "code": "invalid_policy",
    "message": "AllowOnly.Repos.repo requires AllowOnly.Repos.owner",
    "field": "AllowOnly.Repos.repo"
  }
}
```

---

## Policy-to-Label Mapping

## 0) Initial agent label mapping

`label_agent` returns initial secrecy labels based on scope:

- `Repos = "Public"` => `agent.secrecy = []`
- `Repos = {"owner":"foo"}` => `agent.secrecy = ["private:foo"]`
- `Repos = {"owner":"foo", "repo":"bar"}` => `agent.secrecy = ["private:foo/bar"]`

This keeps agent label scope aligned with runtime resource/response label scope.

## 1) Secrecy mapping (runtime object labeling)

Given an object in repository `<owner>/<repo>`:

- If the repository is **public**: secrecy is always `[]` for all `scope_kind` values.
- If the repository is **private**:
  - `scope_kind = public` => `["private"]`
  - `scope_kind = owner` => `["private:<owner>"]`
  - `scope_kind = repo` => `["private:<owner>/<repo>"]`

## 1b) Cache normalized scope

After successful validation/mapping in `label_agent`, cache:

- `scope_kind`: `public|owner|repo`
- `integrity`: `None|Unapproved|Approved|Merged`
- `resolved_mode`: `strict|filter|propagate`

The cache is used by subsequent `label_resource` and `label_response` calls.

## 2) Integrity mapping (runtime object labeling)

The guard still computes object integrity level using existing rules (author association, merged/default-branch, PR lineage, etc.).

Let computed level be one of:
- level 1 => `none`
- level 2 => `Unapproveduted`
- level 3 => `Approveduted`
- level 4 => `Merged`

Runtime integrity label expansion always includes all lower levels up to level `n`:

- `level_n` => `{level_1, ..., level_n}`

Order: `Merged > Approveduted > Unapproveduted > none`.

Scope formatting is determined only by `scope_kind`:

- `scope_kind = public` => unscoped labels
  - e.g. none-only => `["none"]`
  - e.g. merged => `["none", "Unapproveduted", "Approveduted", "Merged"]`
- `scope_kind = owner` => owner-scoped labels using object owner
  - e.g. none-only in `foo/bar` => `["none:foo"]`
  - e.g. merged in `foo/bar` => `["none:foo", "Unapproveduted:foo", "Approveduted:foo", "Merged:foo"]`
- `scope_kind = repo` => repo-scoped labels using object repo
  - e.g. none-only in `foo/bar` => `["none:foo/bar"]`
  - e.g. merged in `foo/bar` => `["none:foo/bar", "Unapproveduted:foo/bar", "Approveduted:foo/bar", "Merged:foo/bar"]`

If no stronger integrity can be derived for a resource/response object, the guard emits:
- `["none"]` for `scope_kind = public`
- `["none:<owner>"]` for `scope_kind = owner`
- `["none:<owner>/<repo>"]` for `scope_kind = repo`

---

## Policy-to-Mode Mapping

Need deterministic mode selection because gateway requires one of `strict|filter|propagate`.

## Recommended phase-1 rule

- `Repos = "Public"` and `Integrity = "None"` => `filter` (public-only equivalent).
- All other valid policies => `strict` by default.
- `propagate` reserved for explicit future policy extension.

There is no `private-only` scope option in phase 1.

This avoids surprising behavior and preserves current semantics.

## Recommended phase-2 extension

Add optional explicit field:

```json
{ "Mode": "strict|filter|propagate" }
```

Validation rule:
- If `Mode` present, use it.
- If absent, apply phase-1 inference rule above.

## Named policy examples

### Organization-only mode (example: Microsoft merged-only)

```json
{
  "AllowOnly": {
    "Repos": { "owner": "microsoft" },
    "min-integrity": "merged"
  }
}
```

Planned `label_agent` result:
- `agent.secrecy = ["private:microsoft"]`
- `agent.integrity = ["none:microsoft", "Unapproveduted:microsoft", "Approveduted:microsoft", "Merged:microsoft"]`
- `difc_mode = "strict"`

---

## Code Changes

## `rust-guard/src/lib.rs`

- Add input/output structs for `label_agent`.
- Add exported `label_agent` function.
- Add global in-process cache for normalized policy context (Mutex/RwLock around a small struct).
- Reuse existing logging and JSON serialization patterns.
- On parse/validation failure in `label_resource`/`label_response`, behavior remains fail-secure.

## Scope-aware adaptation in existing exports

- `label_resource`:
  - Read cached `scope_kind`.
  - Compute object owner/repo context from tool args.
  - Emit secrecy labels at selected scope granularity:
    - if target repo public => `[]`
    - if target repo private and `scope_kind = public` => `private`
    - if target repo private and `scope_kind = owner` => `private:<owner>`
    - if target repo private and `scope_kind = repo` => `private:<owner>/<repo>`
  - Emit integrity tags using selected scope granularity (unscoped for `public`, owner-scoped for `owner`, repo-scoped for `repo`).
  - Never emit empty integrity labels for scoped labeling; emit at least `none` (`none`, `none:<owner>`, or `none:<owner>/<repo>`).

- `label_response`:
  - Read cached `scope_kind`.
  - For each item, extract owner/repo context from the item payload (or fallback tool args).
  - Apply the same secrecy/integrity scope formatting as above per item.
  - When no stronger integrity can be derived, emit the scoped `none` tag instead of `[]`.
  - Keep per-item refinement logic (merged/default-branch/author-association), but encode labels by cached `scope_kind`.

## `rust-guard/src/labels/helpers.rs` (or new module)

- Add policy normalization helpers:
  - parse scope kind,
  - parse Integrity enum,
  - expand integrity labels by computed level + scope_kind + object context.
- Add scope-aware formatting helpers:
  - secrecy tag formatter by scope kind + object visibility/context,
  - integrity tag formatter by scope kind + object context.

## Optional new file: `rust-guard/src/policy.rs`

To keep `lib.rs` small:
- request validation,
- mapping logic,
- cache model + getters/setters,
- typed error definitions.

---

## Testing Plan

## Unit tests

1. Schema acceptance tests
  - valid `"Public"`, `owner`, `owner+repo` policies.
  - policies rejected without `Integrity`.
2. Schema rejection tests
   - unknown keys,
   - invalid owner/repo formats,
   - invalid integrity enum.
3. Mapping tests
   - secrecy/integrity outputs for each scope + integrity level.
4. Cache tests
  - `label_agent` stores normalized `scope_kind`/mode.
  - subsequent labeling calls read cached values.
  - cache replacement works when `label_agent` is called again.
5. Mode tests
  - `"Public"` + `Integrity=None` => `filter`.
  - repo policy + `Integrity` above `None` => `strict`.
6. Error payload tests
   - field path and code correctness.
7. Scope-adaptation tests
  - for public repo objects: secrecy is always `[]` across all scope kinds.
  - for private repo objects: secrecy matches `private` vs `private:<owner>` vs `private:<owner>/<repo>` based on scope kind.
  - none baseline is always emitted (scoped/unscoped per `scope_kind`).
  - stronger integrity labels include none for the same scope.

## Integration tests

- Gateway invokes `label_agent` and applies returned labels/mode.
- Gateway invokes `label_agent`; guard caches scope and uses it in subsequent labeling calls.
- Verify equivalence:
  - public-only behavior,
  - repo-only behavior.

---

## Rollout Plan

1. Implement `label_agent` behind a gateway feature flag.
2. Keep existing env-based session label path as fallback.
3. Add logging for inferred mode and normalized policy.
4. Enable in test runner for one mode first (`public-only`), then `repo-only`.
5. Remove fallback only after parity is proven.

---

## Open Decisions

1. Should `Mode` be inferred only, or explicitly settable in policy phase 1?
2. Should unscoped integrity tags (`Unapproveduted`) be allowed long term, or should all integrity labels require explicit scope?
3. On validation failure, should gateway fail closed (deny all) or fail open (ignore policy)?

Recommended defaults:
- explicit deny/fail-closed,
- infer mode in phase 1,
- add explicit `Mode` in phase 2.
