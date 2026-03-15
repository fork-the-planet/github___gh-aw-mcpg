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

The gateway starts in routed mode on `http://0.0.0.0:8000`, proxying MCP requests to your configured backend servers.

**Required flags:**
- `-i`: Enables stdin for passing JSON configuration
- `-v /var/run/docker.sock`: Required for spawning backend MCP servers
- `-p 8000:8000`: Port mapping must match `MCP_GATEWAY_PORT`

## Guard Policies

Guard policies enforce **Data Information Flow Control (DIFC)** at the gateway level, restricting what data agents can access and where they can write. Each server can have either an `allow-only` or a `write-sink` policy.

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

**`min-integrity`** — Minimum integrity level based on `author_association`:
- `"none"` — All objects (FIRST_TIME_CONTRIBUTOR, FIRST_TIMER, NONE)
- `"unapproved"` — Contributors (CONTRIBUTOR, FIRST_TIME_CONTRIBUTOR)
- `"approved"` — Members (OWNER, MEMBER, COLLABORATOR)
- `"merged"` — Objects reachable from main branch

### write-sink (output servers)

**Required for ALL output servers** when DIFC guards are enabled. Marks a server as a write-only channel that accepts writes from agents with matching secrecy labels:

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
  Client ──────────▶  /mcp/{serverID}  (routed mode)     │
  (JSON-RPC 2.0)   │  /mcp             (unified mode)    │
                    │                                     │
                    │  ┌─────────────┐  ┌──────────────┐  │
                    │  │ DIFC Guards │  │ Auth (7.1)   │  │
                    │  │ (WASM)      │  │ API Key      │  │
                    │  └──────┬──────┘  └──────────────┘  │
                    │         │                            │
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

**Logging**: Per-server log files (`{serverID}.log`), unified `mcp-gateway.log`, markdown workflow previews (`gateway.md`), and machine-readable `rpc-messages.jsonl`.

## API Endpoints

- `POST /mcp/{serverID}` — Routed mode (default): JSON-RPC request to specific server
- `POST /mcp` — Unified mode: JSON-RPC request routed to configured servers
- `GET /health` — Health check (returns `OK`)

Supported MCP methods: `tools/list`, `tools/call`, and any other method (forwarded as-is).

## Further Reading

| Topic | Link |
|-------|------|
| **Configuration Reference** | [docs/CONFIGURATION.md](docs/CONFIGURATION.md) — Server fields, TOML/JSON formats, guard-policy details, custom schemas, gateway fields, validation rules |
| **Environment Variables** | [docs/ENVIRONMENT_VARIABLES.md](docs/ENVIRONMENT_VARIABLES.md) — All env vars for production, development, Docker, and DIFC configuration |
| **Full Specification** | [MCP Gateway Configuration Reference](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/mcp-gateway.md) — Upstream spec with complete validation rules |
| **Guard Response Labeling** | [docs/GUARD_RESPONSE_LABELING.md](docs/GUARD_RESPONSE_LABELING.md) — How guards label MCP responses with secrecy/integrity tags |
| **HTTP Backend Sessions** | [docs/HTTP_BACKEND_SESSION_ID.md](docs/HTTP_BACKEND_SESSION_ID.md) — Session ID management for HTTP transport backends |
| **Architecture Patterns** | [docs/MCP_SERVER_ARCHITECTURE_PATTERNS.md](docs/MCP_SERVER_ARCHITECTURE_PATTERNS.md) — MCP server design patterns and compatibility |
| **Security Model** | [docs/aw-security.md](docs/aw-security.md) — Security architecture overview |
| **Contributing** | [CONTRIBUTING.md](CONTRIBUTING.md) — Development setup, building, testing, project structure |

## License

MIT License
