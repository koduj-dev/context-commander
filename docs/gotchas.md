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

## A concurrency test must hold its transactions open before the barrier

Releasing N goroutines from a `sync.WaitGroup` barrier and *then* letting each
open its own transaction does not test concurrency — it mostly tests that
`pgxpool` opens connections one at a time. The first goroutine gets a warm
connection and finishes its entire transaction while the rest are still doing
TCP setup and authentication, so a read-modify-write race is never reached.

`TestConcurrentFirstEncryptProvisionsOneProjectKey`
(`internal/crypto/concurrency_integration_test.go`) passed against genuinely
broken code for exactly this reason before the barrier moved *inside* the
`db.WithoutPrincipal` callback, where every writer signals only once its
transaction is open. With the barrier in the right place, 4 of 16 writers fail
on the unique constraint every run.

Two consequences worth copying into the next such test:

- the pool needs `MaxConns >= writers` (via `db.NewPoolFromConfig`), or
  goroutines holding transactions deadlock against goroutines that cannot get a
  connection;
- before trusting a concurrency regression test, run it against the *unfixed*
  code and watch it fail. Several tests written for this file looked correct
  and proved nothing.

## Advisory lock classes live in one registry

Postgres advisory locks share one flat, cluster-wide key space, so unrelated
call sites can serialize against each other — or deadlock — by accident.
`internal/db/advisory.go` declares every `LockClass` used anywhere in the
codebase in a single const block for that reason. Add new ones there, and
check the nesting order documented on `AdvisoryXactLock` before taking two.

## Skill content is readable installation-wide, not per-project

`skills` and `skill_versions` have no RLS (migrations/000007), and
`skills.Service.GetVersion` authorizes against an *anchor* project the caller
holds `CapSkillRead` on — which every project role has — rather than against
the skill being read. A principal who knows a skill version's UUID can
therefore read its content even if it was published inside a project they are
not a member of. Verified, not theoretical.

migrations/000007 decided that the *catalog* is installation-wide ("every
authenticated principal in this installation can see what skills exist"). It
did not decide that content is, and until `skill.get` shipped in Phase 1 the
content was unreachable through every adapter, so nobody had to answer the
question. The read path answered it in the widest direction by default.

**Until this is narrowed, do not put project secrets in skill content.** Use
`internal/secrets` for anything that must stay inside one project — it is
project-key-encrypted and RLS-scoped, which skill content is neither (see also
the encryption note above: skill content is stored unencrypted because the key
hierarchy has no installation-level key to anchor it to).

Closing it means allowing a read only when the caller can see a binding for
the skill or published the version themselves, which needs a `published_by`
column on `skill_versions` — tracked in NEXT.md.
