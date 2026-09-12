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

## Who can see a skill

A skill **lives at a scope** — the project it was published from, an
organization, or the installation-wide shelf (`docs/adr/0018`, migration
000021). Visibility follows that scope, and is enforced by Row-Level Security
on `skills`, with `skill_versions` inheriting through its parent, using the
same policy `skill_bindings` has always had.

Two traps worth knowing:

- **A binding does not grant visibility.** `SkillBinding.Scope` says where a
  skill is *applied*; `Skill.Scope` says who can *see* it. They use the same
  vocabulary and mean different things. Installing a skill at installation
  scope does not put it on the marketplace shelf — `skill.promote` does.
- **Promotion is one-way.** A skill only ever widens its scope. Narrowing is
  refused: anyone who could already see it may have installed it.

Skill content is stored **unencrypted** (see the encryption note above: the
key hierarchy has no installation-level key to anchor it to). A project-scoped
skill is therefore only as private as the database itself. Anything that must
be confidential belongs in `internal/secrets`, which is project-key encrypted
and RLS-scoped — never in a skill package.
