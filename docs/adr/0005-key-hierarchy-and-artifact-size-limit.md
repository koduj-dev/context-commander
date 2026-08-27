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

- `RotateRootKey()` rewraps every `project_keys` row; no DEK, no ciphertext, no
  `artifact_versions`/`artifact_version_envelopes` row changes.
- `RotateProjectKey(projectID)` creates a new `project_keys` version and rewraps that project's
  DEKs — i.e. it `UPDATE`s `artifact_version_envelopes` rows, never `artifact_versions` rows. See
  [0010](0010-immutable-artifact-version.md) for why that split exists.

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
