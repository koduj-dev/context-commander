# Gotchas worth remembering

Things that cost real debugging time here, kept so they cost it once. This is
engineering knowledge about *this* codebase — not a style guide, and not a list
of everything that could go wrong. If something bit you and the cause was
non-obvious, it belongs here.

Nothing is filed yet — most of the sharp edges found so far (the RLS bootstrap
gap for project creation, the `SET LOCAL`-vs-pooled-connection principal
context, the `''::uuid` cast on an untouched GUC) turned out to be design
decisions worth their own rationale, so they live as ADRs instead:
[`docs/adr/0002`](adr/0002-non-recursive-rls.md),
[`docs/adr/0003`](adr/0003-set-local-transaction-context.md),
[`docs/adr/0015`](adr/0015-project-creation-rls-bootstrap.md). Use this file
for the smaller stuff that doesn't rise to an ADR — a tooling surprise, a test
fixture trap, a footgun in a dependency — the way
[docker-commander's gotchas.md](https://github.com/koduj-dev/docker-commander/blob/main/docs/gotchas.md)
does.

## Skill Registry content is unencrypted in v1

`internal/crypto.Provider.Encrypt` requires an `EncryptionContext.ProjectID`
— the key hierarchy is root key -> **project** key -> DEK
(docs/adr/0005), with no installation- or organization-level key. An
`internal/skills.SkillVersion` bound at `ScopeInstallation` or
`ScopeOrganization` has no project to anchor a key to, so `Service.Publish`
stores its content in `storage.ObjectStore` unencrypted regardless of
scope, rather than encrypting only project-scoped skills and leaving a
two-tier trust story.

`docs/architecture-cookbook.md` §14.4 lists "private skill data" among
things that should be encrypted — that stays open until an
installation-level key primitive exists. Don't assume skill content is
protected the way artifact/handoff/history content is.
