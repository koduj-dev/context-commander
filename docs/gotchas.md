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

## Who can read a skill's content

Reading a skill version's bytes (`skills.Service.GetVersion`, `skill.get`)
needs two things: `CapSkillRead` on an anchor project, and the version itself
being readable by you. The second rule is:

> You may read a skill's content if you can see a binding for it, or if you
> are a member of the project it was published from.

`skills` and `skill_versions` carry no RLS (migrations/000007), so the anchor
project alone protects nothing — the caller picks it. When `skill.get` first
shipped there was no second check, and any principal holding `CapSkillRead`
anywhere (every role does) could read any version whose UUID it knew,
including one published inside a project it was not a member of. Confirmed by
probe, then closed by migration 000020, which records the publishing project
the `Publish` call already knew and was discarding.

Consequences worth knowing:

- An **installation-scope binding is visible to every principal**, so a skill
  installed installation-wide is readable installation-wide. That is the
  intended registry behaviour, not a leak.
- A version published **before** migration 000020 has no recorded provenance
  and is read as installation-wide. Only rows predating that migration can be
  in this state.
- This does **not** decide what the skill registry *is*. `docs/adr/0012`
  scopes the *binding* (where a skill applies), not the package (where it
  lives). Whether packages are instance-wide, organization-scoped or
  explicitly promoted is still open — migration 000020 records the provenance
  any of those answers would need.
