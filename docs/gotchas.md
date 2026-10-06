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

## Two kinds of credential in one table

A browser session and a personal access token are the same row shape
(`client_credentials`, `docs/adr/0019`). That is deliberate — one verification
path, one revocation story — and it has exactly one consequence worth
remembering: **`client_type` is the only thing telling them apart, and two
readers must disagree about which rows they want.**

- `ListTokensForPrincipal` excludes sessions. The token screen must not offer
  someone the credential they are currently signed in with, from the screen
  that needs it.
- `ListSessionsForPrincipal` returns only sessions.
- `ListCredentialsForPrincipal` is unfiltered and is the **admin** view. A
  session is exactly what an incident asks about, so filtering it there would
  hide the thing being investigated.

A new reader of this table has to pick one of the three on purpose. There is
no safe default.

## The password hash is also a denial-of-service vector

argon2id is slow and memory-hungry **because** that is what protects a leaked
hash, and the live server pays the same cost per attempt. At
`--max-concurrent-auth`'s default of 64, with 64 MiB per verification, an
unauthenticated caller could reserve 4 GiB by starting sign-ins.

So password verifications have their own, much smaller ceiling
(`--max-concurrent-password-hash`, default 4) and are refused with a retry
rather than queued — queueing holds the request open without reducing the
memory in flight, which is the thing being bounded. If the cost parameters in
`internal/identity/password.go` are ever raised, this ceiling has to come down
with them.

The related trap: **the per-credential rate limiter cannot see a brute-force
attempt at all.** A credential id is what a caller receives *after* signing
in, so every failed guess belongs to no credential and passes straight
through. `AllowSignIn` is keyed on the identifier that was typed, lowercased
and trimmed — key it on anything resolved from the database and an address
with no account gets a different allowance from one with an account, which
turns the limiter itself into an account-enumeration oracle.

## `pkill -f` matches the shell that runs it

`pkill -f "bin/ccd serve"` kills the current shell too: the pattern appears in
that shell's own command line, so it matches itself. It has happened twice in
this project, the second time with `pkill -f "addr 127.0.0.1:12002"`. Use
`pkill -x ccd`, or find the pid with `pgrep -ax` and kill that.

And before killing the dev instance at all, read `.cc-dev/README.md` — it
holds the exact command to start it again, including the environment the
process needs. That file exists because the instance was once killed without
it.

## `ccd migrate` runs the migrations the BINARY holds

Migrations are embedded (`migrations/embed.go`, `go:embed`), so `ccd migrate`
applies whatever was compiled into the binary that runs it — not what is in
the working tree. Writing a migration and running `./bin/ccd migrate` without
rebuilding applies nothing.

And it says nothing: the command prints `migrations applied` whether it
applied twelve or zero. The symptom is a handler that 500s on a column the
schema does not have, which is exactly what happened with
`service_principals.created_by` (migration 000026) — twenty minutes spent
looking at Go code for a fault that was a missing `go build`.

Rebuild first, or check the column exists:

    go build -o bin/ccd ./cmd/ccd && ./bin/ccd migrate
    docker exec context-commander-postgres-1 \
      psql -U cc_owner -d context_commander -c "\d service_principals"

## `docker compose down -v` is project-wide, not service-scoped

`docker compose -f deploy/docker-compose.yml --profile ldap down -v` destroyed
`cc_postgres_data` — the whole dev database. Neither `--profile` nor naming a
service narrows `down`: it brings down every service in the compose *project*,
and `-v` removes every volume that project owns.

To recreate one service's state, remove that one volume by name:

    docker compose -f deploy/docker-compose.yml --profile ldap stop ldap
    docker compose -f deploy/docker-compose.yml --profile ldap rm -f ldap
    docker volume rm context-commander_cc_ldap_data context-commander_cc_ldap_config

The reason this came up: osixia/openldap runs its bootstrap LDIFs **once**, on
a fresh volume, so changing a seed file has no effect until the volume is
gone. That makes "wipe and restart" the normal way to iterate on the seed —
which is exactly how a careless `-v` ends up in a command.

What survived, and what that is worth knowing: `.cc-dev/` lives on the host,
so the object store and the root key were untouched. They were also useless —
the project keys and every row of metadata were in the database, so the
remaining ciphertext had nothing left to be read with. Encrypted bytes without
their key hierarchy are not a backup.

## osixia/openldap bootstraps once, and its ACL is on `{1}mdb`

Two traps that both fail silently, found while seeding the dev directory
(`deploy/ldap/`):

**The LDIFs run only on a fresh volume.** Editing a seed file and restarting
does nothing; the tree is whatever the first start produced. Iterating means
removing that service's volumes — and `down -v` is not how (see above).

**An `olcAccess` change must target `olcDatabase={1}mdb,cn=config`.** Getting
the index wrong means the modify applies to a DN that does not exist, the
bootstrap skips it without complaint, the image's default ACLs stay in force,
and a search as the service account returns:

    SEARCH RESULT tag=101 err=32 nentries=0

`err=32` is `noSuchObject`. It reads as "my seed data did not load" when the
data is there and merely unreadable — OpenLDAP answers `noSuchObject` rather
than `insufficientAccess` on purpose, so as not to disclose what exists. If a
bind as the service account succeeds and its searches come back empty, suspect
the ACL before the data.

Also: `replace: olcAccess` replaces *all* of them, including the rule that
grants the local root manage access over `ldapi` — drop it and the container's
own `ldapsearch -Y EXTERNAL`, the tool you reach for to diagnose this, stops
working too.
