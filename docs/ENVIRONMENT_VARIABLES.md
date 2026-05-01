# Environment Variables

Complete reference for MCP Gateway environment variables.

## Required for Production (Containerized Mode)

When running in a container (`run_containerized.sh`), these variables **must** be set:

| Variable | Description | Example |
|----------|-------------|---------|
| `MCP_GATEWAY_PORT` | The port the gateway listens on (used for `--listen` address) | `8080` |
| `MCP_GATEWAY_DOMAIN` | The domain name for the gateway | `localhost` |
| `MCP_GATEWAY_API_KEY` | API key checked by `run_containerized.sh` as a deployment gate; must be referenced in your JSON config via `"${MCP_GATEWAY_API_KEY}"` to enable authentication | `your-secret-key` |

## Optional (Non-Containerized Mode)

When running locally (`run.sh`), these variables are optional (warnings shown if missing):

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_GATEWAY_PORT` | Gateway listening port | `8000` |
| `MCP_GATEWAY_DOMAIN` | Gateway domain | `localhost` |
| `MCP_GATEWAY_API_KEY` | Informational only — not read directly by the binary; must be referenced in your config via `"${MCP_GATEWAY_API_KEY}"` to enable authentication | (disabled) |
| `MCP_GATEWAY_LOG_DIR` | Log file directory (sets default for `--log-dir` flag) | `/tmp/gh-aw/mcp-logs` |
| `MCP_GATEWAY_PAYLOAD_DIR` | Large payload storage directory (sets default for `--payload-dir` flag) | `/tmp/jq-payloads` |
| `MCP_GATEWAY_PAYLOAD_PATH_PREFIX` | Path prefix for remapping payloadPath returned to clients (sets default for `--payload-path-prefix` flag) | (empty - use actual filesystem path) |
| `MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD` | Size threshold in bytes for payload storage (sets default for `--payload-size-threshold` flag) | `524288` |
| `MCP_GATEWAY_SESSION_TIMEOUT` | Session timeout for stateful sessions in both unified (`/mcp`) and routed (`/mcp/<server>`) modes. Accepts Go duration strings (e.g., `30m`, `1h`). Default is 6 hours to match the GitHub Actions default timeout. | `6h` |
| `MCP_GATEWAY_TOOL_TIMEOUT` | Tool invocation timeout in seconds. Used as fallback when `toolTimeout` is not set in the stdin JSON config. Accepts integers in the range `10`–`600`. Priority: stdin `toolTimeout` > this env var > built-in default. | `60` |
| `MCP_GATEWAY_WASM_GUARDS_DIR` | Root directory for per-server WASM guards (`<root>/<serverID>/*.wasm`, first match is loaded) | (disabled) |
| `MCP_GATEWAY_GUARDS_MODE` | Guards enforcement mode: `strict` (deny violations), `filter` (remove denied tools), `propagate` (auto-adjust agent labels) (sets default for `--guards-mode`) | `strict` |
| `MCP_GATEWAY_GUARDS_SINK_SERVER_IDS` | Comma-separated sink server IDs for JSONL guards tag enrichment (sets default for `--guards-sink-server-ids`) | (disabled) |
| `MCP_GATEWAY_TLS_CERT` | Path to TLS server certificate PEM file. When set together with `MCP_GATEWAY_TLS_KEY`, enables HTTPS. Sets default for `--tls-cert`. | (disabled) |
| `MCP_GATEWAY_TLS_KEY` | Path to TLS server private key PEM file. Required when `MCP_GATEWAY_TLS_CERT` is set. Sets default for `--tls-key`. | (disabled) |
| `MCP_GATEWAY_CA_CERT` | Path to CA certificate PEM file for client certificate verification. When set (requires `MCP_GATEWAY_TLS_CERT`/`MCP_GATEWAY_TLS_KEY`), enables mutual TLS (mTLS). Sets default for `--tls-ca`. | (disabled) |
| `MCP_GATEWAY_HMAC_SECRET` | Shared HMAC-SHA256 secret for request signing and replay protection on MCP request endpoints (for example, `/mcp` and `/mcp/<server>`). When set, those MCP requests must carry valid `X-MCP-Timestamp`, `X-MCP-Nonce`, and `X-MCP-Signature` headers. Sets default for `--hmac-secret`. | (disabled) |
| `DEBUG` | Enable debug logging with pattern matching (e.g., `*`, `server:*,launcher:*`) | (disabled) |
| `DEBUG_COLORS` | Control colored debug output (0 to disable, auto-disabled when piping) | Auto-detect |
| `RUNNING_IN_CONTAINER` | Manual override; set to `"true"` to force container detection when `/.dockerenv` and cgroup detection are unavailable | (unset) |

**Note:** `PORT`, `HOST`, and `MODE` are not read by the `awmg` binary directly. However, `run.sh` uses `HOST` (default: `0.0.0.0`), `MODE` (default: `--routed`), and falls back to `PORT` (when `MCP_GATEWAY_PORT` is unset) to set the bind address and routing mode. Use the `--listen` and `--routed`/`--unified` flags when running `awmg` directly.

## Containerized Deployment Variables

When using `run_containerized.sh`, these additional variables are available:

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_GATEWAY_HOST` | Bind address for the gateway | `0.0.0.0` |
| `MCP_GATEWAY_MODE` | Routing mode flag passed to `awmg` (e.g., `--routed`, `--unified`) | `--routed` |

## Docker Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `DOCKER_HOST` | Docker daemon socket path | `/var/run/docker.sock` |
| `DOCKER_API_VERSION` | Docker API version (set by helper scripts, Docker client auto-negotiates) | Set by querying Docker daemon's current API version; falls back to `1.44` if detection fails |

## GitHub Authentication

These variables provide a GitHub token used by the proxy command (`awmg proxy`) **and** by the unified gateway mode (`/mcp`) for collaborator permission checks. The first non-empty value wins, checked in the priority order shown:

| Variable | Description | Default |
|----------|-------------|---------|
| `GITHUB_MCP_SERVER_TOKEN` | Highest-priority GitHub auth token. Useful when running alongside the raw GitHub MCP server which also reads this variable. | (optional) |
| `GITHUB_TOKEN` | Standard GitHub token (set automatically in GitHub Actions) | (optional) |
| `GITHUB_PERSONAL_ACCESS_TOKEN` | Personal access token | (optional) |
| `GH_TOKEN` | Lowest-priority fallback (set by GitHub CLI) | (optional) |

> **Note:** For proxy mode, one of these variables (or `--token`) is only needed when you want a fallback token for upstream authentication—for example, when clients do not send an `Authorization` header. In unified gateway mode, `get_collaborator_permission` requires one of these variables to be set. See `internal/envutil/github.go` for the lookup implementation.

## Proxy Mode Variables

When running `awmg proxy`, these variables configure the upstream GitHub API:

| Variable | Description | Default |
|----------|-------------|---------|
| `GITHUB_API_URL` | Explicit GitHub API endpoint (e.g., `https://copilot-api.mycompany.ghe.com`); used by proxy to set upstream target | (auto-derived) |
| `GITHUB_SERVER_URL` | GitHub server URL; proxy auto-derives API endpoint: `*.ghe.com` → `copilot-api.*.ghe.com`, GHES → `<host>/api/v3`, `github.com` → `api.github.com` | (falls back to `api.github.com`) |

## GitHub Actions OIDC Variables

When any HTTP server uses `auth.type = "github-oidc"`, the gateway reads these environment variables (set automatically by the GitHub Actions runner when `permissions: { id-token: write }` is granted):

| Variable | Description | Default |
|----------|-------------|---------|
| `ACTIONS_ID_TOKEN_REQUEST_URL` | GitHub Actions OIDC token endpoint. Required when any HTTP server uses `auth.type = "github-oidc"`. | (set by GitHub Actions) |
| `ACTIONS_ID_TOKEN_REQUEST_TOKEN` | Bearer token used to authenticate the OIDC token request. Used alongside `ACTIONS_ID_TOKEN_REQUEST_URL`. | (set by GitHub Actions) |

## DIFC / Guard Policy Configuration

These environment variables configure guard policies (e.g., AllowOnly policies for restricting tool access to specific GitHub repositories):

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_GATEWAY_GUARD_POLICY_JSON` | Guard policy JSON (e.g., `{"allow-only":{"repos":"public","min-integrity":"none"}}`) (sets default for `--guard-policy-json`) | (disabled) |
| `MCP_GATEWAY_ALLOWONLY_SCOPE_PUBLIC` | Use public AllowOnly scope (sets default for `--allowonly-scope-public`) | `false` |
| `MCP_GATEWAY_ALLOWONLY_SCOPE_OWNER` | AllowOnly owner scope value (sets default for `--allowonly-scope-owner`) | (disabled) |
| `MCP_GATEWAY_ALLOWONLY_SCOPE_REPO` | AllowOnly repo name (requires owner) (sets default for `--allowonly-scope-repo`) | (disabled) |
| `MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY` | AllowOnly integrity level: `none`, `unapproved`, `approved`, `merged` (sets default for `--allowonly-min-integrity`) | (disabled) |

## OpenTelemetry / Tracing Variables

These standard OpenTelemetry environment variables set defaults for the corresponding `--otlp-*` CLI flags:

| Variable | Description | Default |
|----------|-------------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP HTTP endpoint for trace export (e.g., `http://localhost:4318`). Tracing is disabled when empty. Sets default for `--otlp-endpoint`. | (disabled) |
| `OTEL_SERVICE_NAME` | Service name reported in traces. Sets default for `--otlp-service-name`. | `mcp-gateway` |
