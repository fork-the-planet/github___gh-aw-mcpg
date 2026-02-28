---
name: Nightly Schema Updater
description: Nightly workflow that checks the latest gh-aw release and updates the MCP gateway schema validation URL to the most recent pinned version
on:
  schedule: daily
  workflow_dispatch:

permissions:
  contents: read
  issues: read
  pull-requests: read

network:
  allowed:
    - defaults
    - go

steps:
  - name: Set up Go
    uses: actions/setup-go@v6
    with:
      go-version-file: go.mod
      cache: true

tools:
  github:
    toolsets: [default]
  bash: ["*"]
  edit:

safe-outputs:
  create-pull-request:
    title-prefix: "🔄 "
    labels: [maintenance, automation, schema]
    draft: false
    expires: 7d
  missing-tool:
    create-issue: true

timeout-minutes: 15
---

# Nightly Schema Updater 🔄

You are an AI agent that keeps the MCP Gateway schema validation URL pinned to the latest `gh-aw` release version.

## Mission

The MCP Gateway validates configurations against the `mcp-gateway-config.schema.json` JSON schema from the `github/gh-aw` repository. This schema URL should always reference the latest stable release tag — not `main` — to ensure reproducible, deterministic validation.

Your job is to:
1. Find the latest `github/gh-aw` release tag
2. Check the current schema URLs in the codebase
3. Update them if they are outdated (pointing to `main` or an older version tag)
4. Verify the changes compile and tests pass
5. Open a pull request with the updates

## Step 1: Discover the Latest Release Tag

Use the GitHub MCP tool to get the latest release of `github/gh-aw`:

```
Use github get_latest_release with owner=github, repo=gh-aw
```

Extract the `tag_name` (e.g., `v1.2.3`). This is your target version.

## Step 2: Read Current Schema URLs

Read the two files that contain schema URLs:

```bash
grep -n "schemaURL\|SchemaURL\|raw.githubusercontent.com/github/gh-aw" \
  internal/config/validation_schema.go \
  internal/config/rules/rules.go
```

The files and variables to update are:

- **`internal/config/validation_schema.go`** — variable `schemaURL` (around line 45)
  - Current pattern: `https://raw.githubusercontent.com/github/gh-aw/main/pkg/workflow/schemas/mcp-gateway-config.schema.json`
  - Target pattern: `https://raw.githubusercontent.com/github/gh-aw/<TAG>/pkg/workflow/schemas/mcp-gateway-config.schema.json`

- **`internal/config/rules/rules.go`** — constant `SchemaURL` (around line 15)
  - Current pattern: `https://raw.githubusercontent.com/github/gh-aw/main/docs/public/schemas/mcp-gateway-config.schema.json`
  - Target pattern: `https://raw.githubusercontent.com/github/gh-aw/<TAG>/docs/public/schemas/mcp-gateway-config.schema.json`

## Step 3: Compare Versions

Parse the current URL to extract the version segment (the part between `gh-aw/` and `/pkg` or `/docs`).

- If the current version is already the latest tag, **stop here** — no changes needed, exit successfully.
- If the current version is `main` or an older tag, proceed to update.

## Step 4: Verify the Schema Exists at the New Tag

Before making changes, confirm the schema files actually exist at the new tag URL:

```bash
curl -s -o /dev/null -w "%{http_code}" \
  "https://raw.githubusercontent.com/github/gh-aw/<TAG>/pkg/workflow/schemas/mcp-gateway-config.schema.json"
```

```bash
curl -s -o /dev/null -w "%{http_code}" \
  "https://raw.githubusercontent.com/github/gh-aw/<TAG>/docs/public/schemas/mcp-gateway-config.schema.json"
```

Both should return `200`. If either returns non-200, **stop** and create an issue instead of a PR:

```
Schema file not found at tag <TAG>. Skipping update.
```

## Step 5: Update the Schema URLs

Update both files using the `edit` tool:

### Update `internal/config/validation_schema.go`

Replace the `schemaURL` value with the new versioned URL. If the file contains a comment line matching `// Current schema version: ...`, update it to reflect the new version; otherwise skip the comment update.

### Update `internal/config/rules/rules.go`

Replace the `SchemaURL` constant value with the new versioned URL.

## Step 6: Validate the Changes

After editing, verify the changes are syntactically correct and tests pass:

```bash
go build ./...
```

```bash
go test ./internal/config/...
```

If tests fail, revert the changes and create an issue explaining the failure instead of a PR.

## Step 7: Create a Pull Request

If all checks pass, create a pull request via safe-outputs.

Use the following as the PR body template:

```markdown
## Schema URL Update

Updates the MCP Gateway JSON schema validation URL from the previous version to `<TAG>`.

### Files Changed

- `internal/config/validation_schema.go` — `schemaURL` variable updated to `<TAG>`
- `internal/config/rules/rules.go` — `SchemaURL` constant updated to `<TAG>`

### Why

Pinning to a specific release tag ensures reproducible, deterministic configuration validation — the schema won't silently change between runs.

### Release Notes

See the [gh-aw release notes](https://github.com/github/gh-aw/releases/tag/<TAG>) for changes in this schema version.
```

## Important Notes

- **Only create a PR if URLs actually changed** — if already up-to-date, exit quietly with success
- **Never use `main`** — always pin to a specific release tag
- **Validate before committing** — run `go build ./...` and `go test ./internal/config/...` first
- **One PR per run** — the `create-pull-request` safe output will reuse an existing open PR with the same title prefix if present
