# AGENTS.md

Quick reference for AI agents working with MCP Gateway (Go-based MCP proxy server).

## Quick Start

**Install**: `make install` (install toolchains and dependencies)  
**Build**: `make build` (builds `awmg` binary)  
**Test**: `make test` (run unit tests, no build required)  
**Test-Unit**: `make test-unit` (run unit tests only)  
**Test-Integration**: `make test-integration` (run binary integration tests, auto-builds binary if not present)  
**Test-All**: `make test-all` (run both unit and integration tests)  
**Test-CI**: `make test-ci` (unit tests with coverage and JSON output for CI)  
**Lint**: `make lint` (runs go vet, gofmt checks, and golangci-lint)  
**Coverage**: `make coverage` (unit tests with coverage report)  
**Format**: `make format` (auto-format code with gofmt)  
**Clean**: `make clean` (remove build artifacts)  
**Agent-Finished**: `make agent-finished` (run format, build, lint, and all tests - ALWAYS run before completion)  
**Run**: `./awmg --config config.toml`  
**Run sequentially**: `./awmg --config config.toml --sequential-launch`  
**Run with Custom Log Directory**: `./awmg --config config.toml --log-dir /path/to/logs`  
**Run with Custom Payload Directory**: `./awmg --config config.toml --payload-dir /path/to/payloads`  

## Project Structure

- `internal/auth/` - Authentication header parsing and middleware
- `internal/cmd/` - CLI (Cobra)
- `internal/config/` - Config parsing (TOML/JSON) with validation; the package is split across multiple files
  - `config_core.go` - Core `Config`, `GatewayConfig`, and `ServerConfig` types plus TOML loading
  - `config_stdin.go` - JSON stdin structs and stdin→internal config conversion
  - `config_env.go`, `config_feature.go`, `config_tracing.go` - Environment- and feature-specific config helpers
  - `validation.go`, `validation_env.go`, `validation_schema.go` - Fail-fast field, environment, and schema validation
  - `validation_test.go` - Comprehensive validation tests
- `internal/difc/` - Decentralized Information Flow Control
- `internal/envutil/` - Environment variable utilities
- `internal/guard/` - Security guards (NoopGuard, WasmGuard, WriteSinkGuard)
- `internal/httputil/` - Shared HTTP helper utilities (server, proxy)
- `internal/launcher/` - Backend process management
- `internal/logger/` - Debug logging framework (micro logger)
- `internal/mcp/` - MCP protocol types with enhanced error logging
- `internal/middleware/` - HTTP middleware (jq schema processing)
- `internal/oidc/` - GitHub Actions OIDC token provider and caching
- `internal/proxy/` - Filtering HTTP proxy for the GitHub API with DIFC enforcement
- `internal/server/` - HTTP server (routed/unified modes)
- `internal/strutil/` - String and formatting utilities
- `internal/syncutil/` - Concurrency utilities
- `internal/sys/` - System utilities
- `internal/testutil/` - Test utilities and helpers
- `internal/tracing/` - OpenTelemetry tracing setup and OTLP export
- `internal/tty/` - Terminal detection utilities
- `internal/version/` - Version management

## Key Tech

- **Go 1.25.0** with `cobra`, `toml`, `go-sdk`
- **Protocol**: JSON-RPC 2.0 over stdio
- **Routing**: `/mcp/{serverID}` (routed) or `/mcp` (unified)
- **Docker**: Launches MCP servers as containers
- **Validation**: Spec-compliant with fail-fast error handling
- **Variable Expansion**: `${VAR_NAME}` syntax for environment variables

## Config Examples

**Configuration Spec**: See **[MCP Gateway Configuration Reference](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/mcp-gateway.md)** for complete specification.

**TOML** (`config.toml`):
```toml
[gateway]
port = 3000
api_key = "your-api-key"
payload_dir = "/tmp/jq-payloads"  # Optional: directory for large payload storage (must be absolute)

[servers.github]
command = "docker"
args = ["run", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN", "-i", "ghcr.io/github/github-mcp-server:latest"]
```

**JSON** (stdin):
```json
{
  "mcpServers": {
    "github": {
      "type": "stdio",
      "container": "ghcr.io/github/github-mcp-server:latest",
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "",
        "CONFIG_PATH": "${GITHUB_CONFIG_DIR}"
      }
    }
  }
}
```

**Supported Types**: `"stdio"`, `"http"` (fully supported), `"local"` (alias for stdio)

**Validation Features**:
- Environment variable expansion: `${VAR_NAME}` (fails if undefined)
- Required fields: `container` for stdio, `url` for http
- **Containerization Requirement**: TOML stdio servers must use `command = "docker"` per [MCP Gateway Specification Section 3.2.1](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/mcp-gateway.md#321-containerization-requirement)
- **Note**: In JSON stdin format, the `command` field is not supported - stdio servers must use `container` field
- **Note**: In JSON stdin format, `args` is optional and provides extra Docker runtime arguments inserted before the container image name
- Port range validation: 1-65535
- Timeout validation: positive integers only

## Go Conventions

- Internal packages in `internal/`
- Test files: `*_test.go` with table-driven tests
- Naming: camelCase (private), PascalCase (public)
- Always handle errors explicitly
- Godoc comments for exports
- Mock external dependencies (Docker, network)

## Testing with Testify

**ALWAYS use testify for test assertions** - The project uses [stretchr/testify](https://github.com/stretchr/testify) for all test assertions.

### Assert vs Require

- **`require`**: Use for critical checks - test stops on failure
- **`assert`**: Use for non-critical checks - test continues on failure

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestExample(t *testing.T) {
    result, err := DoSomething()
    require.NoError(t, err)  // Stop if error - can't continue
    assert.Equal(t, "expected", result.Field)  // Continue even if fails
}
```

### Bound Asserters

For tests with multiple assertions, use bound asserters to reduce repetition:

```go
func TestMultipleAssertions(t *testing.T) {
    assert := assert.New(t)
    require := require.New(t)
    
    result := GetResult()
    require.NotNil(result)  // Stop if nil
    
    // Cleaner - no need to pass 't' repeatedly
    assert.Equal("value1", result.Field1)
    assert.Equal("value2", result.Field2)
    assert.True(result.Active)
}
```

### Specific Assertion Methods

Use specific assertion methods instead of generic ones for better error messages:

```go
// ❌ Avoid generic assertions
assert.True(t, len(items) == 0)
assert.True(t, err == nil)
assert.True(t, strings.Contains(msg, "error"))

// ✅ Use specific assertions
assert.Empty(t, items)
assert.NoError(t, err)
assert.Contains(t, msg, "error")
```

### Common Patterns

```go
// Length checking
assert.Len(t, items, 5, "Expected 5 items")

// Unordered slice comparison
assert.ElementsMatch(t, expected, actual, "Slices should contain same elements")

// Nil checking
assert.NotNil(t, obj, "Object should not be nil")
assert.Nil(t, err, "Error should be nil")

// Error checking (prefer NoError over Nil for errors)
assert.NoError(t, err, "Operation should succeed")
assert.Error(t, err, "Operation should fail")

// HTTP status codes
assert.Equal(t, http.StatusOK, response.StatusCode, "Should return 200 OK")

// JSON comparison (ignores formatting)
assert.JSONEq(t, expectedJSON, actualJSON)
```

## Linting

**golangci-lint** is integrated and runs as part of `make lint`:
- Configuration: `.golangci.yml` (version 2 format)
- Enabled linters: `misspell`, `unconvert`
- Disabled linters: `gosec`, `testifylint`, `errcheck`, `gocritic`, `revive`
- Install: `make install` (installs golangci-lint v2.8.0)
- Run manually: `golangci-lint run --timeout=5m`

**testifylint**: Available but disabled due to requiring extensive test refactoring across the codebase.
- Automatically catches common testify mistakes:
  - Suggests `assert.Empty(t, x)` instead of `assert.True(t, len(x) == 0)`
  - Suggests `assert.True(t, x)` instead of `assert.Equal(t, true, x)`
  - Suggests `assert.NoError(t, err)` instead of `assert.Nil(t, err)`
- To run on specific files: `golangci-lint run --enable=testifylint --timeout=5m <files>`
- To run on entire codebase: `golangci-lint run --enable=testifylint --timeout=5m`

**Note**: Some linters (gosec, testifylint, errcheck) are disabled to minimize noise. Enable them for stricter checks:
```bash
golangci-lint run --enable=gosec,testifylint,errcheck --timeout=5m
```

## Test Structure

**Unit Tests** (`internal/` packages):
- Run without building the binary
- Test code in isolation with mocks
- Fast execution, no external dependencies
- Run with: `make test` or `make test-unit`

**Integration Tests** (`test/integration/`):
- Test the compiled `awmg` binary end-to-end
- Require building the binary first
- Test actual server behavior and CLI flags
- Run with: `make test-integration`

**All Tests**: `make test-all` runs both unit and integration tests

## Common Tasks

**Add MCP Server**: Update config.toml with new server entry  
**Add Route**: Edit `internal/server/routed.go` or `unified.go`  
**Add Guard**: Implement in `internal/guard/` and register  
**Add Auth Logic**: Implement in `internal/auth/` package  
**Add Unit Test**: Create `*_test.go` in the appropriate `internal/` package  
**Add Integration Test**: Create test in `test/integration/` that uses the binary

## Agent Completion Checklist

**CRITICAL: Before returning to the user, ALWAYS run `make agent-finished`**

This command runs the complete verification pipeline:
1. **Format** - Auto-formats all Go code with gofmt
2. **Build** - Ensures the project compiles successfully
3. **Lint** - Runs go vet and gofmt checks
4. **Test All** - Executes the full test suite (unit + integration tests)

**Requirements:**
- **ALL failures must be fixed** before completion
- If `make agent-finished` fails at any stage, debug and fix the issue
- Re-run `make agent-finished` after fixes to verify success
- Only report completion to the user after seeing "✓ All agent-finished checks passed!"

**Example workflow:**
```bash
# Make your code changes
# ...

# Run verification before completion
make agent-finished

# If any step fails, fix the issues and run again
# Only complete the task after all checks pass
```

## Debug Logging

**ALWAYS use the logger package for debug logging:**

```go
import "github.com/github/gh-aw-mcpg/internal/logger"

// Create a logger with namespace following pkg:filename convention
var log = logger.New("pkg:filename")

// Log debug messages
// - Writes to stderr with colors and time diffs (when DEBUG matches namespace)
// - Also writes to file logger as text-only (always, when logger is enabled)
log.Printf("Processing %d items", count)
log.Print("Simple debug message")

// Check if logging is enabled before expensive operations
if log.Enabled() {
    log.Printf("Expensive debug info: %+v", expensiveOperation())
}
```

**For operational/file logging, use the file logger directly:**

```go
import "github.com/github/gh-aw-mcpg/internal/logger"

// Log operational events (written to mcp-gateway.log)
logger.LogInfo("category", "Operation completed successfully")
logger.LogWarn("category", "Potential issue detected: %s", issue)
logger.LogError("category", "Operation failed: %v", err)
logger.LogDebug("category", "Debug details: %+v", details)
```

**Note:** Debug loggers created with `logger.New()` now write to both stderr (with colors/time diffs) and the file logger (text-only). This provides real-time colored output during development while ensuring all debug logs are captured to file for production troubleshooting.

**Logging Categories:**
- `startup` - Gateway initialization and configuration
- `shutdown` - Graceful shutdown events
- `client` - MCP client interactions and requests
- `backend` - Backend MCP server operations
- `auth` - Authentication events (success and failures)

**Category Naming Convention:**
- Follow the pattern: `pkg:filename` (e.g., `server:routed`, `launcher:docker`)
- Use colon (`:`) as separator between package and file/component name
- Be consistent with existing loggers in the codebase

**Logger Variable Naming Convention:**
- **Use descriptive names** that match the component: `var log<Component> = logger.New("pkg:component")`
- Examples: `var logLauncher = logger.New("launcher:launcher")`, `var logConfig = logger.New("config:config")`
- **Avoid generic `log` name** when it might conflict with standard library or when the file already imports `log` package
- Capitalize the component part after 'log' (e.g., `logAuth` with capital 'A', `logLauncher` with capital 'L')
- This convention makes it clear which logger is being used and reduces naming collisions
- For components with very short files or temporary code, generic `log` is acceptable but descriptive is preferred

**Examples of good logger naming:**
```go
// Descriptive - clearly indicates the component (RECOMMENDED)
var logLauncher = logger.New("launcher:launcher")
var logPool = logger.New("launcher:pool")
var logConfig = logger.New("config:config")
var logValidation = logger.New("config:validation")
var logUnified = logger.New("server:unified")
var logRouted = logger.New("server:routed")

// Generic - acceptable for simple cases but less clear
var log = logger.New("auth:header")
var log = logger.New("sys:sys")
```


**Debug Output Control:**
```bash
# Enable all debug logs
DEBUG=* ./awmg --config config.toml

# Enable specific package
DEBUG=server:* ./awmg --config config.toml

# Enable multiple packages
DEBUG=server:*,launcher:* ./awmg --config config.toml

# Exclude specific loggers
DEBUG=*,-launcher:test ./awmg --config config.toml

# Disable colors (auto-disabled when piping)
DEBUG_COLORS=0 DEBUG=* ./awmg --config config.toml
```

**Key Features:**
- **Zero overhead**: Logs only computed when DEBUG matches the logger's namespace
- **Time diff**: Shows elapsed time between log calls (e.g., `+50ms`, `+2.5s`)
- **Auto-colors**: Each namespace gets a consistent color in terminals
- **Pattern matching**: Supports wildcards (`*`) and exclusions (`-pattern`)

**When to Use:**
- Non-essential diagnostic information
- Performance insights and timing data
- Internal state tracking during development
- Detailed operation flow for debugging

**When NOT to Use:**
- Essential user-facing messages (use standard logging)
- Error messages (use proper error handling)
- Final output or results (use stdout)

## Environment Variables

- `GITHUB_MCP_SERVER_TOKEN` - Highest-priority GitHub auth token (takes precedence over `GITHUB_TOKEN`, `GITHUB_PERSONAL_ACCESS_TOKEN`, `GH_TOKEN`)
- `GITHUB_PERSONAL_ACCESS_TOKEN` - GitHub auth
- `GITHUB_API_URL` - Explicit GitHub API endpoint (e.g., `https://copilot-api.mycompany.ghe.com`); used by proxy to set upstream target
- `GITHUB_SERVER_URL` - GitHub server URL; proxy auto-derives API endpoint: `*.ghe.com` → `copilot-api.*.ghe.com`, GHES → `<host>/api/v3`, `github.com` → `api.github.com`
- `ACTIONS_ID_TOKEN_REQUEST_URL` - GitHub Actions OIDC token endpoint URL; required for `github-oidc` auth type
- `ACTIONS_ID_TOKEN_REQUEST_TOKEN` - GitHub Actions OIDC request token; required for `github-oidc` auth type
- `MCP_GATEWAY_PORT` - Used by environment validation (`--validate-env`) for container port-mapping checks (validated 1-65535); does not override the gateway listen address
- `MCP_GATEWAY_DOMAIN` - Used by environment validation (`--validate-env`) and containerized startup checks; to set config values use `gateway.domain` (or `"${MCP_GATEWAY_DOMAIN}"` in JSON stdin config)
- `MCP_GATEWAY_API_KEY` - Used by environment validation (`--validate-env`) and containerized startup checks; to enable auth set `gateway.apiKey` (commonly `"${MCP_GATEWAY_API_KEY}"` in JSON stdin config)
- `DEBUG` - Enable debug logging (e.g., `DEBUG=*`, `DEBUG=server:*,launcher:*`)
- `DEBUG_COLORS` - Control colored output (0 to disable, auto-disabled when piping)
- `MCP_GATEWAY_LOG_DIR` - Log file directory (sets default for `--log-dir` flag, default: `/tmp/gh-aw/mcp-logs`)
- `MCP_GATEWAY_WASM_CACHE_DIR` - Disk-backed wazero compilation cache directory (sets default for `--wasm-cache-dir`, default: `<log-dir>/wazero-cache`)
- `MCP_GATEWAY_PAYLOAD_DIR` - Large payload storage directory (sets default for `--payload-dir` flag, default: `/tmp/jq-payloads`). Must be an absolute path.
- `MCP_GATEWAY_PAYLOAD_PATH_PREFIX` - Path prefix for remapping payloadPath returned to clients (sets default for `--payload-path-prefix` flag, default: empty). In JSON stdin config use `gateway.payloadPathPrefix`.
- `MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD` - Size threshold in bytes for payload storage; payloads larger than this are stored to disk (sets default for `--payload-size-threshold` flag, default: `524288`)
- `MCP_GATEWAY_SESSION_TIMEOUT` - Session timeout for stateful sessions in both unified mode (`/mcp`) and routed mode (`/mcp/<server>`). Accepts Go duration strings (e.g., `30m`, `1h`, `2h30m`). (default: `6h`)
- `MCP_GATEWAY_TOOL_TIMEOUT` - Tool invocation timeout in seconds. Fallback when `gateway.toolTimeout` is not set in stdin JSON config. Accepts any integer ≥ 10 (no upper bound). Priority: stdin config > env var > built-in default. (default: `60`)
- `DOCKER_HOST` - Docker daemon socket path (default: `/var/run/docker.sock`)
- `MCP_GATEWAY_GUARDS_SINK_SERVER_IDS` - Comma-separated server IDs whose RPC JSONL logs should include agent secrecy/integrity tag snapshots (sets default for `--guards-sink-server-ids`)
- `MCP_GATEWAY_GUARDS_MODE` - Guards enforcement mode: `strict` (deny violations), `filter` (remove denied tools), `propagate` (auto-adjust agent labels) (sets default for `--guards-mode`, default: `strict`)
- `MCP_GATEWAY_WASM_GUARDS_DIR` - Root directory for per-server WASM guards (`<root>/<serverID>/*.wasm`, first match is loaded)
- `MCP_GATEWAY_GUARD_POLICY_JSON` - Guard policy JSON (e.g., `{"allow-only":{"repos":"public","min-integrity":"none"}}`) (sets default for `--guard-policy-json`)
- `MCP_GATEWAY_ALLOWONLY_SCOPE_PUBLIC` - Use public AllowOnly scope; set to `"true"` to enable (sets default for `--allowonly-scope-public`)
- `MCP_GATEWAY_ALLOWONLY_SCOPE_OWNER` - AllowOnly owner scope value (sets default for `--allowonly-scope-owner`)
- `MCP_GATEWAY_ALLOWONLY_SCOPE_REPO` - AllowOnly repo name, requires owner (sets default for `--allowonly-scope-repo`)
- `MCP_GATEWAY_ALLOWONLY_MIN_INTEGRITY` - AllowOnly integrity level: `none`, `unapproved`, `approved`, `merged` (sets default for `--allowonly-min-integrity`)
- `MCP_GATEWAY_TLS_CERT` - Path to TLS server certificate PEM file; enables HTTPS when set together with `MCP_GATEWAY_TLS_KEY` (sets default for `--tls-cert`)
- `MCP_GATEWAY_TLS_KEY` - Path to TLS server private key PEM file; required when `MCP_GATEWAY_TLS_CERT` is set (sets default for `--tls-key`)
- `MCP_GATEWAY_CA_CERT` - Path to CA certificate PEM file for client certificate verification; enables mutual TLS (mTLS) when set alongside `MCP_GATEWAY_TLS_CERT`/`MCP_GATEWAY_TLS_KEY` (sets default for `--tls-ca`)
- `MCP_GATEWAY_HMAC_SECRET` - Shared HMAC-SHA256 secret for request signing and replay protection; when set, requests to MCP handlers must carry valid `X-MCP-Timestamp`, `X-MCP-Nonce`, and `X-MCP-Signature` headers (sets default for `--hmac-secret`)
- `OTEL_EXPORTER_OTLP_ENDPOINT` - OTLP HTTP endpoint for trace export; sets default for `--otlp-endpoint`
- `OTEL_SERVICE_NAME` - Service name in traces; sets default for `--otlp-service-name`
- `AWMG_BINARY_PATH` - Override binary path for integration tests
- `AWMG_WASM_GUARD_PATH` - Override WASM guard path for proxy integration tests
- `RUNNING_IN_CONTAINER` - Set to `"true"` to force container detection when `/.dockerenv` and cgroup detection are unavailable

**Note:** `MCP_GATEWAY_PORT` is read by the `awmg` binary for environment validation (`--validate-env`) only. Plain `PORT`, `HOST`, and `MODE` are not read by `awmg` directly. However, `run.sh` uses `MCP_GATEWAY_PORT` (falling back to `PORT`), `HOST` (default: `0.0.0.0`), and `MODE` (default: `--routed`) to set the bind address and routing mode. Use the `--listen` and `--routed`/`--unified` flags when running `awmg` directly.

**File Logging:**
- Operational logs are always written to log files in the configured log directory
- Default log directory: `/tmp/gh-aw/mcp-logs` (configurable via `--log-dir` flag or `MCP_GATEWAY_LOG_DIR` env var)
- Falls back to stdout if log directory cannot be created
- **Log Files Created:**
  - `mcp-gateway.log` - Unified log with all messages
  - `{serverID}.log` - Per-server logs (e.g., `github.log`, `slack.log`) for easier troubleshooting
  - `gateway.md` - Markdown-formatted logs for GitHub workflow previews
  - `rpc-messages.jsonl` - Machine-readable JSONL format for RPC message analysis
  - `tools.json` - Available tools from all backend MCP servers (mapping server IDs to their tool names and descriptions)
- Logs include: startup, client interactions, backend operations, auth events, errors

**Per-ServerID Logging:**
- Each backend MCP server gets its own log file for easier troubleshooting
- Use the canonical `LogInfoToServer`, `LogWarnToServer`, `LogErrorToServer`, and `LogDebugToServer` functions
- Example: `logger.LogInfoToServer("github", "backend", "Server started successfully")`
- Logs are written to both the server-specific file and the unified `mcp-gateway.log`
- Thread-safe concurrent logging with automatic fallback

**Large Payload Handling:**
- Large tool response payloads are stored in the configured payload directory
- Default payload directory: `/tmp/jq-payloads` (configurable via `--payload-dir` flag, `MCP_GATEWAY_PAYLOAD_DIR` env var, or `payload_dir` in config). The configured path must be absolute.
- Payloads are organized by session ID: `{payload_dir}/{sessionID}/{queryID}/payload.json`
- This allows agents to mount their session-specific subdirectory to access full payloads
- The jq middleware returns: preview (first `PayloadPreviewSize` chars, default 500), schema, payloadPath, queryID, originalSize, truncated flag

**Understanding the payload.json File:**
- The `payload.json` file contains the **complete original response data** in valid JSON format
- You can read and parse this file directly using standard JSON parsing tools (e.g., `cat payload.json | jq .` or `JSON.parse(fs.readFileSync(path))`)
- The `payloadSchema` in the metadata response shows the **structure and types** of fields (e.g., "string", "number", "boolean", "array", "object")
- The `payloadSchema` does NOT contain the actual data values - those are only in the `payload.json` file
- The `payloadPreview` shows the first `PayloadPreviewSize` characters (default 500) of the JSON for quick reference
- To access the full data with all actual values, read the JSON file at `payloadPath`

**Tools Catalog (tools.json):**
- The gateway maintains a catalog of all available tools from backend MCP servers in `tools.json`
- Located in the log directory (e.g., `/tmp/gh-aw/mcp-logs/tools.json`)
- Updated automatically during gateway startup when backend servers are registered
- Format: JSON mapping of server IDs to arrays of tool information
- Each tool includes: `name` (tool name without server prefix) and `description`
- Example structure:
  ```json
  {
    "servers": {
      "github": [
        {"name": "search_code", "description": "Search for code in repositories"},
        {"name": "get_file_contents", "description": "Get the contents of a file"}
      ],
      "slack": [
        {"name": "send_message", "description": "Send a message to a Slack channel"}
      ]
    }
  }
  ```
- Useful for discovering available tools across all configured backend servers
- Can be used by clients or monitoring tools to understand gateway capabilities

## Error Debugging

**Enhanced Error Context**: Command failures include:
- Full command, args, and environment variables
- Context-specific troubleshooting suggestions:
  - Docker daemon connectivity checks
  - Container image availability
  - Network connectivity issues
  - MCP protocol compatibility checks

## Security Notes

- **Auth**: `Authorization: <apiKey>` header (plain API key per spec 7.1, NOT Bearer scheme)
- **Sessions**: Session ID extracted from Authorization header value
- **Stdio servers**: Containerized execution only (no direct command support)
- **mTLS**: Mutual TLS can be enabled with `--tls-cert`, `--tls-key`, and `--tls-ca` flags (or corresponding env vars) to require client certificates for all connections
- **HMAC request signing**: Set `--hmac-secret` (or `MCP_GATEWAY_HMAC_SECRET`) to require HMAC-SHA256 signed requests; protects against replay attacks using `X-MCP-Timestamp`, `X-MCP-Nonce`, and `X-MCP-Signature` headers

## Resources

- [README.md](./README.md) - Full documentation
- [MCP Protocol](https://github.com/modelcontextprotocol) - Specification
