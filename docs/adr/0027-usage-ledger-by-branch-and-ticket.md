# 0027. A usage ledger by day, person, model, branch and ticket

## Status

Accepted. Extends [ADR 0022](0022-plans-phases-and-presence.md) ("Time and
tokens").

## Context

ADR 0022 records effort per plan step: seconds covered by heartbeats and the
four token counts per model. That answers "what did this step cost". On
2026-11-13 the maintainer's team asked for more:

- "What did this week cost? Who spent it, on which branch, for which
  ticket?" Steps don't answer that. Branches and tickets are how the team
  already names its work, and much of that work happens on no step at all.
- Tokens from a client that never called `plan.report` were dropped, so the
  totals undercounted every session that forgot to report.
- The team wanted statistics from the server, over HTTP for the dashboard
  and over MCP for an agent that is asked about them.

Asking agents to report their own token counts with every write was
considered and rejected. A model cannot see its own usage, so it would
estimate, and an estimate shown beside exact counts reads as a fact. The
counts keep coming from the client hooks, which read them exactly.

## Decision

### The ledger

`usage_ledger` (migration 000039) keeps one row per
`(project, UTC day, principal, model, branch, ticket_key)`. Each row holds
seconds, the four token counts and `turns`, the number of reports that
carried tokens. Counts are added to the row, never logged one by one. The
table therefore grows with distinct days and dimensions, not with reports,
and needs no retention to stay bounded at a team's scale.

- **Time** is time present, whether or not the client is on a step. The rule
  is ADR 0022's: the gap between two reports counts when it is at most 15
  minutes. Seconds go on the `model = ''` row, because time is not spent per
  model.
- **Tokens** go on their model's row. A report without a model goes on the
  `''` row, which reads as "not stated".
- Both are credited in the same transaction as the step's effort, by the same
  function, so the ledger and the steps cannot disagree about a report.

### Where branch and ticket come from

`work_presence` gains `branch` and `ticket_key`, and a credit goes to the
values on the client's previous presence row, because that is where the
time was spent.

- `plan.report` takes `branch` and `ticket_key`. A report that omits them
  keeps the values from the client's previous report in the same project,
  and moving to another project clears them.
- The usage hooks read the branch with `git branch --show-current` in the
  session's working directory and send it with every post. The hook's branch
  wins for that turn's tokens, since it was read when they were spent. It
  knows no ticket, so the presence row's ticket stays.
- A hook configured with `CC_PROJECT` places a client that has no presence
  in that project, on no step. Its tokens then reach the ledger instead of
  being dropped. Without `CC_PROJECT` they are still dropped. A token is
  never attached to a guessed step.

### Classification (ADR 0011)

`branch` and `ticket_key` are OPERATIONAL METADATA, allowlisted here. Grouping
by them is the point, and the database cannot group what it cannot read. The
risk is the same one ADR 0025 accepts for memories: a branch name such as
`fix/customer-x-leak` says something about the work. The alternative,
encrypting them and grouping in Go after decrypting every row, would turn a
summary into a full scan of the project's history on every dashboard load.

### Reading it

`plans.Service.Usage` sums the ledger over a range of at most 366 days,
grouped by any of `project`, `day`, `principal`, `model`, `branch` and
`ticket`. The grouping is spliced into SQL only through an allowlist of
column names. A summary across projects reads every project where the
caller's role holds `plan.read`, the same scoping as plan search. Each row is
priced at the operator's price table (`--usage-prices`). Rows are always read
per model, so they can be priced, and the model is folded away afterwards if
the caller did not group by it. At most 1000 rows are returned, and the
total is always complete.

It is served at `GET /v1/projects/{ns}/{slug}/usage`,
`GET /v1/me/usage/summary` and the MCP tool `usage.summary`.

## Consequences

- Rows have no `DELETE` grant. Purging old usage belongs to retention, which
  NEXT.md already lists.
- One-hour cache writes are still priced as five-minute ones (ADR 0022). The
  ledger inherits that.
- An installation that ran before migration 000039 has steps with effort and
  an empty ledger. Nothing is backfilled, because the steps have no day,
  branch or ticket to backfill from.
