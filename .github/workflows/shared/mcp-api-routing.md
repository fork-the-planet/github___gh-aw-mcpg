---
# MCP Gateway API routing constraints — import this in any workflow that makes
# GitHub API calls to ensure the agent is reminded to use MCP tools exclusively.
---

## ⚠️ IMPORTANT: GitHub API Routing Constraint

**All GitHub API calls MUST be made exclusively through the MCP Gateway's GitHub
MCP server tools.** Direct network access to `api.github.com`, `github.com`, or
any external service is not permitted and will be blocked by the network firewall.

### Correct Usage

Use the provided MCP tools (e.g., `github-mcp-server` toolset) for all GitHub
operations:

```
✅ Use github-mcp-server list_issues with owner=..., repo=...
✅ Use github-mcp-server get_file_contents with owner=..., repo=..., path=...
✅ Use github-mcp-server list_workflow_runs with owner=..., repo=...
```

### Incorrect Usage

Do NOT use `curl`, `wget`, `fetch`, or any other HTTP client to contact GitHub's
APIs directly. Do NOT attempt to contact external AI services:

```
❌ curl https://api.github.com/repos/...          (blocked — use MCP tools)
❌ gh api /repos/...                              (blocked — use MCP tools)
❌ fetch("https://api.github.com/...")            (blocked — use MCP tools)
❌ curl https://chatgpt.com/...                   (blocked — external service)
❌ curl https://api.openai.com/...                (blocked — external service)
```

### Why This Matters

- The MCP Gateway applies **DIFC (Decentralized Information Flow Control)**
  integrity and secrecy labels to all GitHub API responses, enforcing scope
  restrictions and preventing data leaks.
- Direct API calls bypass DIFC enforcement entirely, making it impossible to
  audit what data the agent accessed or ensure scope compliance.
- Direct calls to external AI services (e.g., ChatGPT) are out-of-scope and
  constitute a security boundary violation; all reasoning must happen inside
  the Copilot engine provided by the workflow runtime.
- Network firewall blocks from bypass attempts are **audited** by the Integrity
  Filtering Audit workflow and will be flagged as W-1 warnings.

### Checklist

Before making any API call, verify:
1. ✅ Am I using a GitHub MCP server tool (not `curl`, `gh`, or HTTP fetch)?
2. ✅ Is the target repository in the workflow's `allowed-repos` list?
3. ✅ Is `features.difc-proxy: true` enabled in this workflow's configuration?
4. ✅ Am I NOT trying to contact any external AI service API?
