# Release Notes

## v0.3.6

This is a quality-focused maintenance release for MCP Gateway v0.3.6, emphasizing code reliability, performance improvements, and test coverage across the codebase.

### ⚡ Performance Improvements

- **Faster Rust guard processing**: Eliminated redundant clones in `extract_mcp_response` and `first_matching_scope`, reducing memory allocation overhead in guard evaluation ([#5103](https://github.com/github/gh-aw-mcpg/pull/5103))

### 🔧 Reliability & Code Quality

- **Unified session timeout handling**: `MCP_GATEWAY_SESSION_TIMEOUT` lookup is now deduplicated into a shared `getSessionTimeout()` helper, ensuring consistent behavior across routed and unified server modes ([#5100](https://github.com/github/gh-aw-mcpg/pull/5100))
- **Cleaner server handler construction**: Extracted `buildMCPHandler` to eliminate duplicated handler setup logic across server modes ([#5101](https://github.com/github/gh-aw-mcpg/pull/5101))
- **Streamlined utilities**: `generateRandomID` inlined, truncation delegated to `strutil`, and `loadEnvFile` moved to `envutil` — reducing coupling and improving reuse ([#5104](https://github.com/github/gh-aw-mcpg/pull/5104))
- **Fixed Rust guard compile error**: Resolved unused import error in `labels/mod.rs` that could prevent guard compilation ([#5089](https://github.com/github/gh-aw-mcpg/pull/5089))

### 🧪 Test Coverage

- **Improved CI reliability**: Integration test timeouts increased for Docker image pulls, reducing flaky test failures in CI environments ([#5118](https://github.com/github/gh-aw-mcpg/pull/5118))
- **Expanded sys package tests**: Added success-path coverage for `CheckPortMapping`, `CheckStdinInteractive`, and `CheckLogDirMounted` ([#5077](https://github.com/github/gh-aw-mcpg/pull/5077))
- **Tracing config tests**: Improved test coverage for the config tracing package ([#5076](https://github.com/github/gh-aw-mcpg/pull/5076))
- **Idiomatic testify usage**: Refactored assertions across the test suite to use specific testify methods for clearer failure messages ([#5102](https://github.com/github/gh-aw-mcpg/pull/5102))

### 📚 Debug Observability

- **DIFC label debug logging**: Added structured debug logging to `difc/labels.go` to improve traceability of label evaluation during development and troubleshooting ([#5069](https://github.com/github/gh-aw-mcpg/pull/5069))

### 🐳 Docker Image

The Docker image for this release is available at:

```bash
docker pull ghcr.io/github/gh-aw-mcpg:v0.3.6
# or
docker pull ghcr.io/github/gh-aw-mcpg:latest
```

Supported platforms: `linux/amd64`, `linux/arm64`

---

For complete details, see the [full release notes](https://github.com/github/gh-aw-mcpg/releases/tag/v0.3.6).
