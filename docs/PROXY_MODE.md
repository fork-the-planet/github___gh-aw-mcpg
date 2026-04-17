# Proxy Mode

Proxy mode (`awmg proxy`) is an HTTP(S) forward proxy that intercepts GitHub API requests and applies DIFC (Data Information Flow Control) filtering using the same guard WASM module as the MCP gateway.

## Motivation

The MCP gateway enforces DIFC on MCP tool calls, but tools that call the GitHub API directly — such as `gh api`, `gh issue list`, or raw `curl` — bypass it entirely. Proxy mode closes this gap by sitting between the HTTP client and `api.github.com`, applying guard policies to REST and GraphQL requests.

## Quick Start

### With `gh` CLI (TLS mode)

```bash
# Start the proxy with self-signed TLS
awmg proxy \
  --guard-wasm guards/github-guard/github_guard.wasm \
  --policy '{"allow-only":{"repos":["org/repo"],"min-integrity":"approved"}}' \
  --github-token "$GITHUB_TOKEN" \
  --listen localhost:8443 \
  --tls

# Trust the generated CA and point gh at the proxy
export GH_HOST=localhost:8443
export NODE_EXTRA_CA_CERTS=/tmp/gh-aw/mcp-logs/proxy-tls/ca.crt
export SSL_CERT_FILE=/tmp/gh-aw/mcp-logs/proxy-tls/ca.crt
export GIT_SSL_CAINFO=/tmp/gh-aw/mcp-logs/proxy-tls/ca.crt
gh issue list -R org/repo
```

### With `curl` (plain HTTP)

```bash
# Start the proxy without TLS
awmg proxy \
  --guard-wasm guards/github-guard/github_guard.wasm \
  --policy '{"allow-only":{"repos":["org/repo"],"min-integrity":"approved"}}' \
  --github-token "$GITHUB_TOKEN" \
  --listen localhost:8080

# Use curl directly
curl -H "Authorization: token $GITHUB_TOKEN" \
  http://localhost:8080/api/v3/repos/org/repo/issues
```

## How It Works

```
gh CLI  →  awmg proxy (localhost:8443, TLS)  →  api.github.com
                     ↓
             6-phase DIFC pipeline
             (same guard WASM module)
```

1. The proxy receives an HTTP request (REST GET or GraphQL POST)
2. It maps the URL/query to a guard tool name (e.g., `/repos/:owner/:repo/issues` → `list_issues`)
3. The guard WASM module evaluates access based on the configured policy
4. If allowed, the request is forwarded to `api.github.com`
5. The response is filtered per-item based on secrecy/integrity labels
6. The filtered response is returned to the client

Write operations (PUT, POST, DELETE, PATCH) pass through unmodified.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--guard-wasm` | *(auto-detected in container)* | Path to the guard WASM module |
| `--policy` | | Guard policy JSON (e.g., `{"allow-only":{"repos":["org/repo"]}}`) |
| `--github-token` | *(forwards client auth)* | Fallback GitHub API token for requests without Authorization header |
| `--listen` / `-l` | `127.0.0.1:8080` | Proxy listen address |
| `--log-dir` | `/tmp/gh-aw/mcp-logs` | Log file directory |
| `--guards-mode` | `filter` | DIFC mode: `strict`, `filter`, or `propagate` |
| `--github-api-url` | `https://api.github.com` | Upstream GitHub API URL |
| `--tls` | `false` | Enable HTTPS with auto-generated self-signed certificates |
| `--tls-dir` | `<log-dir>/proxy-tls` | Directory for generated TLS certificate files |
| `--trusted-bots` | *(disabled)* | Additional trusted bot usernames (comma-separated, extends built-in list) |
| `--trusted-users` | *(disabled)* | User logins that receive approved integrity (comma-separated) |

## DIFC Pipeline

The proxy reuses the same 6-phase pipeline as the MCP gateway, with Phase 3 adapted for HTTP forwarding:

| Phase | Description | Shared with Gateway? |
|-------|-------------|---------------------|
| **0** | Extract agent labels from registry | ✅ |
| **1** | `Guard.LabelResource()` — coarse access check | ✅ |
| **2** | `Evaluator.Evaluate()` — secrecy/integrity evaluation | ✅ |
| **3** | Forward request to GitHub API | ❌ Proxy-specific |
| **4** | `Guard.LabelResponse()` — per-item labeling | ✅ |
| **5** | `Evaluator.FilterCollection()` — fine-grained filtering | ✅ |

## REST Route Mapping

The proxy maps REST API URL patterns to guard tool names (see `internal/proxy/router.go` for the exact source of truth). Inbound paths are normalized first:

- `GH_HOST` style REST paths with `/api/v3/...` are normalized to `/...` for routing.
- Query strings are ignored for route matching and still forwarded upstream.

Supported path families include:

| URL Pattern | Guard Tool |
|-------------|-----------|
| `/repos/:owner/:repo/issues` | `list_issues` |
| `/repos/:owner/:repo/issues/:number` | `issue_read` |
| `/repos/:owner/:repo/pulls` | `list_pull_requests` |
| `/repos/:owner/:repo/pulls/:number` | `pull_request_read` |
| `/repos/:owner/:repo/commits` | `list_commits` |
| `/repos/:owner/:repo/commits/:sha` | `get_commit` |
| `/repos/:owner/:repo/contents/:path` | `get_file_contents` |
| `/repos/:owner/:repo/branches` | `list_branches` |
| `/repos/:owner/:repo/releases` | `list_releases` |
| `/search/issues` | `search_issues` |
| `/search/code` | `search_code` |
| `/search/repositories` | `search_repositories` |
| `/user` | `get_me` |
| `/notifications` | `list_notifications` |
| `/orgs/:owner/actions/(secrets|variables)[/:name]` | `actions_list` |
| `/repos/:owner/:repo/discussions...` | `list_discussions` / `get_discussion_comments` |
| `/repos/:owner/:repo/...` (fallback) | `get_file_contents` |
| ... | See `internal/proxy/router.go` for the complete regex list and precedence |

For **read operations** (GET and GraphQL POST), unmatched routes are denied (fail-closed) to avoid accidental unfiltered data exposure. For **write operations** (non-read methods), requests pass through unchanged.

## GraphQL Support

Inbound GraphQL endpoint paths accepted by the proxy:

- `/graphql` (github.com style)
- `/api/graphql` (GHES style used by `gh` when host is GHES/proxy)
- `/api/v3/graphql` (GH_HOST prefix style; normalized)

GraphQL queries are parsed to extract operation type and owner/repo context:

- **Repository-scoped queries** (issues, PRs, commits) — mapped to corresponding tool names
- **Search queries** — mapped to `search_issues` or `search_code`
- **Viewer queries** — mapped to `get_me`
- **Schema introspection (`__schema`, `__type`)** — passed through (safe metadata)
- **Unknown queries** — denied (fail-closed)

Owner and repo are extracted from GraphQL variables (`$owner`, `$name`/`$repo`) or inline string arguments.

When the upstream API base is GHES-style `.../api/v3`, GraphQL forwarding is rewritten to `.../api/graphql` to match GHES routing.

## Policy Notes

- **Repo names must be lowercase** in policies (e.g., `octocat/hello-world` not `octocat/Hello-World`). The guard performs case-insensitive matching against actual GitHub data.
- All policy formats supported by the MCP gateway work identically in proxy mode:
  - Specific repos: `{"allow-only":{"repos":["org/repo"]}}`
  - Owner wildcards: `{"allow-only":{"repos":["org/*"]}}`
  - Multiple repos: `{"allow-only":{"repos":["org/repo1","org/repo2"]}}`
  - Integrity filtering: `{"allow-only":{"repos":["org/repo"],"min-integrity":"approved"}}`

## Container Usage

The proxy is included in the same container image as the MCP gateway. The baked-in guard WASM module at `/guards/github/00-github-guard.wasm` is auto-detected, so `--guard-wasm` is not needed.

### With TLS (for `gh` CLI)

```bash
# Start the proxy — the entrypoint detects "proxy" and skips gateway checks
docker run --rm -p 8443:8443 \
  -e GITHUB_TOKEN \
  -v /tmp/proxy-logs:/tmp/gh-aw/mcp-logs \
  ghcr.io/github/gh-aw-mcpg:latest proxy \
  --policy '{"allow-only":{"repos":["org/repo"],"min-integrity":"approved"}}' \
  --listen 0.0.0.0:8443 \
  --tls

# Trust the CA cert from the mounted log volume
export GH_HOST=localhost:8443
export NODE_EXTRA_CA_CERTS=/tmp/proxy-logs/proxy-tls/ca.crt
export SSL_CERT_FILE=/tmp/proxy-logs/proxy-tls/ca.crt
export GIT_SSL_CAINFO=/tmp/proxy-logs/proxy-tls/ca.crt
gh issue list -R org/repo
```

### Plain HTTP (for `curl` / testing)

```bash
docker run --rm -p 8080:8080 \
  -e GITHUB_TOKEN \
  -v /tmp/proxy-logs:/tmp/gh-aw/mcp-logs \
  ghcr.io/github/gh-aw-mcpg:latest proxy \
  --policy '{"allow-only":{"repos":["org/repo"],"min-integrity":"none"}}' \
  --listen 0.0.0.0:8080

curl -H "Authorization: token $GITHUB_TOKEN" \
  http://localhost:8080/repos/org/repo/issues
```

### Container Notes

- **Entrypoint routing**: The container entrypoint (`run_containerized.sh`) detects `proxy` as the first argument and skips gateway-specific checks (Docker socket, stdin config, `MCP_GATEWAY_PORT`/`DOMAIN`/`API_KEY`).
- **Guard auto-detection**: `--guard-wasm` defaults to the baked-in `/guards/github/00-github-guard.wasm` inside the container.
- **Log volume**: Mount a host directory to `/tmp/gh-aw/mcp-logs` to persist proxy logs and access the generated TLS CA certificate.
- **Listen address**: Use `0.0.0.0` (not `127.0.0.1`) inside the container so the port mapping works.
- **Token**: Pass `GITHUB_TOKEN` as an environment variable; the proxy resolves it automatically.

## Guards Mode

| Mode | Behavior |
|------|----------|
| `strict` | Blocks entire response if any items are filtered |
| `filter` | Removes filtered items, returns remaining (default) |
| `propagate` | Labels accumulate on the agent; no filtering |

## TLS Support

The `gh` CLI forces HTTPS when connecting to a custom `GH_HOST`. The `--tls` flag generates a short-lived (24h) self-signed CA and server certificate at startup, enabling direct `gh` CLI integration without an external TLS terminator.

### Generated Files

When `--tls` is enabled, the proxy writes to `--tls-dir` (default: `<log-dir>/proxy-tls/`):

| File | Purpose |
|------|---------|
| `ca.crt` | CA certificate — add to client trust store |
| `server.crt` | Server certificate (localhost, 127.0.0.1, ::1) |
| `server.key` | Server private key (0600 permissions) |

### Trusting the CA

**gh CLI / Node.js**:
```bash
export NODE_EXTRA_CA_CERTS=/tmp/gh-aw/mcp-logs/proxy-tls/ca.crt
export SSL_CERT_FILE=/tmp/gh-aw/mcp-logs/proxy-tls/ca.crt
export GIT_SSL_CAINFO=/tmp/gh-aw/mcp-logs/proxy-tls/ca.crt
```

**System-wide (Ubuntu)**:
```bash
cp /tmp/gh-aw/mcp-logs/proxy-tls/ca.crt /usr/local/share/ca-certificates/mcpg-proxy.crt
update-ca-certificates
```

**curl**:
```bash
curl --cacert /tmp/gh-aw/mcp-logs/proxy-tls/ca.crt https://localhost:8443/health
```

## Known Limitations

- **GraphQL nested filtering**: Deeply nested GraphQL response structures depend on guard support for item-level labeling.
- **Read-only filtering**: Only GET requests and GraphQL POST queries are filtered. Write operations pass through unmodified.
