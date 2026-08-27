# 0009. PAT token format, and bootstrapping the first principal without a UI

## Status

Accepted

## Context

`docs/architecture-cookbook.md` §5.1 requires project/credential creation to work without opening
the web UI. Round 2 (§8) pointed out this creates a chicken-and-egg problem: the MVP auth model is
"PAT maps to a Principal + ClientCredential," but nothing said how the very first `User` and PAT
come to exist on a fresh install without either raw SQL or a UI that doesn't exist yet. Round 2
addendum 3 additionally specified the token format precisely, so an implementer doesn't invent a
"hash every stored token and compare" lookup, which doesn't scale and invites timing side-channels.

## Decision

**Token format**: `cc_<credential-id>_<random-secret>`, where `credential-id` is the
`client_credentials.id` UUID (public — used for O(1) lookup, `WHERE id = $1`) and
`random-secret` is a high-entropy random string. Only a hash of the secret part is stored
(`client_credentials.token_hash`); the credential ID is not secret and is never hashed. Verifying a
token is: parse out the ID, `SELECT ... WHERE id = $1 AND revoked_at IS NULL AND (expires_at IS
NULL OR expires_at > now())`, then compare the secret part's hash — never "load every credential
and compare."

`client_credentials` additionally carries `last_used_at` (updated on successful auth, best-effort)
and `expires_at` (nullable — a PAT without an expiry is a supported, deliberate choice, not an
oversight).

**Bootstrap** (`cmd/ccd`, `ccd bootstrap` / `ccd user create` / `ccd token create`): a CLI path that
creates the first `User`, wraps it in a `Principal`, and issues a `ClientCredential` (PAT) for it —
entirely through `internal/identity`, the same domain code the HTTP/MCP adapters call, just driven
from `adapters/cli` instead. `docker compose up` on a fresh install must be bootstrappable this way
without hand-written SQL and without the UI existing yet.

## Consequences

- `internal/identity` exposes `CreateUser`, `CreatePrincipalForUser`, `IssueCredential` as ordinary
  domain functions; `adapters/cli` and a future `adapters/http` admin endpoint both call them —
  no bootstrap-only code path duplicates this logic.
- The PAT verification path is O(1) DB lookup + one hash comparison, never a table scan.
