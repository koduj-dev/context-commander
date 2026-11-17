# 0038. Telemetry without content

## Status

Accepted, 2026-11-17. Phase 3's "operational telemetry that doesn't leak
content".

## Context

An operator had logs and nothing else: no request rates, no latencies, no
idea whether the database pool was saturated or which MCP tools agents
actually call. The usual answer is metrics, and the usual mistake is
labelling them with whatever the request carried, which for this product
is a project's name, a path, an id or a search query.

## Decision

**Prometheus metrics on a listener of their own, off by default.**
`ccd serve --metrics-addr 127.0.0.1:9490` (or `CC_METRICS_ADDR`) serves
`/metrics` on that address only; the API's address never serves it.

**What is counted:**

| Metric | Labels |
|---|---|
| `ccd_http_requests_total`, `ccd_http_request_duration_seconds` | `route` (the router's pattern, e.g. `/v1/projects/{namespace}/{slug}/memories`), `method`, `status` (class: `2xx`…) |
| `ccd_mcp_tool_calls_total`, `ccd_mcp_tool_call_duration_seconds` | `tool` (`unknown` for a call the server could not dispatch), `outcome` (`ok`, `tool_error`, `error`) |
| `ccd_db_connections_acquired`, `_idle`, `_max`, `ccd_db_acquire_waits_total` | none |
| `ccd_goroutines`, `ccd_memory_heap_bytes`, `ccd_start_time_seconds` | none |
| `ccd_build_info` | `version`, `go` |

**What is never a label:** a project, namespace, principal, client,
path, query, id, title or body. Labels come from three closed sets, the
router's patterns, the registered tool names and status classes, so a
caller cannot add series by varying what it sends. Tests assert that a
project's slug, a query, an email and an invented tool name do not appear
in the output.

**No dependency.** The text exposition format is small and stable;
`internal/telemetry` writes it in a few hundred lines instead of pulling in
the Prometheus client library and its tree.

## Consequences

- An operator can graph traffic, errors, latency, pool pressure and tool
  usage, and alert on them.
- The metrics still say how busy the installation is, which routes and
  tools are used, and when. Bind the listener to loopback or protect it as
  you would any internal endpoint.
- Not done: tracing. The OpenTelemetry modules in `go.mod` are indirect
  dependencies of other libraries, not instrumentation of ours.
