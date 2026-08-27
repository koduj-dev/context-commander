# 0010. `ArtifactVersion` is immutable; crypto wrapping state lives in a separate envelope table

## Status

Accepted

## Context

The domain invariant "ArtifactVersion is immutable" was stated from round 1 onward, but the
original schema stored `wrapped_dek`/`nonce`/`project_key_version` directly on `artifact_versions`.
`RotateProjectKey()` ([0005](0005-key-hierarchy-and-artifact-size-limit.md)) has to unwrap and
rewrap the DEK, which would require `UPDATE`ing that same row — directly violating the invariant
it was declared next to. Round 2 (blocker C) caught this.

## Decision

Split into two tables:

- `artifact_versions` — immutable: `content_hash`, `storage_key`, `parent_version`,
  `created_by_principal`, `created_by_client`, `created_at`. **Never `UPDATE`d after insert.**
- `artifact_version_envelopes` (1:1, `artifact_version_id` PK) — mutable: `nonce`, `wrapped_dek`,
  `algorithm`, `project_key_version`, `updated_at`. `RotateProjectKey()` `UPDATE`s only this table.

Reading an artifact version joins both tables; writing a new version inserts one row into each
(same DB transaction as the rest of the write path, see
[0013](0013-transactional-outbox-and-storage-lifecycle.md)). Rotating a key never touches
`artifact_versions`, never changes `storage_key`, and never re-encrypts the object in
`ObjectStore`.

## Consequences

- Any code that does `UPDATE artifact_versions` outside the initial `INSERT` is a bug — add a test
  (or a DB trigger, if the false-positive rate stays low enough) that fails if it ever tries.
- Audit records a key rotation as a security operation against `artifact_version_envelopes`, not as
  a new artifact version.
