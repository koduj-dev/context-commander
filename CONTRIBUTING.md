# Contributing to Context Commander

Thanks for your interest in improving Context Commander! This guide covers how to build, test,
and submit changes.

By participating you agree to follow our [Code of Conduct](CODE_OF_CONDUCT.md).

Context Commander is early-stage; expect the domain model to still be settling. Read
[`docs/architecture-cookbook.md`](docs/architecture-cookbook.md) and the [ADRs](docs/adr/) before
proposing a change to `internal/` — several early design mistakes (a globally unique project slug,
`ServicePrincipal` as provenance-only, a self-recursive RLS policy, a domain service trusting RLS
alone with no `AuthzService` call) were caught across two rounds of review and implementation-time
testing, and are now documented as invariants specifically so they don't come back.

## Ways to contribute

- **Report bugs** and **request features** via [issues](../../issues) (use the templates). For
  security problems, **do not** open a public issue — see [SECURITY.md](SECURITY.md).
- **Improve docs** under [`docs/`](docs/), the ADRs, or the README.
- **Send pull requests** — for anything non-trivial, please open an issue first so we can agree on
  the approach before you invest the work.

## Project layout

```text
cmd/ccd/            # main: cobra root, wires adapters/cli into the binary
internal/           # domain packages (identity, organizations, projects, auth, crypto,
                    #   storage, artifacts, db, audit, events, ...) — see each package's doc.go
adapters/           # mcp / http / cli — transport only, never business logic
migrations/         # golang-migrate SQL, embedded into the binary (`ccd migrate`)
deploy/             # docker-compose for local dev (Postgres + MinIO)
docs/adr/           # one file per load-bearing decision — read before touching internal/
```

The production artifact is a **single Go binary** (`ccd`) combining the server, migrations, and
admin CLI — see `docs/architecture-cookbook.md` §21.

## Development setup

You need **Go ≥ 1.27** and a running **Docker daemon** (local dev services, and the test suite —
see below).

```bash
git clone git@github.com:koduj-dev/context-commander.git
cd context-commander
make dev-up                                              # Postgres + MinIO
make build                                                # -> bin/ccd
CC_DATABASE_URL=postgres://cc_owner:cc_owner_dev_password@localhost:5432/context_commander?sslmode=disable \
  ./bin/ccd migrate
CC_DATABASE_URL=postgres://cc_app:cc_app_dev_password@localhost:5432/context_commander?sslmode=disable \
  ./bin/ccd bootstrap --email you@example.com --name "Your Name"
```

## Tests

```bash
make test    # go test ./... -race — needs Docker
make lint    # golangci-lint
```

There is no unit/integration split yet (no `-short` tier): every package's tests either need no
backend at all (pure logic — `internal/crypto`'s AEAD/AAD tests, `internal/identity`'s PAT tests)
or spin up their own disposable Postgres/MinIO via `testcontainers-go`
(`internal/dbtest`) — real backends, not mocks, because RLS/transaction/encryption bugs don't
reproduce against a mock. `make test` runs everything; there's nothing slower to opt out of yet.
If that stops being true (a genuinely slow suite shows up), split it then, not preemptively.

**Adding a test? Prove it can fail.** Comment out or revert the fix, watch the test fail, check it
failed for the *right* reason, then restore. A test that has never failed for the reason it claims
to guard against is not proof of anything.

## Review discipline

- **Before a commit** — read your own diff for correctness *and* security (auth/permissions, input
  handling, secret exposure, unsafe defaults) and fix what you find.
- **New project-scoped table? Same RLS pattern, every time.** `ENABLE ROW LEVEL SECURITY` +
  the identical `cc_has_project_access(...)` policy shape from
  [`docs/adr/0002-non-recursive-rls.md`](docs/adr/0002-non-recursive-rls.md), a `GRANT` to `cc_app`
  scoped to what it actually needs (no blanket `UPDATE`/`DELETE` on tables meant to be immutable —
  see [`docs/adr/0010`](docs/adr/0010-immutable-artifact-version.md)), and at least one negative
  cross-project test that runs *as `cc_app`*, not as `cc_owner` — a test run under the owner role
  proves nothing about RLS.
- **New domain service touching project-scoped data? It calls `AuthzService.Can()` itself**, at the
  top of every method, before the database — see
  [`docs/adr/0014-authz-check-in-domain-service.md`](docs/adr/0014-authz-check-in-domain-service.md)
  for why that belongs in the service, not only in a future adapter middleware.
- **A decision worth an ADR, not just a comment:** anything that changes a domain invariant, a
  table's RLS/privilege shape, or a security-relevant default. Follow the existing numbered
  `docs/adr/NNNN-*.md` files' format; link back to what it supersedes or refines if it revises an
  earlier one, rather than silently contradicting it.

## Code style

- **Go must be `gofmt`-clean** (`gofmt -l $(git ls-files '*.go')`) and `go vet ./...` clean; CI
  enforces both, plus `golangci-lint`.
- Write code that reads like the code around it — match naming, comment density, and idioms. Keep
  comments about *why* (a non-obvious constraint, a bug a design avoids), not *what* the
  well-named code already says.

## A few project conventions

- **Domain packages depend on repository interfaces they define, not on `pgx` types leaking in
  from callers** — see [`docs/adr/0007`](docs/adr/0007-metadata-store-abstraction-sqlite.md) on
  why (a second, non-Postgres backend is planned, not designed blind before a real second
  implementation exists).
- **Provenance is `Principal` (User or ServicePrincipal), never a bare `User`** — see
  [`docs/adr/0004`](docs/adr/0004-principal-model.md). A `created_by_*` column, an RLS session
  variable, anything identifying "who did this" goes through `principals`, not `users`, directly.
- **`SkillVersion`/`ArtifactVersion` are immutable once inserted.** Don't add an `UPDATE` code path
  to either — mutable state that legitimately changes (crypto wrapping, skill installation/pinning)
  lives in a separate table on purpose.

## Working on the interface

The design system lives in two files that must agree:
[`docs/mockups/assets/mockup.css`](docs/mockups/assets/mockup.css) is where a
component is decided, and `web/src/styles/app.css` is the same system in the
application. `npm test` in `web/` fails when they drift — it also checks that
no colour is spelled as a hex literal outside the brand tokens, that no
component styles itself inline, and that every translation still matches the
English source.

Read [`.claude/skills/ui-design/SKILL.md`](.claude/skills/ui-design/SKILL.md)
before touching a stylesheet, a screen or a mockup. It carries the rules and
the reasons: colour comes only from `docs/brand`, English is the origin
language, sentences are never assembled from fragments, dates and byte sizes
are formatted from raw values rather than sent as strings, lists page by
cursor and never by page number, and the interface never offers a control for
something the backend cannot do.

## Before a structural change

Read [`docs/CURRENT-ARCHITECTURE.md`](docs/CURRENT-ARCHITECTURE.md) first. It
carries the precedence rules between the architecture documents, the ADRs and
the code, the decisions that are settled and should not be re-opened, and the
register of places where the architecture and the repository currently differ
on purpose.

The rule that matters most: **absence from an architecture document is not a
reason to delete implemented behaviour.** When the documents and the code
disagree, classify the drift before changing either.

## Pull requests

1. Branch off `main`.
2. Keep commits focused; write imperative, descriptive messages with a leading `[add]`/`[fix]`/
   `[wip]`/`[refactor]`-style tag (see recent `git log` for the convention).
3. Make sure `make test` and `make lint` pass.
4. Update `docs/architecture-cookbook.md`/add an ADR for load-bearing changes.
5. Open the PR against `main` and fill in the template.

## License

By contributing, you agree that your contributions are licensed under the project's
[GNU AGPL-3.0](LICENSE) — see [`docs/adr/0008`](docs/adr/0008-agpl-3.0-license.md) for what that
does and does not restrict.
