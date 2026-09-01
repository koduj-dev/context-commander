# How Context Commander is tested

This is a multi-tenant service where the entire point is that one project cannot
see another project's data. That's exactly the kind of bug a mock database won't
catch, so this page describes the actual test tiers that exist in the repository
today, what each one proves, and what runs where.

## The tiers

| Tier | What it proves | Where | Needs |
|---|---|---|---|
| **1 · Unit** | Logic, parsing, pure decision functions (e.g. `auth.RoleCan`, `organizations.RoleCan`) with no I/O | plain `*_test.go` files across the package | nothing |
| **2 · Integration** | Real Postgres, real RLS, real authorization — no mocks | `*_integration_test.go` in every domain package, via `internal/dbtest` | Docker |

Both tiers run together: `make test` is `go test ./... -race -count=1`, and CI
(`.github/workflows/ci.yml`) runs the same command unconditionally — GitHub's
`ubuntu-latest` runner has a Docker daemon available, so there is no `-short`
split to skip the integration tier in CI the way some sibling projects do.
Locally, integration tests need Docker running (`make dev-up` starts the same
Postgres/MinIO compose stack, but the integration tests spin up their **own**
disposable container per run via `testcontainers-go` — they don't reuse
`dev-up`'s instance).

`make lint` (`golangci-lint`, config in `.golangci.yml`) and a `gitleaks` secret
scan run as separate CI jobs alongside `test`.

## Why integration tests use a real Postgres, not mocks

`internal/dbtest.New` starts a disposable `postgres:16-alpine` container, applies
`deploy/postgres/init/01-roles.sql` (the same `cc_owner`/`cc_app` role split as
production) and every embedded migration, then hands the test two DSNs:

- **`Owner`** — RLS-exempt, table-owning role. Fixture setup (inserting rows to
  test against) uses this.
- **`App`** — the role the production server actually connects as, subject to
  Row-Level Security. The behavior under test runs against this one — an
  assertion made against `Owner` proves nothing about RLS, since `Owner` bypasses
  it.

This matters because the authorization model here isn't only "checked in Go
code": every domain service checks a capability (`docs/adr/0014`) *and* runs
inside `db.WithPrincipal`, which sets the Postgres session's principal via
`SET LOCAL` so RLS enforces the same boundary independently (`docs/adr/0002`,
`docs/adr/0003`). A bug that only breaks one of those two layers — the Go check
or the RLS policy — needs a real Postgres connection with a real session to
surface. This is also why `docs/adr/0006`'s "org owner has no implicit project
access" rule and `docs/adr/0014`'s "authz check lives in the domain service, not
the adapter" rule are both things a mocked-DB test cannot actually verify.

## Adversarial tests live next to the feature, not in a separate tier

Unlike a separate pentest suite, the tests that assert an attack is *rejected*
sit in the same `*_integration_test.go` file as the feature they guard, named
for what they deny — e.g. `TestReaderCannotWrite`, `TestNonMemberDeniedCrossProject`,
`TestRLSIsolationBetweenProjects`, `TestOrganizationOwnerHasNoImplicitProjectAccess`,
`TestPublishDeniedWithoutCapability`. Every domain package that checks
capabilities carries at least one RLS cross-project isolation test and one
role-denial test. When adding a new domain service or a new capability, add
both: a positive test that the intended role can act, and a negative test that
every other role/non-member cannot.

`adapters/http/http_test.go` additionally asserts `TestNonMemberGetsNotFoundNotForbidden`
— the HTTP layer must not distinguish "exists but you can't see it" from
"doesn't exist" in its response, since a 403-vs-404 split leaks whether a
project/resource ID exists to a caller who has no business knowing.

## What isn't covered yet

- No fuzzing.
- No load/soak testing.
- No multi-node or failover testing — this is a single-process server today.
- Migrations are exercised by every integration test run (they're applied
  fresh against a disposable container each time) but there's no dedicated
  test for migrating an existing populated database forward.
