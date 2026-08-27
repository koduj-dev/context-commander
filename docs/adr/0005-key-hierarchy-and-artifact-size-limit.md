# 0005. Envelope encryption key hierarchy, AAD binding, and MVP artifact size limit

## Status

Accepted

## Context

`docs/architecture-cookbook.md` §14 requires application-level envelope encryption but round 1
review (blocker 3) found the original schema had no explicit *project key* object, so "KEK per
project" and its rotation were undefined. Round 2 (blocker C, §10) added two more requirements:
key rotation must never mutate an immutable `ArtifactVersion`, and AES-GCM ciphertext must be bound
to its context via AAD or a valid ciphertext could theoretically be replayed against a different
DB record. Round 2 §5/§8 (implementation addenda) additionally closed the streaming-vs-limit
question left open in round 1.

## Decision

**Three-level key hierarchy:**

```text
root/installation key (KMS/Vault/env provider — never stored in DB)
        │  wraps
        ▼
project_keys (verzovaná per-project, wrapped_key + root_key_ref)
        │  wraps
        ▼
DEK per artifact_version (in artifact_version_envelopes, not artifact_versions)
```

- `RotateRootKey(tx, previous RootKeyProvider)` rewraps every `project_keys` row in place (same
  `key_version`, new `wrapped_key`/`root_key_ref`); no DEK, no ciphertext, no
  `artifact_versions`/`artifact_version_envelopes` row changes. It takes the outgoing root key
  explicitly as `previous`, separate from the `Provider`'s own (incoming) `RootKeyProvider` — found
  necessary by testing, not designed up front: once a root key stops being "current," nothing else
  retains access to it (the whole point of never storing it in the DB), so a rotation is the one
  operation that legitimately needs both the old and new key material at once, and the interface
  has to say so explicitly rather than pretend `CurrentKey()` alone can serve both roles. If a row's
  `root_key_ref` matches neither `previous` nor the current key, rotation fails loudly for that row
  instead of silently mismatching. This is a cross-project maintenance operation — it runs against
  every `project_keys` row regardless of project, so it executes with an RLS-exempt
  (`cc_owner`-connected) transaction, not through the normal per-principal `WithPrincipal` path
  (docs/adr/0002 — RLS is scoped per-principal-per-project by design, which a "rotate for the whole
  installation" operation doesn't fit, and shouldn't be forced into).
- `RotateProjectKey(projectID)` **creates a new, additional `project_keys` version** — it does not
  touch any existing `artifact_version_envelopes` row. Revised from the original design (which had
  this eagerly rewrap every existing DEK) once implementation surfaced two problems with that: it
  made rotation an O(artifacts-in-project) operation instead of O(1), and it made
  `internal/crypto` (Phase 2) depend on `artifact_version_envelopes` (Phase 3), which doesn't need
  to exist yet. Instead, rotation matches how AWS/GCP KMS key rotation actually works: a new key
  version is created, **new** encryptions use it, and every existing `ArtifactVersionEnvelope` keeps
  referencing whatever `project_key_version` encrypted it — `Decrypt` looks that exact version up
  (`project_keys` rows are never deleted), so old envelopes stay decryptable indefinitely without
  any rewrap. Eagerly migrating existing envelopes to the latest key version (e.g. for
  crypto-shredding an old version) is a distinct, separately-invoked maintenance operation, not an
  implicit consequence of "rotate."

**AAD**: every `Encrypt`/`Decrypt` call takes an `EncryptionContext{FormatVersion, ProjectID,
ResourceType, ResourceID, ResourceVersion}`, canonically encoded (fixed field order, fixed-width
encoding — never ad-hoc string concatenation) and passed as AES-GCM's additional authenticated
data. A ciphertext produced for `project A / artifact_version X / v3` fails to decrypt under
`project B / artifact_version Y / v7` even with the correct key.

**Nonce**: a fresh random 96-bit nonce per encryption operation, stored alongside the ciphertext.
Reusing a (key, nonce) pair is a hard requirement to avoid — tested with a large-N uniqueness check
in CI, not just asserted.

**`content_hash`**: computed over plaintext *before* encryption (not the storage backend's ETag,
which only hashes ciphertext). Whether it's a public `SHA-256` or a project-scoped keyed
fingerprint (HMAC) is left as a follow-up decision — a public hash is a mild
known-plaintext-detection oracle against a leaked metadata DB, not a blocker.

**MVP artifact size limit — decided (round 2 addendum 2): 50 MB per artifact**, configurable, not
hardcoded. `internal/storage.ObjectStore` stays `io.Reader`-based (streaming-friendly), but
`internal/crypto.CryptoProvider` operates on `[]byte` in v1. A chunked/streamed AEAD container
format is not designed now — it gets designed against real usage data if the 50 MB limit turns out
to matter, not speculatively before the first artifact exists.

## Consequences

- `internal/crypto.CryptoProvider` signature takes `EncryptionContext`, not a bare `projectID`.
- Any subsystem that encrypts content-bearing payloads (artifacts, handoffs, history — see
  [0011](0011-encryption-classification.md)) constructs its own `EncryptionContext` with its own
  `ResourceType`; there is exactly one crypto entry point, not one per subsystem.
- `artifact.write` for a payload over 50 MB returns a clear `413`-equivalent error, not an OOM.
