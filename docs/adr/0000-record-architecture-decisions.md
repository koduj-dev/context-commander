# 0000. Record architecture decisions

## Status

Accepted

## Context

`docs/architecture-cookbook.md` captures the original design rationale end to end, but it is a
single large working document. Individual decisions inside it went through two rounds of external
review that changed several of them non-trivially (see the numbered ADRs in this directory). A
decision needs its own short, dated record so a future contributor can find "what did we decide
and why" without re-reading the whole cookbook, and so a decision can be revisited explicitly
(superseded) instead of silently drifting.

## Decision

Use lightweight ADRs (this format) for load-bearing architecture decisions, numbered sequentially.
Each ADR links back to the relevant section of `docs/architecture-cookbook.md` and, where
applicable, to the review round that changed it. The cookbook stays as background/rationale; the
ADRs are the current, authoritative statement of what to build.

## Consequences

- New load-bearing decisions get a new ADR, not just a cookbook edit.
- Superseding a decision means adding a new ADR that says so and marking the old one
  `Status: Superseded by 000X`, not deleting it.
