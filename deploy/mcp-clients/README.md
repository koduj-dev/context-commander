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

## Token usage

The plans board (docs/adr/0022) shows the time spent on each step and, if the
client sends them, the tokens. A model cannot see its own usage, so the
tokens come from the client. Claude Code writes every response's usage to the
session transcript, and
[`claude-code-usage-hook.py`](claude-code-usage-hook.py) is a `Stop` hook
that sends it after every turn. It counts fresh input, output, cache reads
and cache writes separately, because they are billed differently. For an
agent, almost all of the input is cache reads.

Install it once per machine:

```bash
mkdir -p ~/.claude/hooks
cp deploy/mcp-clients/claude-code-usage-hook.py ~/.claude/hooks/
export CC_URL="http://127.0.0.1:8490"   # the server; CC_TOKEN is already set for MCP
```

Then add this to `~/.claude/settings.json`. Merge it into an existing
`hooks.Stop` list if there is one; do not replace it:

```json
{
  "hooks": {
    "Stop": [
      { "hooks": [ { "type": "command", "command": "python3 ~/.claude/hooks/claude-code-usage-hook.py", "timeout": 10 } ] }
    ]
  }
}
```

Or ask Claude Code to do it: *"Install the Context Commander usage hook from
deploy/mcp-clients/README.md (Token usage) into my user settings."* The steps
above are written so an agent can follow them.

What to know:

- **Use the same `CC_TOKEN` as the MCP connection.** The tokens go to the
  step that this credential last reported with `plan.report`. A different
  token is a different client, and that client is nowhere on the board.
- **Tokens are only recorded while the agent is on a step.** If it never called
  `plan.report`, the server drops the tokens rather than guessing a step. The
  hook says so on stderr, which Claude Code shows in verbose mode.
- **Each turn's report also counts as a heartbeat.** A turn shorter than 15
  minutes keeps the step's tracked time running even if the agent forgets to
  report.
- **What is not counted yet:**
  - Subagents' turns, unless they appear in the main transcript.
  - Which model a token was for. Prices differ by model, so a cost estimate
    needs that next.
- **Codex CLI** logs token counts in its session files as well. There is no
  hook for it here yet.

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
