# AWF Pipeline Environment Variables

Comprehensive reference for environment variables used across the **gh-aw ↔ AWF pipeline**, covering the full lifecycle from Actions runner initialization through DIFC proxy rewriting, compiler injection, AWF container generation, and agent container execution.

This document addresses the complexity described in gh-aw-firewall#1492, gh-aw-firewall#1452, gh-aw-firewall#1481, and gh-aw#23461, where underdocumented variable interactions across pipeline stages caused production issues.

---

## 1. Variables Set by the Actions Runner

These are set by the GitHub Actions runtime before any pipeline step runs. They are the **canonical source of truth** for the GitHub instance identity.

| Variable | github.com | GHES | GHEC (`*.ghe.com`) |
|----------|-----------|------|--------------------|
| `GITHUB_SERVER_URL` | `https://github.com` | `https://github.company.com` | `https://company.ghe.com` |
| `GITHUB_API_URL` | `https://api.github.com` | `https://github.company.com/api/v3` | `https://api.company.ghe.com` |
| `GITHUB_GRAPHQL_URL` | `https://api.github.com/graphql` | `https://github.company.com/api/graphql` | `https://api.company.ghe.com/graphql` |
| `GITHUB_TOKEN` | Runner token | Runner token | Runner token |
| `GITHUB_REPOSITORY` | `owner/repo` | `owner/repo` | `owner/repo` |

---

## 2. Variables Rewritten by the DIFC Proxy

When the DIFC proxy step (`start_difc_proxy.sh`) runs, it rewrites these variables via `$GITHUB_ENV`. All subsequent steps in the workflow see the rewritten values.

| Variable | Original Value | DIFC-Rewritten Value | Impact |
|----------|---------------|---------------------|--------|
| `GH_HOST` | *(not set)* | `localhost:18443` | `gh` CLI routes all requests through the local proxy |
| `GITHUB_API_URL` | `https://api.github.com` | `https://localhost:18443/api/v3` | REST API calls routed through proxy |
| `GITHUB_GRAPHQL_URL` | `https://api.github.com/graphql` | `https://localhost:18443/api/graphql` | GraphQL calls routed through proxy |
| `GITHUB_SERVER_URL` | *(set by runner)* | **Not rewritten** | Remains canonical — safe to derive `GH_HOST` from it inside the container |

**Critical insight**: `GITHUB_SERVER_URL` is deliberately left unchanged by the DIFC proxy. This makes it the only reliable source for deriving the correct GitHub instance hostname inside an agent container.

---

## 3. Variables Set by the gh-aw Compiler

The compiler may inject additional variables via `$GITHUB_ENV` in workflow steps before AWF runs.

| Variable | When Set | Purpose |
|----------|---------|---------|
| `GH_REPO` | v0.64.3+ (after DIFC proxy step) | Set to `${GITHUB_REPOSITORY}`; bypasses `GH_HOST` ↔ remote matching in `gh` CLI |
| `COPILOT_GITHUB_TOKEN` | Token exchange step | Scoped token for Copilot API calls |

---

## 4. AWF Proxy Variables (Agent Container)

AWF's `docker-manager.ts` generates these environment variables for the **agent container** via `docker-compose.yml`.

| Variable | Value | Purpose |
|----------|-------|---------|
| `HTTP_PROXY` | `http://172.30.0.10:3128` | Route HTTP traffic through Squid |
| `HTTPS_PROXY` | `http://172.30.0.10:3128` | Route HTTPS through Squid (CONNECT tunnel) |
| `https_proxy` | `http://172.30.0.10:3128` | Lowercase variant for Yarn 4 / undici / Corepack |
| `http_proxy` | *(NOT set)* | **Intentionally omitted** — see note below |
| `SQUID_PROXY_HOST` | `squid-proxy` | Raw hostname for JVM tool proxy configuration |
| `SQUID_PROXY_PORT` | `3128` | Raw port for JVM tool proxy configuration |
| `NO_PROXY` | `localhost,127.0.0.1,::1,0.0.0.0,172.30.0.10,172.30.0.20[,172.30.0.30]` | Bypass proxy for local/sidecar traffic |
| `no_proxy` | *(same as `NO_PROXY`)* | Lowercase variant for Python `requests` and similar tools |
| `JAVA_TOOL_OPTIONS` | `-Dhttp.proxyHost=... -Dhttps.proxyHost=...` | JVM system property proxy (set in `entrypoint.sh`) |

> **Why `http_proxy` is NOT set**: On Ubuntu 22.04, `curl` ignores the uppercase `HTTP_PROXY` variable for HTTP requests (httpoxy mitigation). Setting the lowercase `http_proxy` would cause Squid's 403 error page to return exit code 0, breaking security test assertions. HTTP traffic falls through to iptables DNAT → Squid instead.

---

## 5. GitHub Identity Variables in the Agent Container

These variables control how the `gh` CLI and Copilot CLI identify and reach the target GitHub instance.

| Variable | With `--env-all` | Without `--env-all` | Behavior |
|----------|-----------------|--------------------|----|
| `GH_HOST` | **Always derived from `GITHUB_SERVER_URL`** (PR github/gh-aw-mcpg#1493). Deleted on github.com; set to hostname on GHES/GHEC. | Same | Prevents DIFC-proxy-leaked values from breaking `gh` |
| `GITHUB_SERVER_URL` | Passed through from host, then **explicitly re-set** from `process.env` | Explicitly set if present | Canonical GitHub instance URL |
| `GITHUB_API_URL` | Passed through (**not excluded**) | Explicitly set if present | **⚠️ May carry DIFC-rewritten value** (`https://localhost:18443/...`) |
| `GITHUB_GRAPHQL_URL` | Passed through (**not excluded**) | Not explicitly forwarded | **⚠️ May carry DIFC-rewritten value** (`https://localhost:18443/...`) |

> **Gap**: `GITHUB_API_URL` and `GITHUB_GRAPHQL_URL` are not sanitized the same way `GH_HOST` is. When `--env-all` is used, these may still carry DIFC-rewritten localhost values into the container. The `gh` CLI and Copilot CLI may use these for REST and GraphQL calls.

---

## 6. API Proxy Sidecar Variables

When `--enable-api-proxy` is active, the system is split across two environments: the agent container (which sees placeholder values) and the API proxy sidecar (which holds real credentials).

### Agent Container (placeholder values)

| Variable | Value | Purpose |
|----------|-------|---------|
| `OPENAI_BASE_URL` | `http://172.30.0.30:10000/v1` | Points to sidecar, not the real OpenAI API |
| `ANTHROPIC_BASE_URL` | `http://172.30.0.30:10001` | Points to sidecar, not the real Anthropic API |
| `COPILOT_API_URL` | `http://172.30.0.30:10002` | Points to sidecar, not the real Copilot API |
| `GOOGLE_GEMINI_BASE_URL` | `http://172.30.0.30:10003` | Points to sidecar, not the real Gemini API (see gh-aw-firewall#2348) |
| `GEMINI_API_BASE_URL` | *(excluded)* | Removed to prevent Gemini CLI from bypassing the sidecar |
| `COPILOT_GITHUB_TOKEN` | `placeholder-token-for-credential-isolation` | Set early (before `--env-all`) to prevent override by host value |
| `COPILOT_TOKEN` | `placeholder-token-for-credential-isolation` | For Copilot CLI compatibility |
| `ANTHROPIC_AUTH_TOKEN` | `placeholder-token-for-credential-isolation` | For Claude Code CLI compatibility |
| `CLAUDE_CODE_API_KEY_HELPER` | `/usr/local/bin/get-claude-key.sh` | Returns placeholder key when invoked |
| `OPENAI_API_KEY` | *(excluded)* | Removed from agent container environment |
| `ANTHROPIC_API_KEY` | *(excluded)* | Removed from agent container environment |

### API Proxy Sidecar (real credentials)

| Variable | Value | Purpose |
|----------|-------|---------|
| `OPENAI_API_KEY` | Real key | Injected into upstream OpenAI requests |
| `ANTHROPIC_API_KEY` | Real key | Injected as `x-api-key` header in upstream Anthropic requests |
| `COPILOT_GITHUB_TOKEN` | Real token | Injected as Bearer auth in upstream Copilot requests |
| `GOOGLE_API_KEY` | Real key | Injected into upstream Gemini API requests (port 10003) |
| `COPILOT_API_TARGET` | Override or auto-derived | See Section 7 for derivation rules |
| `OPENAI_API_TARGET` | Override (default: `api.openai.com`) | Upstream OpenAI API hostname |
| `ANTHROPIC_API_TARGET` | Override (default: `api.anthropic.com`) | Upstream Anthropic API hostname |
| `GITHUB_SERVER_URL` | From host environment | Used by `deriveCopilotApiTarget()` |
| `HTTP_PROXY` / `HTTPS_PROXY` | `http://172.30.0.10:3128` | Sidecar routes outbound traffic through Squid |
| `NO_PROXY` | `localhost,127.0.0.1,::1` | Bypasses proxy for internal healthcheck traffic only |

---

## 7. Copilot API Target Derivation

The `deriveCopilotApiTarget()` function in the API proxy sidecar (`server.js`) resolves the upstream Copilot API endpoint using the following three-tier priority:

| Priority | Source | Example |
|----------|--------|---------|
| 1 (highest) | `COPILOT_API_TARGET` environment variable | Any custom endpoint override |
| 2 | Auto-derived from `GITHUB_SERVER_URL` | See table below |
| 3 (lowest) | Hardcoded default | `api.githubcopilot.com` |

### Auto-derivation by instance type

| `GITHUB_SERVER_URL` | Instance Type | Derived `COPILOT_API_TARGET` |
|---------------------|---------------|------------------------------|
| `https://github.com` | Public GitHub | `api.githubcopilot.com` |
| `https://company.ghe.com` | GHEC | `copilot-api.company.ghe.com` |
| `https://github.company.com` | GHES | `api.enterprise.githubcopilot.com` |

> **Important**: The Copilot inference sidecar uses `copilot-api.<slug>.ghe.com`, **not** `api.<slug>.ghe.com`. This is distinct from the **mcpg DIFC proxy**, which forwards REST/GraphQL `gh api` calls and therefore targets the REST API host `api.<slug>.ghe.com` (derived from `GITHUB_SERVER_URL`). Do not point the DIFC proxy at `copilot-api.*` — that endpoint does not serve the REST API (e.g. `/rate_limit`).

---

## 8. GHEC Domain Auto-Allowlisting

AWF's `extractGhecDomainsFromServerUrl()` in `cli.ts` automatically adds GHEC domains to the Squid proxy allowlist.

For `GITHUB_SERVER_URL=https://company.ghe.com`, the following domains are added:

- `company.ghe.com`
- `api.company.ghe.com`
- `copilot-api.company.ghe.com`
- `copilot-telemetry-service.company.ghe.com`

---

## 9. One-Shot Token Protection

The `AWF_ONE_SHOT_TOKENS` mechanism (implemented as an `LD_PRELOAD` library) protects sensitive tokens by caching the value on the first `getenv()` call and scrubbing the variable from `/proc/self/environ`.

### Protected tokens

`COPILOT_GITHUB_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN`, `GITHUB_API_TOKEN`, `GITHUB_PAT`, `GH_ACCESS_TOKEN`, `OPENAI_API_KEY`, `OPENAI_KEY`, `ANTHROPIC_API_KEY`, `CLAUDE_API_KEY`, `CODEX_API_KEY`

### Not currently protected (known gap)

`GITHUB_MCP_SERVER_TOKEN`, `GH_AW_GITHUB_TOKEN` — identified in gh-aw-firewall#1481.

---

## 10. EXCLUDED_ENV_VARS

Variables in this list are never forwarded to the agent container, even when `--env-all` is used.

### Base set (always excluded)

`PATH`, `PWD`, `OLDPWD`, `SHLVL`, `_`, `SUDO_COMMAND`, `SUDO_USER`, `SUDO_UID`, `SUDO_GID`

### When `--enable-api-proxy` is active (additionally excluded)

`OPENAI_API_KEY`, `OPENAI_KEY`, `CODEX_API_KEY`, `ANTHROPIC_API_KEY`, `CLAUDE_API_KEY`

### Not excluded (requires manual attention)

| Variable | Status | Notes |
|----------|--------|-------|
| `GH_HOST` | ✅ Handled | Always overridden by AWF from `GITHUB_SERVER_URL` (PR github/gh-aw-mcpg#1493) |
| `GITHUB_API_URL` | ⚠️ Gap | May carry DIFC-rewritten `localhost:18443` value |
| `GITHUB_GRAPHQL_URL` | ⚠️ Gap | May carry DIFC-rewritten `localhost:18443` value |

---

## 11. Variable Lifecycle Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│ GitHub Actions Runner                                               │
│                                                                     │
│  GITHUB_SERVER_URL=https://github.com       ← Set by runner        │
│  GITHUB_API_URL=https://api.github.com      ← Set by runner        │
│  GITHUB_GRAPHQL_URL=https://api.github.com/graphql                 │
│  GITHUB_TOKEN=ghs_xxx                       ← Set by runner        │
│                                                                     │
│  ┌────────────────────────────────────────────────────────────┐     │
│  │ DIFC Proxy Step (if enabled)                               │     │
│  │                                                            │     │
│  │  GH_HOST=localhost:18443                  → $GITHUB_ENV    │     │
│  │  GITHUB_API_URL=https://localhost:18443/api/v3             │     │
│  │  GITHUB_GRAPHQL_URL=https://localhost:18443/api/graphql    │     │
│  │  GITHUB_SERVER_URL ← unchanged (canonical)                 │     │
│  └────────────────────────────────────────────────────────────┘     │
│                                                                     │
│  ┌────────────────────────────────────────────────────────────┐     │
│  │ gh-aw Compiler Steps                                       │     │
│  │  GH_REPO=${GITHUB_REPOSITORY}             → $GITHUB_ENV    │     │
│  │  (v0.64.3+: bypasses GH_HOST ↔ remote matching)           │     │
│  └────────────────────────────────────────────────────────────┘     │
│                                                                     │
│  ┌────────────────────────────────────────────────────────────┐     │
│  │ AWF CLI (awf --env-all --enable-api-proxy ...)             │     │
│  │                                                            │     │
│  │  Reads: process.env (includes DIFC-rewritten values)       │     │
│  │  Generates: docker-compose.yml with agent environment      │     │
│  │                                                            │     │
│  │  ┌─────────────────────────────────────────────────────┐   │     │
│  │  │ Agent Container (172.30.0.20)                       │   │     │
│  │  │                                                     │   │     │
│  │  │  HTTP_PROXY=http://172.30.0.10:3128                 │   │     │
│  │  │  HTTPS_PROXY=http://172.30.0.10:3128                │   │     │
│  │  │  GH_HOST=<derived from GITHUB_SERVER_URL>           │   │     │
│  │  │  GITHUB_SERVER_URL=<from runner, unchanged>         │   │     │
│  │  │  GITHUB_API_URL=<⚠️ may be DIFC-rewritten>         │   │     │
│  │  │  OPENAI_BASE_URL=http://172.30.0.30:10000/v1        │   │     │
│  │  │  COPILOT_API_URL=http://172.30.0.30:10002           │   │     │
│  │  │  COPILOT_GITHUB_TOKEN=placeholder-token-...         │   │     │
│  │  └─────────────────────────────────────────────────────┘   │     │
│  │                                                            │     │
│  │  ┌─────────────────────────────────────────────────────┐   │     │
│  │  │ API Proxy Sidecar (172.30.0.30)                     │   │     │
│  │  │                                                     │   │     │
│  │  │  OPENAI_API_KEY=sk-xxx (real)                       │   │     │
│  │  │  ANTHROPIC_API_KEY=sk-ant-xxx (real)                │   │     │
│  │  │  COPILOT_GITHUB_TOKEN=ghu_xxx (real)                │   │     │
│  │  │  HTTP_PROXY=http://172.30.0.10:3128                 │   │     │
│  │  │  GITHUB_SERVER_URL=<from runner>                    │   │     │
│  │  └─────────────────────────────────────────────────────┘   │     │
│  │                                                            │     │
│  │  ┌─────────────────────────────────────────────────────┐   │     │
│  │  │ Squid Proxy (172.30.0.10)                           │   │     │
│  │  │                                                     │   │     │
│  │  │  Domain ACL from --allow-domains                    │   │     │
│  │  │  + auto-added GHEC domains                          │   │     │
│  │  │  + auto-added API target domains                    │   │     │
│  │  └─────────────────────────────────────────────────────┘   │     │
│  └────────────────────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 12. Outstanding Gaps

| # | Gap | Description | Affected Variables |
|---|-----|-------------|-------------------|
| 1 | `GITHUB_API_URL` / `GITHUB_GRAPHQL_URL` leakage | These can carry DIFC-rewritten `localhost:18443` values into the agent container via `--env-all`. AWF sanitizes `GH_HOST` (PR github/gh-aw-mcpg#1493) but not these. The Copilot CLI uses `GITHUB_API_URL` for REST API calls. | `GITHUB_API_URL`, `GITHUB_GRAPHQL_URL` |
| 2 | One-shot token coverage | `GITHUB_MCP_SERVER_TOKEN` and `GH_AW_GITHUB_TOKEN` are absent from the `AWF_ONE_SHOT_TOKENS` list | `GITHUB_MCP_SERVER_TOKEN`, `GH_AW_GITHUB_TOKEN` |
| 3 | ~~No Gemini API proxy~~ — **Fixed** | `GOOGLE_GEMINI_BASE_URL` is now set to `http://172.30.0.30:10003` to route Gemini CLI through the API proxy sidecar, and `GEMINI_API_BASE_URL` is excluded. See gh-aw-firewall#2348. An additional MCP protocol compatibility fix was applied to mcpg to handle Gemini CLI v0.37.x calling `tools/call` before completing the session handshake. | `GOOGLE_GEMINI_BASE_URL`, `GEMINI_API_BASE_URL`, `GOOGLE_API_KEY` |
| 4 | No single reference document | Each component documents its own variables in isolation; interactions across stages (DIFC → AWF → container) were not described anywhere | All |

---

## 13. Variable Precedence in AWF

When AWF constructs the container environment, variables are applied in this order (last write wins):

1. **Base variables** — hardcoded by AWF (e.g., `HTTP_PROXY`, `HTTPS_PROXY`)
2. **`--env-all`** — all host environment variables (filtered by `EXCLUDED_ENV_VARS`)
3. **`--env-file`** — variables from a file
4. **`--env`** — individual variable overrides
5. **Post-processing overrides** — e.g., `GH_HOST` is always derived from `GITHUB_SERVER_URL` and overrides any value set in steps 2–4

---

## References

- gh-aw-firewall#1492 — `GH_HOST` proxy passthrough bug
- github/gh-aw-mcpg#1493 — PR implementing `GH_HOST` sanitization
- gh-aw-firewall#1452 — `GH_HOST` auto-injection interaction with `--env-all`
- gh-aw-firewall#1481 — `--env-all` exposing secrets; one-shot token list incomplete
- gh-aw#23461 — User-reported `GH_HOST` breakage
- gh-aw#23092 — Safe outputs env vars not reaching agent container
