# 0014. `AuthzService.Can()` is called by the domain service itself, not only by adapter middleware

## Status

Accepted

## Context

`docs/architecture-cookbook.md` and the implementation plan both say authorization should be
checked from "a single `AuthzService.Can()` call from one middleware/interceptor for all adapters"
— aimed at the pitfall where each HTTP handler (say) does its own role check and someone adds a new
handler and forgets. Phase 3 built `internal/artifacts.Service` before any adapter (HTTP/MCP/CLI —
Phase 4+) exists, and while doing so it became clear that "one middleware for all adapters" is
actually a weaker guarantee than it sounds: it still means N places (one middleware per adapter
*kind* — HTTP, MCP, CLI each need their own request-to-capability mapping) that all have to
correctly wire up the check, which is the same class of drift risk the pitfall was trying to avoid,
one level up.

## Decision

`internal/artifacts.Service` calls `AuthzService.Can()` itself, at the top of every method that
touches project-scoped data (`Write`, `Read`, `List`, `Stat`, `History`, `Delete`), before touching
the database. A denied check returns `ErrForbidden`, distinguishable from `ErrNotFound`.

This makes the *domain service* the chokepoint, not adapter middleware: every adapter (HTTP, MCP,
CLI) and every other domain service that ever calls into `artifacts` goes through it automatically,
with nothing to remember to wire up per adapter. Row-Level Security (docs/adr/0002) remains the
layer beneath this — defense-in-depth if this check is ever bypassed by a bug, not a substitute for
it, exactly as originally specified.

Future domain services (`internal/handoffs`, `internal/skills`, ...) should follow the same
pattern: take an `*auth.AuthzService` in their constructor, check the relevant capability first in
every method.

## Consequences

- Adapters (Phase 4+) do not need their own authorization middleware for capability checks — they
  still need an *authentication* step (PAT -> Principal, per docs/adr/0009), but the capability
  decision itself is the domain service's job.
- Every domain service method takes a `principalID` as its first real argument (after `ctx`), not
  a full `Principal` — the service resolves what it needs via `AuthzService`/repositories itself.
- `ErrForbidden` vs. `ErrNotFound` is surfaced as-is from the domain layer; whether an adapter turns
  `ErrForbidden` into an HTTP 403 or folds it into a 404 (to avoid leaking a project's existence) is
  that adapter's decision, not this package's.
