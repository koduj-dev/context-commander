# 0037. Where the root key comes from: environment, file, or Vault

## Status

Accepted, 2026-11-17. Phase 3's "the root key comes only from an
environment variable".

## Context

The root key wraps every project key (ADR 0005). It could only be given as
`CC_ROOT_KEY`, so it sat in `.env` or `ccd.env` on the server, in the
process environment, and in every backup of those files. Container
platforms and systemd have better places for a secret than an environment
file, and a team with HashiCorp Vault wants the key to exist in plaintext
nowhere on disk.

## Decision

`crypto.LoadRootKey` reads a root key from exactly one of:

| Variable | What it holds |
|---|---|
| `CC_ROOT_KEY` | the key, base64 (as before) |
| `CC_ROOT_KEY_FILE` | a path to a file holding the key: a Docker or Kubernetes secret, or a systemd credential (`LoadCredential=` and `CC_ROOT_KEY_FILE=%d/root-key`) |
| `CC_ROOT_KEY_VAULT_CIPHERTEXT` | the key encrypted by Vault's Transit engine; the server asks Vault to decrypt it at startup |

More than one, or none, is a refusal to start. The same applies to
`CC_ROOT_KEY_PREVIOUS` during a rotation. Every command that needs the key
reads it this way, and `ccd serve` logs which source it used, never the key.

**Vault Transit, not Vault KV.** With KV, the key is stored in Vault and
read back in plaintext over the network. With Transit, the key is
encrypted once by the operator:

```bash
vault write -field=ciphertext transit/encrypt/context-commander plaintext="$CC_ROOT_KEY"
```

and the server holds only the ciphertext and a token allowed to decrypt
with that one Transit key. Revoking the token stops a stolen configuration
from yielding the key. The settings are Vault's own: `VAULT_ADDR`,
`VAULT_TOKEN` or `VAULT_TOKEN_FILE`, `VAULT_NAMESPACE`, `VAULT_CACERT`, plus
`CC_ROOT_KEY_VAULT_KEY` (default `context-commander`) and
`CC_ROOT_KEY_VAULT_MOUNT` (default `transit`). It speaks Vault's HTTP API
directly; no Vault library is added.

**The fingerprint does not depend on the source.** `root_key_ref` is a hash
of the key, so moving the same key from the environment to a file or to
Vault changes nothing in the database and needs no rotation.

## Consequences

- The key can leave environment files, and with Vault, the disk.
- The key is still read into the server's memory at startup: an attacker
  who controls the process has it, as before (SECURITY.md).
- Not done: cloud KMS services (AWS KMS, Google Cloud KMS, Azure Key Vault).
  Each needs its provider's SDK; the same shape (a ciphertext the KMS
  decrypts at startup) will fit when one is asked for.
