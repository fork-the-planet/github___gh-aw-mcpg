# OTEL Tracing in Sentry

This document describes where MCP Gateway telemetry data appears in Sentry when OTEL tracing is enabled.

## Trace URL Format

```
https://github.sentry.io/performance/trace/<trace-id>/
```

The trace ID is logged during workflow runs:
```
[otlp] resolved trace-id=<32-char hex>
```

To extract from a CI run:
```bash
gh run view <run-id> --log | grep "resolved trace-id"
```

## Span Structure

The MCP Gateway emits three span types:

| Span Name | Kind | Description |
|-----------|------|-------------|
| `gateway.request` | SERVER | Top-level HTTP handler for each MCP request |
| `mcp.tool_call` | INTERNAL | Full tool call lifecycle (phases 0–6) |
| `gateway.backend.execute` | CLIENT | Backend MCP server invocation |

For the API proxy (DIFC enforcement), additional spans are emitted:

| Span Name | Kind | Description |
|-----------|------|-------------|
| `proxy.difc_pipeline` | INTERNAL | Full DIFC evaluation pipeline |
| `proxy.backend.forward` | CLIENT | Upstream GitHub API request |

Spans are children of the workflow's parent trace (linked via `GITHUB_AW_OTEL_TRACE_ID` / `GITHUB_AW_OTEL_PARENT_SPAN_ID` env vars passed to the container).

## Attribute Locations in Sentry UI

In Sentry's trace detail view, expand a span and look under **Tags & Attributes**. Attributes are grouped by dot-separated prefix.

### `gen_ai` group

| Attribute | Type | Description | Appears On |
|-----------|------|-------------|------------|
| `gen_ai.tool.name` | string | Tool name (e.g., `search_code`, `get_file_contents`) | `mcp.tool_call`, `gateway.backend.execute`, `proxy.difc_pipeline`, `proxy.backend.forward` |
| `gen_ai.operation.name` | string | GenAI operation name (`execute_tool`) | `mcp.tool_call` |
| `gen_ai.agent.name` | string | Gateway GenAI agent name (`mcp-gateway`) | `mcp.tool_call` |
| `gen_ai.agent.id` | string | Backend MCP server ID (e.g., `github`, `slack`) | `mcp.tool_call`, `gateway.backend.execute` |
| `gen_ai.conversation.id` | string | Truncated session ID for correlation | `gateway.request` |

### `mcp` group

| Attribute | Type | Description | Appears On |
|-----------|------|-------------|------------|
| `mcp.method` | string | JSON-RPC method (always `tools/call`) | `mcp.tool_call` |

### `gateway` group

| Attribute | Type | Description | Appears On |
|-----------|------|-------------|------------|
| `gateway.tag` | string | Handler log tag used for tracing (for example, `unified` in unified mode or `routed:<backendID>` in routed mode) | `gateway.request` |

### `http` group

| Attribute | Type | Description | Appears On |
|-----------|------|-------------|------------|
| `http.request.method` | string | HTTP method (`POST`) | `gateway.request` |
| `http.response.status_code` | number | Response status code | `gateway.request`, `mcp.tool_call` |
| `url.path` | string | Request path (e.g., `/mcp`) | `gateway.request`, `proxy.difc_pipeline`, `proxy.backend.forward` |

### `rate_limit` group

| Attribute | Type | Description | Appears On |
|-----------|------|-------------|------------|
| `rate_limit.hit` | boolean | Whether a rate limit was triggered | `mcp.tool_call`, `gateway.backend.execute`, `proxy.backend.forward` |

### Span Events

`mcp.tool_call` and `proxy.difc_pipeline` include milestone events such as `difc.pre_phases_complete`, `difc.access_denied`, and `rate_limit.detected`. For `gateway.backend.execute`, backend transport errors are recorded via `span.RecordError()` with a generic message (`"tool execution failed"`) to avoid leaking internal details to trace backends.

## Important Sentry Behavior

1. **Numeric custom attributes are dropped** — Sentry only preserves numeric values for attributes it recognizes (e.g., `http.response.status_code`). Unknown numeric attributes are silently discarded.

2. **String custom attributes are preserved** — Custom string attributes (like `gen_ai.*`) are always retained.

3. **PII scrubbing filters "token" in names** — Sentry's default data scrubbing rules redact values of any attribute containing "token" in the key name (treats it as a credential).

4. **Hierarchical grouping** — Attributes are grouped by dot prefix in the UI (e.g., all `gen_ai.*` under "gen_ai", all `mcp.*` under "mcp").

5. **Schema URL conflict warning** — You may see: `conflicting Schema URL: .../1.27.0 and .../1.40.0`. This is a non-fatal warning from the Go OTel SDK due to resource detection version skew and does not prevent export.

## Configuration

OTEL tracing is configured via JSON stdin config or TOML:

### JSON stdin (container mode)

```json
{
  "gateway": {
    "opentelemetry": {
      "endpoint": "https://o205451.ingest.us.sentry.io/api/<PROJECT_ID>/envelope",
      "headers": "x-sentry-auth=sentry sentry_key=<PUBLIC_KEY>",
      "serviceName": "mcp-gateway",
      "traceId": "${GITHUB_AW_OTEL_TRACE_ID}",
      "spanId": "${GITHUB_AW_OTEL_PARENT_SPAN_ID}"
    }
  }
}
```

### TOML (standalone mode)

```toml
[gateway.opentelemetry]
endpoint = "https://o205451.ingest.us.sentry.io/api/<PROJECT_ID>/envelope"
headers = "x-sentry-auth=sentry sentry_key=<PUBLIC_KEY>"
service_name = "mcp-gateway"
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP endpoint URL (fallback when not in config) |
| `OTEL_EXPORTER_OTLP_HEADERS` | Auth headers in `key=value,key=value` format |
| `OTEL_SERVICE_NAME` | Service name (fallback, default: `mcp-gateway`) |

### Endpoint Path Handling

The gateway automatically appends `/v1/traces` to the configured endpoint per the OTEL spec. This means:
- Configure the **base URL** only (no `/v1/traces` suffix needed)
- The same endpoint URL works for both the JS framework and the Go gateway
- Override via `signal_path` / `signalPath` config field if your collector uses a non-standard path

### Trace Context Propagation

The gateway links into the workflow's trace via parent context:

1. Framework creates trace ID, exports setup span
2. Framework passes `GITHUB_AW_OTEL_TRACE_ID` + `GITHUB_AW_OTEL_PARENT_SPAN_ID` to container
3. Gateway config uses `${VAR}` expansion to read these values
4. `tracing.ParentContext()` creates a remote parent span context
5. `httpServer.BaseContext` propagates parent to all HTTP handler spans

## Service Identity

- **Service name**: `mcp-gateway` (configurable via `service_name` / `serviceName`)
- **Instrumentation scope**: `github.com/github/gh-aw-mcpg`
- **Span kinds**: SERVER (HTTP handlers), INTERNAL (tool call pipeline), CLIENT (backend calls)
- **Service version**: Binary version (e.g., `v0.3.16`)

## Flush Behavior

Spans are flushed on graceful shutdown:
- The `/close` endpoint triggers `cancel()` on the signal context (not `os.Exit`)
- Normal shutdown path runs: HTTP drain → context cancel → `TracerProvider.Shutdown()` flushes pending spans
- Default flush timeout: 5 seconds

If spans are missing, verify:
1. The gateway received a `/close` request (check logs for shutdown sequence)
2. Network allows egress to `*.ingest.us.sentry.io` (port 443)
3. Auth header format is correct: `x-sentry-auth=sentry sentry_key=<hex>`
