# Agentic Workflow Frontmatter Policy (Proposed)

This document summarizes a proposed frontmatter policy format for limiting which GitHub content is exposed to an agent.

## Goal

Provide a small, explicit policy surface in workflow frontmatter that can express:

- **Scope filtering** (what repository visibility/scope is allowed)
- **Integrity floor** (minimum trust level for input content)

Both controls are used to filter inputs before the agent consumes content.

---

## Proposed Policy Shape (JSON)

```json
{
  "AllowOnly": {
    "Repos": "Public",
    "min-integrity": "none"
  }
}
```

### Field semantics

- `AllowOnly.Repos` (optional)
  - `"Public"`: allow only public-scope inputs (must be the only scope entry)
  - `{ "owner": "<string>" }`: scope to public or private content owned by a GitHub owner (org/user); `owner` must conform to GitHub owner naming rules
  - `{ "owner": "<string>", "repo": "<string>" }`: scope to a specific repository under that owner; `repo` must be repo name only (no slash)

- `AllowOnly.min-integrity` (required)
  - `None`
  - `Unapproved`
  - `Approved`
  - `Merged`

Rules:
- `Integrity` is required for all policies.

---

## Formal JSON Schema (Strict)

Use this schema to validate policy frontmatter strictly.

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://example.local/schemas/agentic-workflow-policy.schema.json",
  "title": "Agentic Workflow AllowOnly Policy",
  "type": "object",
  "additionalProperties": false,
  "required": ["AllowOnly"],
  "properties": {
    "AllowOnly": {
      "type": "object",
      "additionalProperties": false,
      "minProperties": 1,
      "properties": {
        "Repos": {
          "oneOf": [
            {
              "type": "string",
              "enum": ["Public"]
            },
            {
              "type": "object",
              "additionalProperties": false,
              "required": ["owner"],
              "properties": {
                "owner": {
                  "type": "string",
                  "minLength": 1,
                  "maxLength": 39,
                  "pattern": "^(?!.*--)[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$"
                },
                "repo": {
                  "type": "string",
                  "minLength": 1,
                  "maxLength": 100,
                  "pattern": "^(?!\\.)(?!.*\\.\\.)(?!.*\\.$)[A-Za-z0-9._-]{1,100}$"
                }
              }
            }
          ]
        },
        "Integrity": {
          "type": "string",
          "enum": ["None", "Unapproved", "Approved", "Merged"]
        }
      }
      ,
      "required": ["Integrity"]
    }
  }
}
```

### Strictness properties

- Closed objects everywhere (`additionalProperties: false`).
- `AllowOnly` is required at the top level.
- `AllowOnly` must contain at least one supported key (`minProperties: 1`).
- `Repos` accepts exactly one variant (`"Public"` or `{owner[, repo]}`).
- `owner` and `repo` (repo name only) are pattern-validated.
- `Integrity` is restricted to an enum (`None|Unapproved|Approved|Merged`).
- `Integrity` is required.

### Additional validation notes

- `Repos` and `Integrity` are used together (`Integrity` is always required).
- `Repos` string form must be exactly `Public`.
- `repo` can only appear with `owner`.
- `owner` must conform to GitHub owner naming rules:
  - may contain alphanumeric characters and single hyphens (`-`)
  - cannot contain consecutive hyphens (`--`)
  - cannot begin or end with `-`
  - max length is 39 characters
  - cannot contain `/`
- `repo` must conform to GitHub repository-name rules:
  - may contain alphanumeric characters, `-`, `_`, `.`
  - cannot contain spaces
  - cannot begin or end with `.`
  - cannot contain consecutive periods (`..`)
  - max length is 100 characters
  - cannot contain `/`

### Owner name examples

Valid:
- `lpcox`
- `microsoft`
- `my-org`

Invalid:
- `-org` (starts with `-`)
- `org-` (ends with `-`)
- `my--org` (contains consecutive hyphens)
- `my org` (contains space)
- `my/org` (contains `/`)

### Repository name examples

Valid:
- `github-guard`
- `my_repo.v2`
- `a.b-c_d`

Invalid:
- `.repo` (starts with `.`)
- `repo.` (ends with `.`)
- `my..repo` (contains consecutive periods)
- `my repo` (contains space)
- `owner/repo` (contains `/`)

---

## Mode equivalence examples

### Public-only equivalent

```json
{
  "AllowOnly": {
    "Repos": "Public",
    "min-integrity": "none"
  }
}
```

Interpretation:
- Equivalent to current **public-only** behavior.
- Uses `none` as the baseline integrity floor.

### Repo-only equivalent (for `lpcox/github-guard`)

```json
{
  "AllowOnly": {
    "Repos": { "owner": "lpcox", "repo": "github-guard" },
    "min-integrity": "unapproved"
  }
}
```

Interpretation:
- Equivalent to current **repo-only** intent for scoped repository reads.
- Requires at least Unapproveduted trust.

### Organization-only mode (example: Microsoft merged-only)

```json
{
  "AllowOnly": {
    "Repos": { "owner": "microsoft" },
    "min-integrity": "merged"
  }
}
```

Interpretation:
- Restricts agent-visible content to owner scope `microsoft`.
- Requires merged-level trust for objects to pass.
- Commonly used for high-trust organization-only workflows.

---

## Assumed runtime model

Assumption: the selected policy scope is passed to the guard on each label request.

The guard then emits secrecy/integrity labels at that scope granularity.

---

## Secrecy labeling by scope

For all scopes, public repository data remains `[]` secrecy.

### `Repos: "Public"`
- Public repo object => `[]`
- Private repo object => `[]` (resource excluded by scope policy)

### `Repos: {"owner":"foo"}`
- Public repo object => `[]`
- Private repo object owned by `foo` => `["private:foo"]`

### `Repos: {"owner":"foo", "repo":"bar"}`
- Public repo object => `[]`
- Private repo `foo/bar` => `["private:foo/bar"]`

---

## Integrity labeling by scope

Integrity level expansion remains hierarchical:

- `none` => `["none{:<scope?>}"]`
- `Unapproved` => `["none{:<scope?>}", "Unapproveduted{:<scope?>}"]`
- `Approved` => `["none{:<scope?>}", "Unapproveduted{:<scope?>}", "Approveduted{:<scope?>}"]`
- `Merged` => `["none{:<scope?>}", "Unapproveduted{:<scope?>}", "Approveduted{:<scope?>}", "Merged{:<scope?>}"]`

Order: `Merged > Approveduted > Unapproveduted > none`.

Baseline behavior when no stronger integrity can be derived:
- `Repos = "Public"` => `["none"]`
- `Repos = {"owner":"foo"}` => `["none:foo"]`
- `Repos = {"owner":"foo", "repo":"bar"}` => `["none:foo/bar"]`

Objects with stronger integrity always include the corresponding `none` tag for the same scope.

Scope controls whether tags are unscoped or scoped.

### Global public scope (`"Public"`)

Example for Approveduted object:

```json
["none", "Unapproveduted", "Approveduted"]
```

(Equivalent pattern applies for `merged` by adding `"merged"`.)

### Owner scope (`owner: "foo"`)

Example for merged object:

```json
["none:foo", "Unapproveduted:foo", "Approveduted:foo", "Merged:foo"]
```

### Repo scope (`owner: "foo", repo: "bar"`)

Example for Approveduted object:

```json
["none:foo/bar", "Unapproveduted:foo/bar", "Approveduted:foo/bar"]
```

---

## Evaluation model

A practical evaluation order is:

1. Apply `AllowOnly.Repos` to determine whether an input is in policy scope.
2. If in scope, apply `AllowOnly.min-integrity`.
3. Expose only items that satisfy both checks.

This keeps the policy easy to reason about and aligns with existing public-only and repo-only behavior.

---

## Notes

- This is a schema/design summary, not a wire-format contract yet.
- Existing detailed integrity derivation rules remain in `docs/INTEGRITY_TAG_SPEC.md`.
- Existing secrecy derivation details remain in `docs/SECRECY_TAG_SPEC.md`.
