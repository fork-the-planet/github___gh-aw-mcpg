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
       "agentId": "${MCP_GATEWAY_AGENT_ID}"
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
     -e MCP_GATEWAY_AGENT_ID=your-agent-id \
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
  - For backward compatibility, JSON stdin also accepts legacy snake_case server timeout aliases: `connect_timeout` and `tool_timeout` (prefer `connectTimeout` and `toolTimeout`).
- `--env <file>`: Load environment variables from a `.env` file before startup.
- `-v`, `-vv`, `-vvv`: Increase verbosity (`info`, `debug`, `trace`).
- A complete reference for all environment variables — including guard policy, TLS, tracing, authentication tokens, and containerized deployment — is in [docs/ENVIRONMENT_VARIABLES.md](docs/ENVIRONMENT_VARIABLES.md).

Common operational environment variables include:
- `MCP_GATEWAY_DOMAIN` — gateway domain used by containerized startup checks and commonly referenced from config
- `MCP_GATEWAY_LOG_DIR` — log file directory (default: `/tmp/gh-aw/mcp-logs`)
- `MCP_GATEWAY_PAYLOAD_DIR` — large payload storage directory (must be absolute path; default: `/tmp/jq-payloads`)
- `MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD` — size threshold in bytes for payload storage (default: `524288`)
- `MCP_GATEWAY_SESSION_TIMEOUT` — session timeout for stateful unified/routed MCP sessions (default: `6h`)
- `MCP_GATEWAY_TOOL_TIMEOUT` — global tool invocation timeout fallback when JSON stdin `gateway.toolTimeout` is not set (built-in default: `60`)
- `MCP_GATEWAY_FORCE_PUBLIC_REPOS` — when `true` (default), auto-forces `repos="public"` allow-only policy when workflow repo is public
- `MCP_GATEWAY_AGENT_ID` — agent identifier for env validation and containerized startup checks
- `MCP_GATEWAY_API_KEY` — *deprecated alias for `MCP_GATEWAY_AGENT_ID`*; still accepted with a deprecation warning, prefer `MCP_GATEWAY_AGENT_ID`
- `ACTIONS_ID_TOKEN_REQUEST_URL`, `ACTIONS_ID_TOKEN_REQUEST_TOKEN` — required for `github-oidc` auth type (set automatically by GitHub Actions)
- `DOCKER_HOST` — Docker daemon socket path (default: `/var/run/docker.sock`)
- `RUNNING_IN_CONTAINER` — set to `"true"` to force container detection when `/.dockerenv` and cgroup detection are unavailable
- `MCP_GATEWAY_SHUTDOWN_TIMEOUT`
- `MCP_GATEWAY_WASM_CACHE_DIR`
- `MCP_GATEWAY_PAYLOAD_PATH_PREFIX`
- `MCP_GATEWAY_URL_DOMAIN_AUDIT`
- `MCP_GATEWAY_TLS_CERT`, `MCP_GATEWAY_TLS_KEY`, `MCP_GATEWAY_CA_CERT`, `MCP_GATEWAY_HMAC_SECRET`
- `MCP_GATEWAY_GUARD_POLICY_JSON`, `MCP_GATEWAY_GUARDS_SINK_SERVER_IDS`

## Authentication

The gateway reads a GitHub token from the first non-empty value of these variables (checked in priority order):

| Variable | Description |
|----------|-------------|
| `GITHUB_MCP_SERVER_TOKEN` | Highest-priority GitHub auth token |
| `GITHUB_TOKEN` | Standard GitHub token (set automatically in GitHub Actions) |
| `GITHUB_PERSONAL_ACCESS_TOKEN` | Personal access token |
| `GH_TOKEN` | Lowest-priority fallback (set by GitHub CLI) |

For proxy mode, a token is needed only when clients do not send their own `Authorization` header. In unified gateway mode, collaborator permission checks require one of these to be set. See [`docs/ENVIRONMENT_VARIABLES.md`](docs/ENVIRONMENT_VARIABLES.md) for full details.

## Tracing

The gateway supports OpenTelemetry distributed tracing. Set these variables to enable it:

| Variable | Description |
|----------|-------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP HTTP endpoint (e.g., `http://localhost:4318`); tracing is disabled when empty |
| `OTEL_EXPORTER_OTLP_HEADERS` | Comma-separated `key=value` headers for OTLP export (W3C Baggage format); used as fallback when not set in TOML config. With JSON stdin, use this variable for shared OTLP headers because `gateway.opentelemetry.headers` is not supported there |
| `GH_AW_OTLP_ENDPOINTS` | Comma-separated OTLP URLs (or JSON array with per-endpoint `headers`) for multi-backend fan-out; all listed endpoints receive every span. Takes precedence over `OTEL_EXPORTER_OTLP_ENDPOINT`. |
| `OTEL_SERVICE_NAME` | Service name reported in traces (default: `mcp-gateway`) |

Use `--otlp-sample-rate <float>` to control trace sampling (range `0.0`–`1.0`, default `1.0`).

See [`docs/ENVIRONMENT_VARIABLES.md`](docs/ENVIRONMENT_VARIABLES.md) for full details.

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

## Gateway Configuration

Key configuration fields (gateway-level under `[gateway]` in TOML / `"gateway"` in JSON, plus top-level JSON stdin fields):

| Field | Description |
|-------|-------------|
| `agent_id` / `agentId` | Agent/session identifier used for routing and optional auth matching |
| `api_key` / `apiKey` | Deprecated alias for `agent_id` / `agentId` (accepted with warnings) |
| `port` | Metadata only; validated (1–65535) but does not control the listen address. Use the `--listen` flag to set the listen address. `MCP_GATEWAY_PORT` is read by `--validate-env` only as a required-variable presence check; port-mapping and listen-address construction are handled by wrapper scripts (`run.sh`, `run_containerized.sh`). |
| `domain` | Gateway domain for external access (for example `"localhost"` in TOML/JSON, or `"${MCP_GATEWAY_DOMAIN}"` in JSON stdin where `${...}` expansion is supported) |
| `startup_timeout` / `startupTimeout` | Seconds to wait for backend server startup (default `30`) |
| `tool_timeout` / `toolTimeout` | Seconds to wait for tool execution (default `60`; JSON stdin only: env fallback `MCP_GATEWAY_TOOL_TIMEOUT`) |
| `keepalive_interval` / `keepaliveInterval` | Interval in seconds between keepalive pings sent to HTTP backends (`-1` disables keepalive; default `1500`) |
| `payload_dir` / `payloadDir` | Directory for large payload storage (must be absolute path) |
| `payload_path_prefix` / `payloadPathPrefix` | Optional path prefix used when returning `payloadPath` values to clients (for remapped/mounted payload directories) |
| `payload_size_threshold` / `payloadSizeThreshold` | Size threshold in bytes for payload storage (default: `524288`) |
| `trusted_bots` / `trustedBots` | Additional bot usernames to treat as trusted with "approved" integrity. Additive to the built-in trusted bot list. Non-empty array when present. Example: `["my-bot[bot]"]` |
| `force_public_repos` / `forcePublicRepos` | Enables/disables auto-forcing allow-only policy to `repos="public"` when the workflow repository is public (default enabled) |
| `sink_visibility_exempt_servers` / `sinkVisibilityExemptServers` | Server IDs exempted from default sink-visibility enforcement for write-sink handling |
| `[gateway.opentelemetry]` / `[gateway.tracing]` (TOML), `gateway.opentelemetry` (JSON stdin) | Nested OpenTelemetry tracing configuration blocks (`opentelemetry` is preferred; legacy TOML `tracing` is still supported) |
| `guards` (JSON stdin top-level / TOML top-level) | Optional guard definitions and policy configuration used for DIFC enforcement |
| `customSchemas` (JSON stdin top-level) | Map custom server `type` names to HTTPS JSON schema URLs for custom server validation |

For the full gateway field list (including rate limiting, tracing, keepalive, and more), see **[docs/CONFIGURATION.md](docs/CONFIGURATION.md)**.

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

**Security**: WASM-based DIFC guards enforce secrecy and integrity labels per request. Guards are loaded from `MCP_GATEWAY_WASM_GUARDS_DIR` and assigned per-server. Authentication uses the configured agent identifier value per MCP spec 7.1 (`Authorization: <agent-id>`), and session routing can also use `X-Agent-ID`.

**Logging**: Per-server log files (`{serverID}.log`) and unified `mcp-gateway.log` use bracketed UTC ISO-8601 timestamps with milliseconds (`[YYYY-MM-DDTHH:mm:ss.SSSZ]`). Machine-readable `rpc-messages.jsonl` records include required `timestamp`, `event` (`snake_case`), and `_schema` fields; RPC events use `_schema: "rpc-message/v2"` with `event` values `rpc_request`/`rpc_response`, and DIFC filter records use `_schema: "difc-filtered/v2"` with `event: "difc_filtered"`. Markdown workflow previews (`gateway.md`) and the wazero cache (`<parent-of-log-dir>/wazero-cache`, a sibling of `--log-dir` by default) are also produced.

## API Endpoints

- `POST /mcp/{serverID}` — Routed mode (default): JSON-RPC request to specific server
- `POST /mcp` — Unified mode: JSON-RPC request routed to configured servers
- `GET /health` — Health check; returns JSON `{"status":"healthy" | "unhealthy","specVersion":"...","gatewayVersion":"...","servers":{...}}`
- `POST /close` — Graceful shutdown; terminates all backend servers and exits the process (auth-protected when agent ID is configured; not HMAC-protected)
- `GET /reflect` — Unauthenticated DIFC label snapshot for all known agents (gateway and proxy mode)

### `GET /reflect` response schema

```json
{
  "agents": {
    "<agent-id>": {
      "secrecy": ["<tag>"],
      "integrity": ["<tag>"]
    }
  },
  "mode": "strict|filter|propagate",
  "timestamp": "RFC3339 UTC timestamp"
}
```

- `agents`: map keyed by agent ID with current DIFC labels.
- `secrecy`/`integrity`: sorted arrays of label tags (empty array when none).
- `mode`: active DIFC enforcement mode.
- `timestamp`: snapshot generation time in UTC (`time.RFC3339`).

> Security note: `/reflect` is intentionally unauthenticated for local/runtime debugging and operational introspection. It exposes active agent IDs and current DIFC labels, so operators should only expose the gateway/proxy on trusted networks.

Supported MCP methods: `tools/list`, `tools/call` (proxied to backend servers), plus standard lifecycle methods (`initialize`, etc.) handled natively by the MCP SDK.

## Proxy Mode

The gateway can also run as an HTTP forward proxy (`awmg proxy`) that intercepts GitHub API requests from tools like `gh` CLI and applies the same DIFC filtering:

```bash
awmg proxy \
  --guard-wasm guards/github-guard/github_guard.wasm \
  --policy '{"allow-only":{"repos":["org/repo"],"min-integrity":"approved"}}' \
  --github-token "$GITHUB_TOKEN" \
  --listen localhost:8080
```

This maps ~50 REST URL patterns and ~15 GraphQL query patterns to guard tool names, then runs the same 6-phase DIFC pipeline used by the MCP gateway. See [docs/PROXY_MODE.md](docs/PROXY_MODE.md) for full documentation.

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
