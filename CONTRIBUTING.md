# Contributing

Context Commander is early-stage; expect the domain model to still be settling. Read
[`docs/architecture-cookbook.md`](docs/architecture-cookbook.md) and the [ADRs](docs/adr/) before
proposing a change to `internal/` — several early design mistakes (global unique project slugs,
`ServicePrincipal` as provenance-only, self-recursive RLS policies) were caught in review and are
now documented as invariants specifically so they don't come back.

## Design rules

1. **Project is the normal authorization boundary.** Don't add per-file ACLs casually.
2. **Provider IDs are opaque external metadata.** Never use a vendor session ID as canonical
   Context Commander identity.
3. **MCP is an adapter.** Domain logic in `internal/` must stay usable from HTTP/CLI too — never
   import an `adapters/*` package from `internal/`.
4. **Skills add capability/knowledge; Gates constrain behavior.** Don't merge those abstractions.
5. **Security enforcement happens server-side.** Prompt guidance is not authorization.
6. **Secrets are not artifacts.** Use secret references/providers.
7. **RLS principal context is `SET LOCAL` inside a transaction, never session-level `SET`.** Only
   `internal/db.WithPrincipal()` may set it.
8. **`ArtifactVersion` is immutable.** Key rotation touches `artifact_version_envelopes`, never
   `artifact_versions`.
9. **`SkillVersion` is immutable.** `skill.update` touches only draft/metadata; new content always
   goes through `skill.publish`.

The full, current list of domain invariants lives in the implementation plan (ask a maintainer for
the link) and should be mirrored into ADRs as the corresponding subsystem is implemented.

## Workflow

```bash
git config core.hooksPath .githooks   # enable local gitleaks pre-commit (one-time)
make lint    # golangci-lint — must pass before a PR
make test    # go test ./... — must pass before a PR
```

- New tables that carry `project_id` must enable Row-Level Security using the
  `cc_has_project_access()` pattern from `docs/adr/0002-non-recursive-rls.md` and get at least one
  negative cross-project test.
- Never commit secrets, real tokens, or `.env` files. `gitleaks` runs in CI and as a pre-commit
  hook; a finding blocks the PR.
