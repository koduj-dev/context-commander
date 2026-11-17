# 0039. Retention by age, set by the operator

## Status

Accepted, 2026-11-17. The rest of Phase 3's "retention rules", after
ADR 0036.

## Context

ADR 0036 removes what someone deleted. It left memories, conversation
history and the audit trail kept forever, because how long a team must keep
them depends on the team: a company with a retention policy needs to drop
old history; one in litigation needs to keep everything.

## Decision

**`ccd purge` takes age rules, and every one defaults to 0, keep forever:**

| Flag | Removes |
|---|---|
| `--memories-archived-after N` | archived and superseded memories not changed for N days. Active memories are never removed by age: they are what the team still relies on. |
| `--history-after N` | conversations whose newest object is older than N days, with their external sessions and objects |
| `--audit-after N` | audit trail rows older than N days, with their encrypted payloads |

Projects under legal hold are skipped by every rule. `--dry-run` counts
what each rule would remove. Each kind runs in one transaction, so a
conversation goes with all its objects or not at all.

**No defaults that delete.** The product does not decide what a team must
keep. An installation that sets nothing loses nothing, exactly as before.

**The security log is not covered.** It is installation-wide and holds no
content; an operator who needs to trim it does so in SQL.

## Consequences

- An operator can apply a retention policy with one daily command
  (`ccd-purge.timer`, with the flags in a drop-in).
- Removed audit rows are gone: a policy that removes them should match what
  the team is required to show.
