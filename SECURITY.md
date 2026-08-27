# Security Policy

## Reporting a vulnerability

Please report suspected security vulnerabilities privately via a GitHub Security Advisory
(`Security` tab → `Report a vulnerability`) rather than a public issue. Do not include working
exploit code or real credentials in the report — describe the class of problem and, where
possible, a minimal reproduction against a disposable local instance.

## Scope and threat model

Context Commander's default deployment mode ("standard mode") allows the server to process
plaintext after authorized decryption — this is a deliberate tradeoff to support search, previews,
secret scanning, and server-side transformations. It is **not** a zero-knowledge system in this
mode. Application-level encryption (see `docs/adr/0003-key-hierarchy.md`) protects against:

- stolen disks, leaked backups, database dumps,
- leaked object-store contents,
- a storage administrator without decryption keys.

It does **not** protect against:

- an attacker who fully controls the running server process,
- a compromised host with access to decryption keys,
- a malicious authorized user,
- data already sent to an external model/vendor by a client,
- a secret an agent copies into a permitted artifact (mitigated, not prevented, by the secret
  scanner gate).

Project membership is the primary authorization boundary (see `docs/adr/0001-*.md` and
`docs/architecture-cookbook.md` §4). Row-Level Security is defense-in-depth on top of the
application authorization service, not a replacement for it.

## Supported versions

Pre-1.0: only the latest commit on `main` is supported.
