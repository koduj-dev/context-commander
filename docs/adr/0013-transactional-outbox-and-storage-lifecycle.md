# 0013. Transactional outbox for events; failure-safe ObjectStore/Postgres write order

## Status

Accepted

## Context

Two related dual-system consistency problems, both flagged in review round 2:

- **§5**: `ObjectStore` (S3/filesystem) and PostgreSQL cannot share an ACID transaction. Without an
  explicit order, either the object upload can succeed while the DB commit fails (orphan
  ciphertext), or the DB can commit first and reference an object that was never actually written.
- **§6**: if `event.publish` is a separate step run *after* the DB commit of the mutation it
  describes, its failure means "the artifact exists but no one was told" — an unreliable
  notification layer no matter how well-designed the event ID scheme is.

## Decision

**Write order** for anything that touches both `ObjectStore` and Postgres (e.g. `artifact.write`):

1. Encrypt and upload the ciphertext to `ObjectStore` under an immutable, never-reused object key.
2. Verify the upload succeeded.
3. Open one Postgres transaction (`internal/db.WithPrincipal`) that inserts the
   `artifact_versions` + `artifact_version_envelopes` rows, updates `artifacts.current_version`,
   writes the audit row, and writes the outbox row (below) — all in the same transaction.
4. Commit.

If step 3/4 fails, the object uploaded in step 1 becomes an **orphan candidate** — never referenced
by any DB row — cleaned up asynchronously (a periodic sweep of objects with no matching
`artifact_versions.storage_key`, older than a grace period). The DB is never left pointing at an
object that doesn't exist; the reverse (an unreferenced object sitting in storage) is the
acceptable failure mode.

Delete is symmetric but inverted: write a DB tombstone / logical deletion and commit first, then
physically remove the object asynchronously after commit — never delete the object before the DB
change is durable, or a rollback would leave a DB row pointing at nothing.

**Transactional outbox**: `domain_events` (`seq bigserial`, `event_id uuid`, `project_id`, `type`,
`resource_type`, `resource_id`, `payload_kind`, `created_at`) is written in the *same* Postgres
transaction as the domain mutation it describes (step 3 above). This groundwork lands in Phase 3,
when the first mutations that need it (artifact writes) are implemented — not deferred to Phase 9,
which only adds the feed/transport (SSE or polling) reading this table.

`seq` is the monotonic resume cursor (`GET /events?after=182731`); `event_id` is for
identity/deduplication only, never for ordering.

## Consequences

- `artifact.write`'s Go implementation is explicitly two phases (upload-then-transact), not a
  single call that hides the ordering.
- A rollback of the Phase-3 transaction must never leave a `domain_events` row without the
  corresponding domain change, and vice versa — same transaction, so this is structural, not a
  behavior to test for regressions separately (though a test still exists per the plan's
  acceptance criteria).
- An orphan-object cleanup job/query is part of Phase 3's scope, not a "later" item.
