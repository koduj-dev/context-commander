# Supported MCP clients

Context Commander guarantees two concrete MCP client implementations rather
than a vague "works with any MCP client". Both connect over **Streamable
HTTP** to `ccd serve`'s `/mcp` endpoint, authenticating with a Personal
Access Token as `Authorization: Bearer <token>`.

| Client | Config file | Fixture here |
|---|---|---|
| Claude Code | `.mcp.json` in the project root (or `~/.claude.json`) | [`claude-code.mcp.json`](claude-code.mcp.json) |
| Codex CLI | `~/.codex/config.toml` (or project-local `.codex/config.toml`) | [`codex-config.toml`](codex-config.toml) |

Both fixtures read the token from `$CC_TOKEN` instead of embedding it — a
project-local config file is exactly the one that gets committed by accident.

These files are not documentation samples: `adapters/mcp/clients_test.go`
parses them and asserts the endpoint and credential each client would
actually send, then runs the same handshake and `context.bootstrap` call
against a real server. A change to the server's MCP surface that would break
either client fails the test suite rather than a user's first session.

## Claude Code

```bash
export CC_TOKEN="cc_..."          # printed by `ccd bootstrap`
cp deploy/mcp-clients/claude-code.mcp.json .mcp.json
claude                              # then: /mcp   to verify the connection
```

Or without the fixture file:

```bash
claude mcp add --transport http context-commander http://127.0.0.1:8490/mcp \
  --header "Authorization: Bearer $CC_TOKEN"
```

## Codex CLI

```bash
export CC_TOKEN="cc_..."
cat deploy/mcp-clients/codex-config.toml >> ~/.codex/config.toml
codex                               # the tools appear as context_commander.*
```

## What to expect once connected

The server advertises `context.bootstrap` as the first call to make. On a
project that has a handoff it returns, in one response: the project and your
role in it, the handoff to continue, the content of the project's effective
skills, the artifacts it says to read, and an event cursor. Everything after
that first step uses the per-domain tools (`handoff.*`, `artifact.*`,
`skill.*`, `event.list`).

## Note on TLS

Both examples above point at loopback in cleartext, which is what `ccd serve`
allows by default. Any non-loopback address requires TLS (`--tls-cert` /
`--tls-key`) or an explicit `--insecure-allow-cleartext` — a bearer token and
decrypted project content must not travel a shared network in the clear.
Change the URL to `https://` in the fixture when you put the server behind a
TLS-terminating proxy.
