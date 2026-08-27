# 0016. Go MCP SDK: the official `modelcontextprotocol/go-sdk`

## Status

Accepted

## Context

`docs/architecture-cookbook.md` §3.4 and the implementation plan deliberately left the concrete Go
MCP SDK unpicked until Phase 4 actually started, rather than fixing it up front against an
ecosystem still moving (§3.4: "MCP is an adapter, not the internal architecture" — the domain must
not depend on protocol-library specifics regardless of which one is chosen).

As of Phase 4 (August 2026), two credible options exist:

- **`github.com/modelcontextprotocol/go-sdk`** — the official Go SDK, maintained by the MCP spec
  org together with Anthropic and Google. Spec-complete for the current MCP revision, actively
  released (v1.4.1 in March 2026 to v1.7.0 by July 2026), with production adopters already
  migrated onto it from community SDKs.
- **`github.com/mark3labs/mcp-go`** — the original, influential community SDK. Still maintained,
  and its own design informed the official SDK's, but it has been effectively superseded as the
  default recommendation now that an official, spec-org-backed SDK exists.

## Decision

Use **`github.com/modelcontextprotocol/go-sdk/mcp`, v1.7.0+**.

Server-side shape used in `adapters/mcp`:

- `mcp.NewServer(impl *mcp.Implementation, opts *mcp.ServerOptions) *mcp.Server` — one server
  instance, built from the same domain services (`internal/artifacts.Service`, `internal/projects.Store`,
  `internal/identity`) every other adapter uses — no business logic lives in this package
  (`docs/architecture-cookbook.md` §3.4, `CONTRIBUTING.md`).
- `mcp.AddTool[In, Out](server, tool, handler)` — the generic form, which derives the JSON Schema
  for a tool's input/output from Go struct types instead of hand-written schemas that can drift
  from the handler.
- Transport is pluggable and chosen independently of tool registration: `mcp.StreamableHTTPHandler`
  for the long-running server exposed over HTTP (the modern "Streamable HTTP" transport, superseding
  plain SSE in the spec), and `mcp.StdioTransport` kept available for local/CLI-integration use —
  same `Server` and tool set either way.

## Consequences

- `adapters/mcp` depends on this SDK; `internal/` never does (docs/architecture-cookbook.md §3.4,
  Rule 3 in `CONTRIBUTING.md`) — if the SDK's API changes or a future revision of the spec
  standardizes something `internal/` already models on its own terms, only this adapter package
  needs to change.
- Authentication for MCP requests goes through `identity.Authenticate` — the same chokepoint
  `adapters/cli` and `adapters/http` use (`docs/adr/0009`) — not a bespoke MCP-specific token check.
- This ADR settles the last item the implementation plan had deliberately left open; no further
  MCP-SDK evaluation is expected before a real reason to revisit it appears (a breaking SDK change,
  a spec revision the SDK doesn't yet support, or a concrete production issue).
