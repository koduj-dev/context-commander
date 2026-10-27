# 0007. Metadata store abstraction: Postgres now, embedded SQLite for standalone installs

## Status

Accepted

## Context

The plan so far assumes PostgreSQL as *the* metadata database, with a DB role model
(`cc_owner`/`cc_app`) and Row-Level Security as defense-in-depth (see
[0002-non-recursive-rls.md](0002-non-recursive-rls.md)). That's a reasonable default for shared
deployments, but it forces anyone who wants a single-user/standalone install to run a Postgres
server just to try Context Commander. Requiring an installed system service everywhere is a
worse fit for the "self-hosted" pitch than an embedded database.

The good news: nothing about the authorization model actually depends on Postgres. Row-Level
Security was always specified as a *second* layer on top of `AuthzService.Can()`
(`docs/architecture-cookbook.md` §4, and ADR 0002's own text: "Row-Level Security is defense-in-depth on top
of `AuthzService.Can()`, not a replacement for it"). `AuthzService.Can()` — called once from a central
middleware/interceptor before any repository call — is already required to be the actual source of
truth. RLS exists to catch the case where that call was skipped by a bug, not to be the only thing
standing between a request and another project's data.

That means an embedded engine with no roles, no `SET LOCAL`, and no RLS concept (SQLite) is not
a correctness gap — it's a deployment mode that runs with one fewer defense-in-depth layer, which
is an acceptable tradeoff for a single-user/standalone install where the blast radius of an
application-layer authorization bug is "you attacked your own local data."

## Decision

1. Domain packages (`internal/identity`, `internal/projects`, `internal/artifacts`, ...) depend on
   **repository interfaces they define themselves**, never on `pgx` types directly. Example:
   `internal/projects.MembershipStore` is an interface; `internal/db/postgres` provides the
   implementation used in Phase 1–2.
2. `internal/db.WithPrincipal(ctx, fn)` is a **transaction-scoping** abstraction, not
   Postgres-specific. The Postgres implementation opens a `pgx.Tx` and sets the principal via
   `set_config('app.current_principal_id', ..., true)` inside it (see ADR 0003). A future SQLite
   implementation opens a `database/sql` transaction and does *not* set any session variable —
   there is nothing for RLS policies to read, because SQLite has no RLS.
3. `AuthzService.Can()` remains mandatory and backend-independent. It is what actually decides
   ALLOW/DENY; Postgres RLS is allowed to be absent without changing behavior.
4. **PostgreSQL is the MVP default and reference implementation** — Phases 1–3 target it
   exclusively, including the RLS/DB-role work in ADR 0002. SQLite is an explicitly planned second
   backend for standalone installs, implemented once the repository interfaces have stabilized
   against a real (Postgres) implementation, not designed blind up front.
5. Migrations are engine-specific (`golang-migrate` supports both), not shared SQL files — the two
   schemas do not need to be byte-identical, only satisfy the same repository interfaces.

## Consequences

- Adds an interface layer between domain code and `pgx` from day one (Phase 1), which is a small
  amount of extra ceremony now in exchange for not having to retrofit it later.
- `cc_has_project_access()` / `SET LOCAL` / the `cc_owner`/`cc_app` role split (ADR 0002) are
  documented as **Postgres-backend-specific hardening**, not a domain invariant every backend must
  reproduce.
- Test suites that assert RLS behavior (see ADR 0002 acceptance criteria) run only against the
  Postgres backend; the shared negative-authorization tests (READER write → DENY, cross-project
  read → DENY) run against `AuthzService` directly and therefore against any backend.
- SQLite support itself is **not** in the Phase 0–3 scope — this ADR only commits to not building a
  Postgres-shaped hole that a second backend can't climb out of later.
