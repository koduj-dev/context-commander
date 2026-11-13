# 0031. One-hour cache writes counted apart; retention for the usage ledger

## Status

Accepted. Refines [ADR 0022](0022-plans-phases-and-presence.md) ("Time and
tokens") and [ADR 0027](0027-usage-ledger-by-branch-and-ticket.md), both of
which recorded the two gaps closed here as consequences.

## Context

Two follow-ups to the usage ledger, both raised by the maintainer on
2026-11-13.

- **One-hour cache writes were under-priced.** Anthropic bills a
  five-minute cache write at 1.25x input and a one-hour write at 2x. A cache
  write was one count with one rate, so every one-hour write was priced as
  a five-minute one. Claude Code's transcripts already split them:
  `usage.cache_creation.ephemeral_5m_input_tokens` and
  `ephemeral_1h_input_tokens` sit beside `cache_creation_input_tokens`.
- **The ledger had no way to forget.** ADR 0027 left rows without a
  `DELETE` grant and deferred purging to retention. Its growth is bounded by
  distinct days and dimensions, but who spent what on which branch is a
  per-person record, and an operator may not want to keep it forever.

## Decision

### One-hour cache writes are a subset, not a fifth kind

`cache_write_1h_tokens` is the part of `cache_write_tokens` that was
written to the one-hour cache. It is added to `plan_item_effort`,
`plan_item_effort_models` and `usage_ledger` (migration 000042), with a
`CHECK` that it never exceeds `cache_write_tokens`.

A fifth, separate kind was considered and rejected:

- With a subset, the four counts still add up to every token reported.
  Every total, screen and client that sums them keeps meaning the same
  thing, and none of them had to change.
- A client or a row that knows nothing of one-hour writes simply has none.
  Old rows, the Codex hook and any client written before this read
  correctly without a backfill.
- A separate kind would have made `cache_write_tokens` mean "five-minute
  writes" from some date on and "all writes" before it, which is exactly
  the kind of ambiguity a total must not have.

A report whose one-hour part exceeds its cache writes is refused
(`invalid`), like any other implausible delta. The Claude Code hook sends it
from `ephemeral_1h_input_tokens`, clamped to the cache writes. A session
that was already running when the hook was upgraded has a state file
without one-hour counts; the hook then sends at most the turn's cache writes
as one-hour, so that session's earlier one-hour writes stay priced as
five-minute ones rather than being sent in a way the server refuses.

`plan.report` over MCP carries only fresh input and output, as before, so
it does not take the new count. `POST /v1/me/usage` does. The plan effort
and usage summary responses, over HTTP and MCP, return it beside the four
counts.

### Pricing

A price table may give `cache_write_1h` per model. It is optional and
defaults to `cache_write`: every table written before this keeps parsing
and keeps pricing one-hour writes as it did. `ParsePrices` still refuses
unknown fields and still requires the four original rates, for the reasons
ADR 0022 gives. A cost is the five-minute part
(`cache_write_tokens - cache_write_1h_tokens`) at `cache_write` plus the
one-hour part at `cache_write_1h`.

### Retention

`ccd serve --usage-ledger-retention DAYS` (`CC_USAGE_LEDGER_RETENTION`)
keeps that many days of the ledger. The default, 0, keeps it forever, as
before. The server purges once at start and then hourly, deleting rows
whose `day` is more than `DAYS` days before today in UTC.

- **The purge is a `SECURITY DEFINER` function,** `cc_purge_usage_ledger`,
  owned by the table-owning role that ADR 0002 exempts from RLS and
  executable by `cc_app`, the same shape as the other functions that work
  without a principal (activity roll-up, the embedding sweep). It deletes only rows older
  than the retention it is given and refuses a retention below one day, so
  it cannot be turned into "delete everything". `cc_app` still has no
  `DELETE` on `usage_ledger`: no request can remove a counted cost.
- **The schedule is a ticker in the server process,** like the embedding
  index's sweep (ADR 0028). Unlike activity retention (migration 000029),
  it does not ride on writes: the retention is the operator's setting, and
  the database does not know it.
- **Only the ledger is pruned.** `plan_item_effort` and its per-model rows
  belong to the plan step and go when the plan does.

## Consequences

- A summary over a range older than the retention is short, silently: the
  rows are gone. The operator chose that by setting it.
- One-hour writes made before migration 000042, or by a session that was
  running when its hook was upgraded, stay counted as five-minute writes.
  Nothing is backfilled, because the counts that would tell them apart were
  never stored.
- The Codex hook sends no one-hour part; OpenAI's usage does not report one.
