# Dev environment notes

How the maintainer's machine is set up, and what the test suite expects
locally. Nothing here is required to *use* Context Commander — it's here so a
development session doesn't start by rediscovering it.

For behaviours of the app itself that are easy to get wrong, see
[Gotchas](gotchas.md) (currently empty — see that file for why).

## Requirements

- **Go 1.27+** (`go.mod` pins `go 1.27.0`).
- **Docker + Docker Compose** — needed for local Postgres (`make dev-up`) and for the
  containers the integration tests start themselves
  *and* for the test suite: `make test` runs real integration tests against a
  disposable Postgres via `testcontainers-go` (`internal/dbtest`), not mocks.
  Multi-tenant RLS bugs only show up against the real thing — see
  `docs/architecture-cookbook.md` "Testování" and
  [ADR 0002](adr/0002-non-recursive-rls.md).

## Local services

`make dev-up` starts Postgres (`:5432`). Two more services sit behind profiles because
nothing in everyday work needs them: `--profile s3` for an S3 server to poke
`internal/storage/s3` at (`:9090`), and `--profile ldap` for a seeded directory to develop
LDAP sign-in against (`:1389`)
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

## Running `ccd` itself in Docker

`Dockerfile` (repo root) builds a static, distroless image — the committed
`web/dist` is embedded via `go:embed` at build time, so the image needs no
Node stage. `deploy/docker-compose.yml`'s `app` service runs it, but sits
behind a `profiles: ["app"]` gate so a plain `docker compose ... up` (what
`make dev-up` runs) keeps only starting Postgres — bring `app` up with
`--profile app` explicitly. See the README's "Running entirely in Docker"
for the exact commands; port `8490` (not `8080`) to leave room next to
docker-commander's `8470` if both run on the same host.

`ccd serve`'s own refusal to bind a non-loopback address without TLS
(`isLoopbackAddr` in `adapters/cli/serve.go`) is why the image's `CMD` passes
`--insecure-allow-cleartext` — `0.0.0.0` inside the container is not
loopback, but the port is only published to `127.0.0.1` on the host (same
posture as postgres above), so Docker's own network isolation is the
actual boundary, same as it is for those two.

## The LDAP directory for working on sign-in

`docker compose -f deploy/docker-compose.yml --profile ldap up -d` brings up a
seeded slapd on `127.0.0.1:1389`. Then:

```bash
CC_LDAP_BIND_PASSWORD=ldap-dev-password \
./bin/ccd serve --addr 127.0.0.1:8080 \
  --ldap-url ldap://127.0.0.1:1389 \
  --ldap-bind-dn "cn=readonly,ou=services,dc=example,dc=test" \
  --ldap-user-base "ou=people,dc=example,dc=test"
```

Every seeded account's password is `ldap-dev-password`. The four people exist
to cover what breaks code rather than to look like a company:

| uid | what it is for |
|---|---|
| `alice`, `bob` | ordinary entries, with `mail` and `displayName` |
| `carol` | **no `mail` attribute** — provisioning must refuse with a reason, not crash and not invent an address |
| `dave` | `mail` recorded in a different case from how anyone types it |

`cn=readonly,ou=services,…` is the service account the server searches as;
binding as `cn=admin` would work and is exactly the habit not to teach, since
a directory *read* does not need write access.

The integration tests (`internal/directory`) start their own container from
the same `deploy/ldap/*.ldif`, so they need Docker but not this service. If
you edit a seed file, see `docs/gotchas.md` — the image bootstraps once, on a
fresh volume, and the way to redo it is **not** `down -v`.

## The OIDC provider for working on sign-in

`docker compose -f deploy/docker-compose.yml --profile oidc up -d` brings up a
Keycloak with the `context-commander` realm on `127.0.0.1:18081`. The command
that points `ccd serve` at it, the seeded users and what each one is for, and
the traps that had to be measured rather than assumed are all in
[`deploy/oidc/README.md`](../deploy/oidc/README.md) — kept there, next to the
realm file, rather than copied here where it would drift.

The integration tests in `internal/directory` start their own Keycloak from
the same realm file, so they need nothing from compose; it adds about thirty
seconds to that package's run.
