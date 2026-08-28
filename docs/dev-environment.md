# Dev environment notes

How the maintainer's machine is set up, and what the test suite expects
locally. Nothing here is required to *use* Context Commander — it's here so a
development session doesn't start by rediscovering it.

For behaviours of the app itself that are easy to get wrong, see
[Gotchas](gotchas.md) (currently empty — see that file for why).

## Requirements

- **Go 1.27+** (`go.mod` pins `go 1.27.0`).
- **Docker + Docker Compose** — needed for local Postgres/MinIO (`make dev-up`)
  *and* for the test suite: `make test` runs real integration tests against a
  disposable Postgres via `testcontainers-go` (`internal/dbtest`), not mocks.
  Multi-tenant RLS bugs only show up against the real thing — see
  `docs/architecture-cookbook.md` "Testování" and
  [ADR 0002](adr/0002-non-recursive-rls.md).

## Local services

`make dev-up` starts Postgres (`:5432`) and MinIO (`:9000`, console `:9001`)
via `deploy/docker-compose.yml`. Both are named-volume-backed, not bind-mounted
into the repo — a container-owned bind mount under the repo tree breaks
`go build ./...` (permission denied walking container-owned files).

Two DB roles matter, defined in `deploy/postgres/init/01-roles.sql`
([ADR 0002](adr/0002-non-recursive-rls.md)):

| Role | Used for | RLS |
|---|---|---|
| `cc_owner` | owns every table/function; migrations run as this role | exempt (table owner) |
| `cc_app` | the only role the running server (and `ccd`) connects as | enforced |

```bash
export CC_DATABASE_URL=postgres://cc_owner:cc_owner_dev_password@localhost:5432/context_commander?sslmode=disable
./bin/ccd migrate

export CC_DATABASE_URL=postgres://cc_app:cc_app_dev_password@localhost:5432/context_commander?sslmode=disable
./bin/ccd bootstrap --email you@example.com --name "Your Name"
```

Test fixtures follow the same split (`internal/dbtest.DSNs{Owner, App}`):
seed setup data through `Owner` (RLS-exempt), but the actual behaviour under
test runs against `App` — the same role production connects as — or an RLS
test proves nothing.

## Build / test / lint

- `make build` → `./bin/ccd`.
- `make test` → `go test ./... -race -count=1`. Needs Docker running
  (testcontainers).
- `make lint` → `golangci-lint run ./...` (must be installed separately).
- `make tidy` → `go mod tidy`.
