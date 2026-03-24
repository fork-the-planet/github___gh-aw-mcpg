---
name: Integrity Filtering Audit
description: Daily audit of recent agentic workflow runs in github/gh-aw to identify integrity filtering problems
on:
  schedule: daily on weekdays
  workflow_dispatch:

permissions:
  contents: read
  issues: read
  actions: read

engine: copilot

network:
  allowed:
    - defaults
    - github

tools:
  github:
    toolsets: [repos, issues, actions]
    allowed-repos: ["github/gh-aw", "github/gh-aw-mcpg"]
    min-integrity: approved
  bash: true
  edit:

safe-outputs:
  mentions: false
  allowed-github-references: []
  max-bot-mentions: 1
  create-issue:
    title-prefix: "[integrity-audit] "
    labels: [integrity-audit, automation]
    close-older-issues: true
    expires: 7
    max: 1

timeout-minutes: 20
---

# Integrity Filtering Audit

**Goal**: Audit recent agentic workflow runs in `github/gh-aw` to detect integrity
filtering anomalies, misconfigurations, or silent failures in the DIFC pipeline.

## Background

The MCP Gateway enforces Decentralized Information Flow Control (DIFC) on all
GitHub API calls made by agentic workflows. The guard (WASM module) labels each
API response with integrity and secrecy tags. The evaluator then filters items
that violate the agent's policy.

Common problems to look for:
- **Zero DIFC events** in runs that should have filtering (proxy not intercepting)
- **Unexpected access** to out-of-scope repositories
- **Guard errors** or labeling failures in logs
- **Unscoped integrity tags** (e.g., `approved` instead of `approved:owner/repo`)
- **Empty responses** where data was expected (over-filtering)
- **Search result leaks** where out-of-scope items appear in filtered results

## Procedure

### Step 1: Gather Recent Workflow Runs

Use the GitHub CLI to list completed agentic workflow runs from the last 24 hours
in `github/gh-aw`. Focus on runs that used the MCP Gateway (look for workflows
with names containing "agent", "copilot", or that have MCP-related artifacts).

```bash
# List recent completed runs from the last 24 hours
gh run list --repo github/gh-aw --limit 50 --json databaseId,name,conclusion,createdAt,headBranch,workflowName --status completed 2>/dev/null | jq '[.[] | select((.createdAt | fromdateiso8601) > (now - 86400))]'
```

### Step 2: Download and Inspect Artifacts

For each relevant run, download the agent artifacts which contain:
- `mcp-logs/rpc-messages.jsonl` — RPC messages with DIFC metadata
- `mcp-logs/mcp-gateway.log` — Gateway operational log
- `mcp-logs/gateway.md` — Markdown-formatted gateway log

```bash
# For each run ID, download artifacts to a temp directory
TMPDIR=$(mktemp -d)
gh run download <RUN_ID> --repo github/gh-aw --dir "$TMPDIR" 2>/dev/null || echo "No artifacts"
```

### Step 3: Analyze DIFC Events

For each downloaded artifact set, check:

1. **JSONL integrity tags**: Parse `rpc-messages.jsonl` for entries with
   `difc_secrecy` and `difc_integrity` fields. Flag any with empty or
   unscoped tags.

2. **Filtered counts**: Look for `DIFC_FILTERED` or `filtered` entries.
   Compare the filtered count to total items — high filter ratios may indicate
   over-filtering or misconfiguration.

3. **Guard errors**: Search gateway logs for `ERROR`, `Phase .* failed`,
   `guard not initialized`, or `unknown REST endpoint`.

4. **Scope violations**: Check if any response contains data from repositories
   NOT in the workflow's `allowed-repos` policy.

```bash
# Example: Count DIFC events in JSONL
grep -c 'difc_integrity' "$TMPDIR"/*/mcp-logs/rpc-messages.jsonl 2>/dev/null || echo "0"

# Example: Find guard errors
grep -iE 'error|failed|blocked|unknown' "$TMPDIR"/*/mcp-logs/mcp-gateway.log 2>/dev/null | head -20
```

### Step 4: Classify Findings

Classify each finding by severity:
- 🔴 **Critical**: Data leak (out-of-scope data returned), guard bypass, or
  labeling failure that could expose unauthorized data
- 🟡 **Warning**: Over-filtering (legitimate data blocked), unscoped tags,
  or zero DIFC events in a run that should have filtering
- 🟢 **Info**: Normal filtering behavior, expected blocks, or configuration notes

### Step 5: Create Summary Issue

Create an issue with the audit results using the following structure:

```markdown
### Integrity Filtering Audit — ${{ github.repository }}

**Audit period**: Last 24 hours
**Runs analyzed**: [N] completed runs in github/gh-aw
**Runs with artifacts**: [N]

### Findings Summary

| Severity | Count | Description |
|----------|-------|-------------|
| 🔴 Critical | [N] | [brief] |
| 🟡 Warning | [N] | [brief] |
| 🟢 Info | [N] | [brief] |

<details>
<summary><b>Critical Findings</b></summary>

[Details of each critical finding with run ID, workflow name, and evidence]

</details>

<details>
<summary><b>Warnings</b></summary>

[Details of each warning]

</details>

<details>
<summary><b>Informational</b></summary>

[Normal observations and stats]

</details>

### Runs Analyzed

| Run | Workflow | Branch | DIFC Events | Filtered | Status |
|-----|----------|--------|-------------|----------|--------|
| [§ID](run_url) | name | branch | N | N | ✅/⚠️/❌ |

### Recommendations

[Actionable suggestions based on findings]
```

If there are no findings (all runs look healthy), still create the issue with
a clean bill of health — this provides an audit trail.
