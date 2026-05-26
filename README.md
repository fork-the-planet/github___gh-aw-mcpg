# MCP Gateway

A gateway for Model Context Protocol (MCP) servers.

This gateway is used with [GitHub Agentic Workflows](https://github.com/github/gh-aw) via the `sandbox.mcp` configuration to provide MCP server access to AI agents running in sandboxed environments.

## Quick Start

1. **Pull the Docker image** (when available):
   ```bash
   docker pull ghcr.io/github/gh-aw-mcpg:latest
   ```

2. **Create a configuration file** (`config.json`):
   ```json
   {
     "gateway": {
       "apiKey": "${MCP_GATEWAY_API_KEY}"
     },
     "mcpServers": {
       "github": {
         "type": "stdio",
         "container": "ghcr.io/github/github-mcp-server:latest",
         "env": {
           "GITHUB_PERSONAL_ACCESS_TOKEN": ""
         }
       }
     }
   }
   ```

   Looking for complete examples? See [`config.example.toml`](config.example.toml), [`config.example-payload-threshold.toml`](config.example-payload-threshold.toml), and [`example-http-config.json`](example-http-config.json).

3. **Run the container**:
   ```bash
   docker run --rm -i \
     -e MCP_GATEWAY_PORT=8000 \
     -e MCP_GATEWAY_DOMAIN=localhost \
     -e MCP_GATEWAY_API_KEY=your-secret-key \
     -v /var/run/docker.sock:/var/run/docker.sock \
     -v /path/to/logs:/tmp/gh-aw/mcp-logs \
     -p 8000:8000 \
     ghcr.io/github/gh-aw-mcpg:latest < config.json
   ```

> [!NOTE]
> The container entrypoint script (`run_containerized.sh`) automatically adds `--config-stdin` when it starts `awmg`. If you run `awmg` directly (outside the container) and want to pipe JSON config, you must pass `--config-stdin` explicitly.

Inside the container, the gateway starts in routed mode on `http://0.0.0.0:8000`, proxying MCP requests to your configured backend servers. When running `awmg` directly without `--listen`, the default listen address is `http://127.0.0.1:3000`.

**Required flags:**
- `-i`: Enables stdin for passing JSON configuration
- `-v /var/run/docker.sock`: Required for spawning backend MCP servers
- `-p 8000:8000`: Port mapping must match `MCP_GATEWAY_PORT`
- If you configure `payloadDir` / `MCP_GATEWAY_PAYLOAD_DIR`, use an absolute path (for example `/tmp/jq-payloads`)
- If you configure `payloadDir`, you can also tune `payloadSizeThreshold` / `MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD` to control when payloads are written to disk (default: `524288` bytes)

When running `awmg` directly (outside `docker run`), useful CLI flags include:
- `--config-stdin`: Read JSON config from stdin (required when piping config, e.g. `cat config.json | awmg --config-stdin --routed`).
- `--env <file>`: Load environment variables from a `.env` file before startup.
- `-v`, `-vv`, `-vvv`: Increase verbosity (`info`, `debug`, `trace`).
- Containerized-only startup env vars such as `MCP_GATEWAY_HOST` and `MCP_GATEWAY_MODE` are documented in [docs/ENVIRONMENT_VARIABLES.md](docs/ENVIRONMENT_VARIABLES.md).

## Guard Policies

Guard policies enforce integrity filtering and private-data leaking at the gateway level, restricting what data agents can access and where they can write. Each server can have either an `allow-only` or a `write-sink` policy.

### allow-only (source servers)

Restricts which repositories a guard allows and at what integrity level:

```json
"github": {
  "type": "stdio",
  "container": "ghcr.io/github/github-mcp-server:latest",
  "env": { "GITHUB_PERSONAL_ACCESS_TOKEN": "" },
  "guard-policies": {
    "allow-only": {
      "repos": ["github/gh-aw-mcpg", "github/gh-aw"],
      "min-integrity": "unapproved"
    }
  }
}
```

**`repos`** — Repository access scope:
- `"all"` — All repositories accessible by the token
- `"public"` — Public repositories only
- `["owner/repo"]` — Exact match
- `["owner/*"]` — All repos under owner
- `["owner/prefix*"]` — Repos matching prefix

**`min-integrity`** — Minimum integrity level required for content items. Levels from highest to lowest:
- `"merged"` — Objects reachable from main branch
- `"approved"` — Members (OWNER, MEMBER, COLLABORATOR); private repo items; trusted bots
- `"unapproved"` — Contributors (CONTRIBUTOR, FIRST_TIME_CONTRIBUTOR)
- `"none"` — All objects (FIRST_TIMER, NONE)
- `blocked` — Items from `blocked-users` (always denied; not a configurable value)

**`blocked-users`** *(optional)* — Array of GitHub usernames whose content is unconditionally blocked. Items from these users receive `blocked` integrity (below `none`) and are always denied, even when `min-integrity` is `"none"`. Cannot be overridden by `approval-labels`.

**`approval-labels`** *(optional)* — Array of GitHub label names that promote a content item's effective integrity to `approved` when present. Enables human-review gates where a maintainer labels an item to allow it through. Uses `max(base, approved)` so it never lowers integrity. Does not override `blocked-users`.

**`trusted-users`** *(optional)* — Array of GitHub usernames whose content is unconditionally elevated to `approved` integrity. Useful for granting specific external contributors (e.g., trusted open-source maintainers) the same treatment as repository members, without lowering `min-integrity` globally. Uses `max(base, approved)` so it never lowers integrity. Does not override `blocked-users`.

**`tool-call-limits`** *(optional)* — Map of tool names to per-session call limits enforced by the gateway before the backend is invoked. Positive values hard-limit that tool for the session, while `0` or an omitted entry leaves the tool unlimited.

```json
"guard-policies": {
  "allow-only": {
    "repos": ["myorg/*"],
    "min-integrity": "approved",
    "tool-call-limits": {"issue_read": 1},
    "blocked-users": ["spam-bot", "compromised-user"],
    "approval-labels": ["human-reviewed", "safe-for-agent"],
    "trusted-users": ["alice", "trusted-contributor"]
  }
}
```

For comprehensive documentation on integrity filtering, see the [Integrity Filtering Reference](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/integrity.md).

### write-sink (output servers)

**Required for ALL output servers** when guards are enabled. Marks a server as a write-only channel that accepts writes from agents with matching secrecy labels:

```json
"safeoutputs": {
  "type": "stdio",
  "container": "ghcr.io/github/safe-outputs:latest",
  "guard-policies": {
    "write-sink": {
      "accept": ["private:github/gh-aw-mcpg", "private:github/gh-aw"]
    }
  }
}
```

The `accept` entries must match the secrecy tags assigned by the guard. Key mappings:

| `allow-only.repos` | `write-sink.accept` |
|---|---|
| `"all"` or `"public"` | `["*"]` |
| `["owner/repo"]` | `["private:owner/repo"]` |
| `["owner/*"]` | `["private:owner"]` |
| `["owner/prefix*"]` | `["private:owner/prefix*"]` |

See **[docs/CONFIGURATION.md](docs/CONFIGURATION.md)** for the complete mapping table and accept pattern reference.

## Architecture

```
                    ┌─────────────────────────────────────┐
                    │           MCP Gateway               │
  Client ──────────▶  /mcp/{serverID}  (routed mode)      │
  (JSON-RPC 2.0)    │ /mcp             (unified mode)     │
                    │                                     │
                    │  ┌─────────────┐  ┌──────────────┐  │
                    │  │ Guards      │  │ Auth (7.1)   │  │
                    │  │ (WASM)      │  │ API Key      │  │
                    │  └──────┬──────┘  └──────────────┘  │
                    │         │                           │
                    │  ┌──────▼──────┐  ┌──────────────┐  │
                    │  │ GitHub MCP  │  │ Safe Outputs │  │
                    │  │ (stdio/     │  │ (write-sink) │  │
                    │  │  Docker)    │  │              │  │
                    │  └─────────────┘  └──────────────┘  │
                    └─────────────────────────────────────┘
```

**Transport**: JSON-RPC 2.0 over stdio (containerized Docker) or HTTP (session state preserved)

**Routing**: Routed mode (`/mcp/{serverID}`) exposes each backend at its own endpoint. Unified mode (`/mcp`) routes to all configured servers through a single endpoint.

**Security**: WASM-based DIFC guards enforce secrecy and integrity labels per request. Guards are loaded from `MCP_GATEWAY_WASM_GUARDS_DIR` and assigned per-server. Authentication uses plain API keys per MCP spec 7.1 (`Authorization: <api-key>`).

**Logging**: Per-server log files (`{serverID}.log`) and unified `mcp-gateway.log` use bracketed UTC ISO-8601 timestamps with milliseconds (`[YYYY-MM-DDTHH:mm:ss.SSSZ]`). Machine-readable `rpc-messages.jsonl` records include required `timestamp`, `event` (`snake_case`), and `_schema` fields; RPC events use `_schema: "rpc-message/v2"` with `event` values `rpc_request`/`rpc_response`, and DIFC filter records use `_schema: "difc-filtered/v2"` with `event: "difc_filtered"`. Markdown workflow previews (`gateway.md`) and the wazero cache (`<parent-of-log-dir>/wazero-cache`, a sibling of `--log-dir` by default) are also produced.

## API Endpoints

- `POST /mcp/{serverID}` — Routed mode (default): JSON-RPC request to specific server
- `POST /mcp` — Unified mode: JSON-RPC request routed to configured servers
- `GET /health` — Health check; returns JSON `{"status":"healthy" | "unhealthy","specVersion":"...","gatewayVersion":"...","servers":{...}}`

Supported MCP methods: `tools/list`, `tools/call`, and any other method (forwarded as-is).

## Proxy Mode

The gateway can also run as an HTTP forward proxy (`awmg proxy`) that intercepts GitHub API requests from tools like `gh` CLI and applies the same DIFC filtering:

```bash
awmg proxy \
  --guard-wasm guards/github-guard/github_guard.wasm \
  --policy '{"allow-only":{"repos":["org/repo"],"min-integrity":"approved"}}' \
  --github-token "$GITHUB_TOKEN" \
  --listen localhost:8080
```

This maps ~25 REST URL patterns and GraphQL queries to guard tool names, then runs the same 6-phase DIFC pipeline used by the MCP gateway. See [docs/PROXY_MODE.md](docs/PROXY_MODE.md) for full documentation.

## Further Reading

| Topic | Link |
|-------|------|
| **Proxy Mode** | [docs/PROXY_MODE.md](docs/PROXY_MODE.md) — HTTP forward proxy for DIFC filtering of `gh` CLI and REST/GraphQL requests |
| **Integrity Filtering** | [Integrity Filtering Reference](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/integrity.md) — Integrity levels, blocked-users, approval-labels, and filtering configuration |
| **Configuration Reference** | [docs/CONFIGURATION.md](docs/CONFIGURATION.md) — Server fields, TOML/JSON formats, guard-policy details, custom schemas, gateway fields, validation rules |
| **Environment Variables** | [docs/ENVIRONMENT_VARIABLES.md](docs/ENVIRONMENT_VARIABLES.md) — All env vars for production, development, Docker, and guard configuration |
| **Full Specification** | [MCP Gateway Configuration Reference](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/mcp-gateway.md) — Upstream spec with complete validation rules |
| **Guard Response Labeling** | [docs/GUARD_RESPONSE_LABELING.md](docs/GUARD_RESPONSE_LABELING.md) — How guards label MCP responses with secrecy/integrity tags |
| **HTTP Backend Sessions** | [docs/HTTP_BACKEND_SESSION_ID.md](docs/HTTP_BACKEND_SESSION_ID.md) — Session ID management for HTTP transport backends |
| **Architecture Patterns** | [docs/MCP_SERVER_ARCHITECTURE_PATTERNS.md](docs/MCP_SERVER_ARCHITECTURE_PATTERNS.md) — MCP server design patterns and compatibility |
| **Gateway Compatibility** | [docs/GATEWAY_COMPATIBILITY_QUICK_REFERENCE.md](docs/GATEWAY_COMPATIBILITY_QUICK_REFERENCE.md) — Quick reference for gateway compatibility |
| **Security Model** | [docs/aw-security.md](docs/aw-security.md) — Security architecture overview |
| **Contributing** | [CONTRIBUTING.md](CONTRIBUTING.md) — Development setup, building, testing, project structure |

## License

MIT License
