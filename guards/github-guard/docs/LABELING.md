# GitHub Guard DIFC Labeling Specification

This document specifies how the GitHub Guard assigns DIFC (Decentralized Information Flow Control) labels to GitHub resources.

## Overview

Labels are **derived, not stored**: all labels are computed from Git history and GitHub API metadata. The guard does not store or persist labels.

## Label Types

> **Important**: The implementation currently uses `unapproved:<scope>`, `approved:<scope>`, and `merged:<scope>` integrity labels.
> Older `contributor:<scope>` / `project:<scope>` terminology in historical docs maps to:
> - `contributor` → `unapproved`
> - `project` → `approved`
> - merged/default-branch endorsement → `merged`

### Secrecy Labels

Secrecy labels constrain **information release** (confidentiality).

| Label | Meaning |
|-------|---------|
| `[]` (empty) | Public data, may be disclosed to anyone |
| `private:<owner>/<repo>` | Restricted to collaborators of the repository |
| `secret` | Must not be disclosed (e.g., scanning alerts referencing actual secrets) |

**Flow Rule**: Data can only be consumed by agents with *equal or higher* secrecy clearance.

### Integrity Labels

Integrity labels represent **endorsement and trust**, not provenance.

| Label | Meaning |
|-------|---------|
| `[]` (empty) | No endorsement, untrusted |
| `unapproved:<scope>` | Endorsed at reader-contribution level |
| `approved:<scope>` | Endorsed at repository writer/maintainer level |
| `merged:<scope>` | Endorsed as merged/default-branch reachable history |

**Hierarchy**: `merged > approved > unapproved > (none)`

**Flow Rule**: Agents can only trust data with *the same or greater* endorsement than their own.

### Hierarchical Expansion

The guard explicitly expands integrity labels to include all implied lower levels:

| When assigning... | Guard includes... |
|-------------------|-------------------|
| `unapproved:<scope>` | `unapproved:<scope>` |
| `approved:<scope>` | `unapproved:<scope>`, `approved:<scope>` |
| `merged:<scope>` | `unapproved:<scope>`, `approved:<scope>`, `merged:<scope>` |

This ensures DIFC flow checks work correctly without the evaluator understanding the hierarchy.

## Label Derivation Rules

### Repositories

| Condition | Secrecy | Integrity |
|-----------|---------|-----------|
| Public repository | `[]` | `approved + unapproved` |
| Private repository | `private:<owner>/<repo>` | `approved + unapproved` |

### Commits

| Condition | Secrecy | Integrity |
|-----------|---------|-----------|
| On default/protected branch | `S(repo)` | `merged + approved + unapproved` |
| On feature branch | `S(repo)` | `unapproved` (or elevated by policy evidence) |
| From bot | `S(repo)` | `approved + unapproved` |

### Pull Requests

| Condition | Secrecy | Integrity |
|-----------|---------|-----------|
| Merged | `S(repo)` | `merged + approved + unapproved` |
| Open (human author) | `S(repo)` | `unapproved` (or elevated by policy evidence) |
| From bot | `S(repo)` | `approved + unapproved` |

### Issues and Comments

| Condition | Secrecy | Integrity |
|-----------|---------|-----------|
| From repo owner | `S(repo)` | `approved + unapproved` |
| From bot | `S(repo)` | `approved + unapproved` |
| From verified contributor | `S(repo)` | `unapproved` |
| From unknown author | `S(repo)` | `[]` (untrusted) |

**Note**: A "verified contributor" is a user with at least one merged PR in the repository.

### Security Alerts

| Tool | Secrecy | Integrity |
|------|---------|-----------|
| Secret scanning | `secret` | `approved + unapproved` |
| Code scanning | `private:<repo>` | `approved + unapproved` |
| Dependabot | `private:<repo>` | `approved + unapproved` |
| Global advisories | `[]` | `approved:github` |
| Repo advisories | `private:<repo>` | `unapproved` |

### File Contents

| Condition | Secrecy | Integrity |
|-----------|---------|-----------|
| Sensitive path patterns | `secret` | Varies |
| Normal files | `S(repo)` | Varies based on commit |

**Sensitive path patterns**:
- `.env*`, `*.key`, `*.pem`, `*secret*`, `*password*`
- `config/secrets/*`, `.github/workflows/*` (may contain secrets)

## Implementation Notes

### Integrity Scope

For organization-owned repos, the scope is the org name (e.g., `github`).
For user-owned repos, the scope is the full repo path (e.g., `octocat/Hello-World`).

### Bot Detection

Users with these patterns are treated as bots:
- Username ends with `[bot]`
- Username ends with `-bot`
- Known bots: `dependabot`, `renovate`, `github-actions`, `copilot`

### Collection Responses

For list/search operations, the guard labels each item individually:
- `search_repositories`: Each repo labeled based on its `private` field
- `list_issues`: Each issue labeled based on author status
- `list_pull_requests`: Each PR labeled based on merged state

This enables fine-grained filtering where some items pass and others are blocked.

## References

- [github-difc.md](https://github.com/lpcox/gh-aw-mcpg/blob/main/docs/github-difc.md) - Full DIFC specification
- [README.md](../README.md) - Project overview
