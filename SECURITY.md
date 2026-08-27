# Security Policy

## Supported versions

Pre-1.0, no releases yet: only the latest commit on `main` is supported.

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report privately through either:

- **GitHub** — *Security → Report a vulnerability* (private vulnerability reporting /
  advisories) on this repository, **or**
- **Email** — **filip@koduj.dev**.

Please include, as far as you can:

- a description of the issue and its impact,
- steps to reproduce or a proof of concept,
- the commit/tag you're on and relevant deployment details (local docker-compose,
  standalone, self-hosted).

Do not include working exploit code or real credentials in the report — describe the class of
problem and, where possible, a minimal reproduction against a disposable local instance. We aim
to acknowledge a report within a few days. This is a community project maintained on a
best-effort basis — thanks for your patience.

## What review this code has had — and what it hasn't

The domain design went through two rounds of external architecture review before
implementation started (see the `docs/adr/` for what each one changed and why). During
implementation, every phase is covered by tests that run against a real, disposable
Postgres/MinIO (via `testcontainers-go`) — not mocks — specifically because RLS, transaction, and
encryption bugs don't reproduce against a mock. That combination has already found and fixed real
defects before they shipped, not just hypothetical ones: see "Invariants worth knowing" below for
three examples.

Stated plainly, so nobody infers more than is there:

- **There has been no third-party security audit.** The above is the maintainer's own process,
  not an independent assessment. Treat it as diligence, not as assurance.
- **Test coverage covers what it was pointed at.** That a code path has a passing test is evidence
  about that path, not a claim about the rest of the codebase.
- **This project is pre-1.0 and the domain model is still settling.** Expect rougher edges than a
  1.0 release, particularly in subsystems noted as not yet implemented in `docs/architecture-cookbook.md`.

## Scope & threat model

Context Commander's default deployment mode ("standard mode") allows the server to process
plaintext after authorized decryption — this is a deliberate tradeoff to support search, previews,
secret scanning, and server-side transformations. It is **not** a zero-knowledge system in this
mode (see `docs/architecture-cookbook.md` §25 for the future zero-knowledge-mode roadmap).
Application-level envelope encryption (`docs/adr/0005-key-hierarchy-and-artifact-size-limit.md`)
protects against:

- stolen disks, leaked backups, database dumps,
- leaked object-store contents,
- a storage administrator without decryption keys.

It does **not** protect against:

- an attacker who fully controls the running server process,
- a compromised host with access to decryption keys,
- a malicious authorized user,
- data already sent to an external model/vendor by a client,
- a secret an agent copies into a permitted artifact — mitigated, not prevented, by the future
  secret-scanner gate (`docs/architecture-cookbook.md` §15.1, not yet implemented).

**Project membership is the primary authorization boundary**
(`docs/adr/0001-project-namespace-not-global-slug.md`,
`docs/adr/0006-organization-vs-project-authorization.md`). Every domain service that touches
project-scoped data calls `AuthzService.Can()` itself before touching the database
(`docs/adr/0014-authz-check-in-domain-service.md`); PostgreSQL Row-Level Security
(`docs/adr/0002-non-recursive-rls.md`) is defense-in-depth beneath that, not a replacement for it.

**Secrets are never ordinary artifacts.** A `client_credentials` row stores only a hash of a PAT's
random-secret part (`docs/adr/0009-pat-format-and-bootstrap.md`) — the credential ID is public and
used for O(1) lookup, never the secret itself.

## Invariants worth knowing before changing security-sensitive code

Three rules the identity/crypto code depends on. Each exists because breaking it produced a real
defect that implementation-time testing caught before it shipped — not a hypothetical.

- **RLS principal context is set with `SELECT set_config('app.current_principal_id', $1, true)`,
  never a literal `SET LOCAL ... = $1`.** Postgres's `SET` doesn't accept a bound parameter for
  the value; `set_config()` does, safely. And it must be `is_local = true` (transaction-scoped),
  never session-level — a pooled connection is reused across unrelated requests, so a session-level
  `SET` would leak one request's principal into the next
  (`docs/adr/0003-set-local-transaction-context.md`). Only `internal/db.WithPrincipal()` may call
  this; nothing else opens a transaction for project-scoped work.
- **A custom Postgres GUC returns `NULL` only the *first* time it's touched on a connection.**
  After that, an unset transaction on the same (reused) connection gets `''` back, not `NULL` — a
  naive `::uuid` cast in an RLS policy would hard-error instead of safely denying. Found by testing
  against a real Postgres with a forced single-connection pool, not by reasoning about it in
  advance; `cc_has_project_access()` guards the cast internally
  (`docs/adr/0002-non-recursive-rls.md`).
- **`AuthzService.Can()` is called by the domain service itself**, not only trusted to have been
  checked by an adapter earlier in the request. `internal/artifacts.Service` shipped its first
  draft with no such call at all, relying solely on RLS — exactly backwards from the project's own
  "RLS is defense-in-depth, not the primary check" rule
  (`docs/adr/0014-authz-check-in-domain-service.md`).

Generally **out of scope**: issues that require an already-compromised host or database, running
the app without the documented protections (e.g. exposing the dev `docker-compose.yml` credentials
in production), or vulnerabilities in PostgreSQL/MinIO/dependencies themselves (report those
upstream, though we're happy to bump versions).
