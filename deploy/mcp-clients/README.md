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

## Planning agent

[`planning-agent.md`](planning-agent.md) is a ready prompt for an agent that
works through a project's idea stack (docs/adr/0026): it takes one idea,
reads the project's context, proposes a solution with a draft plan, and
stops. Context Commander does not run it; start it by hand, from cron or
from CI, with a token of its own.

## Token usage

The plans board (docs/adr/0022) shows the time spent on each step and, if the
client sends them, the tokens. A model cannot see its own usage, so the
tokens come from the client. Claude Code writes every response's usage and
model to the session transcript, and
[`claude-code-usage-hook.py`](claude-code-usage-hook.py) is a `Stop` hook
that sends it after every turn, per model. It counts fresh input, output,
cache reads and cache writes separately, because they are billed
differently. For an agent, almost all of the input is cache reads. Of the
cache writes, it also sends how many went to the one-hour cache
(`cache_write_1h_tokens`), which costs more than the five-minute one.
Subagents count too: the hook reads their transcripts, which Claude Code
keeps in `<session>/subagents/` beside the session's own.

Install it once per machine:

```bash
mkdir -p ~/.claude/hooks
cp deploy/mcp-clients/claude-code-usage-hook.py ~/.claude/hooks/
export CC_URL="http://127.0.0.1:8490"   # the server; CC_TOKEN is already set for MCP
export CC_PROJECT="team/app"            # optional: count sessions that never reported a step
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
- **Tokens land on the step the agent last reported, and always in the
  project's usage ledger** (docs/adr/0027). If the agent never called
  `plan.report`, the tokens reach no step. Set `CC_PROJECT=namespace/slug` and
  the hook counts them in that project's usage anyway. Without it the server
  drops them, and the hook says so on stderr (Claude Code shows that in
  verbose mode).
- **The branch comes from the repository.** The hook runs
  `git branch --show-current` in the session's working directory and sends
  the result, so usage can be grouped by branch. A ticket key comes from the
  agent's `plan.report` (`ticket_key`), because no hook can know it.
- **Each turn's report also counts as a heartbeat.** A turn shorter than 15
  minutes keeps the step's tracked time running even if the agent forgets to
  report.
- **Nothing is sent twice.** The hook remembers what it sent per session, in
  `~/.local/state/context-commander/usage/` (or `$XDG_STATE_HOME`). It
  records tokens as sent even when the server dropped them, so they are never
  credited to a later step. A state file from the previous version, which
  had no models, is honoured.
- **A subagent's tokens arrive with the next `Stop`.** A subagent running in
  the background may finish after the turn that started it. The hook sends
  its tokens when the next turn ends.

### Codex CLI

Codex CLI writes each session to a rollout file under
`~/.codex/sessions/YYYY/MM/DD/`. The file records the model of every turn
and a running token total after every response.
[`codex-usage-hook.py`](codex-usage-hook.py) reads it and sends the growth
per model, with the same never-send-twice state. OpenAI counts cached input
inside input, so the hook sends input minus cached input as fresh input, and
the cached part as cache reads.

Install it once per machine:

```bash
mkdir -p ~/.codex/hooks
cp deploy/mcp-clients/codex-usage-hook.py ~/.codex/hooks/
export CC_URL="http://127.0.0.1:8490"   # CC_TOKEN is already set for bearer_token_env_var
```

Then register it as a `Stop` hook in `~/.codex/hooks.json`. Merge it into an
existing `hooks.Stop` list if there is one:

```json
{
  "hooks": {
    "Stop": [
      { "hooks": [ { "type": "command", "command": "python3 ~/.codex/hooks/codex-usage-hook.py", "timeout": 10 } ] }
    ]
  }
}
```

A Codex version without hooks can run the script as its `notify` program
instead. Codex passes the event as the last argument, and the script finds
the rollout by the thread id. Add this line to `~/.codex/config.toml`, at
the top level, unless a `notify` program is already set:

```toml
notify = ["python3", "/home/you/.codex/hooks/codex-usage-hook.py"]
```

Use the same `CC_TOKEN` as the MCP connection (`bearer_token_env_var` in
[`codex-config.toml`](codex-config.toml)), and have the agent call
`plan.report`, as for Claude Code.

### Cost estimate

The server can show what the reported tokens would have cost on the API. That
is how you compare API pricing with a subscription. Give it a price table:

```bash
cp deploy/usage-prices.example.json /etc/context-commander/usage-prices.json
# check every price against the vendor's current price list, then
ccd serve --usage-prices /etc/context-commander/usage-prices.json   # or CC_USAGE_PRICES
```

The table gives prices per million tokens, for each model and each of the
four kinds. A key ending in `*` matches every model id that starts with the
rest. The example's values were copied on the date in the file and are an
example only. Codex models are left out of it; add them from OpenAI's price
list. The Plans tab then shows an estimate next to the tokens of each item,
plan and phase. Models the table does not price show "no price", and their
tokens are left out of the estimate rather than counted as free. It is an
estimate, not what was spent. One-hour cache writes are priced at
`cache_write_1h` when the table gives it, and at `cache_write` when it does
not, so a table written before that rate existed keeps working.

### Keeping the ledger bounded

The usage ledger keeps one row per project, day, person, model, branch and
ticket, forever by default. To keep only recent days, start the server with
`--usage-ledger-retention DAYS` (or `CC_USAGE_LEDGER_RETENTION`). Once an
hour it deletes the rows whose day is more than that many days before today
(UTC). Plan steps keep their own time and tokens; only the ledger behind the
Usage screens is pruned. See
[ADR 0031](../../docs/adr/0031-one-hour-cache-writes-and-ledger-retention.md).

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
