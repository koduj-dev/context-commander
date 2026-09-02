# Context Commander

Self-hosted, vendor-neutral shared context and collaboration layer for humans and AI agents.

> **Agents are clients. Context is the product.**

## The problem

It is easy to use several AI tools — ChatGPT, Claude Code, Codex, custom agents — but hard to make
them cooperate. They don't share a neutral project state: research done in one chat has to be
manually copied into another tool, context gets re-explained, artifacts get shuffled by hand.

Context Commander gives independent AI clients a common, project-scoped workspace: artifacts,
context, handoffs, skills, events, history, and access control — owned by the project, not by any
single AI vendor.

## Non-goals

Context Commander deliberately does **not** try to be:

- **an agent orchestrator** — it does not decide which model runs, which agent wakes up next, or
  how a multi-agent reasoning loop works. It exposes events and handoffs that an orchestrator can
  consume, but does not require one.
- **an S3/MCP filesystem wrapper** — `list/read/write/delete` alone would just be another
  object-store MCP server. The product is *project context*: membership, artifacts, handoffs,
  skills, events, history, and integrations.
- **a Git replacement** — Git remains the correct system for source code and source history.
  Context Commander stores AI-generated artifacts, research, handoffs, and cross-agent
  collaboration state, and may bind a project to one or more repositories as metadata.

See [`docs/architecture-cookbook.md`](docs/architecture-cookbook.md) for the full design rationale
and [`docs/adr/`](docs/adr/) for individual architecture decisions.

## Status

Design draft → implementation. See the ADRs in `docs/adr/` for what is decided and why.

## Architecture

Modular monolith written in Go. Domain logic lives under `internal/`; `adapters/` maps that domain
onto MCP, HTTP, and CLI without duplicating business logic.

```text
context-commander/
├── cmd/ccd/            single binary: server + migrations + CLI
├── internal/           domain packages (identity, projects, crypto, artifacts, ...)
├── adapters/           mcp / http / cli — transport only, no business logic
├── migrations/         golang-migrate SQL migrations
├── deploy/             docker-compose for local development
└── docs/               architecture cookbook + ADRs
```

## Development

Requires Go 1.27+, Docker, and Docker Compose (`make test` also needs Docker — the integration
suite runs against a real, disposable Postgres via testcontainers-go, not mocks).

```bash
make dev-up      # start Postgres + MinIO
make build       # build ./cmd/ccd -> bin/ccd
make test        # run the test suite (unit + real-Postgres integration tests)
make lint        # golangci-lint
```

Migrations and the first user/PAT are handled by the binary itself, no external tool or UI
required:

```bash
export CC_DATABASE_URL=postgres://cc_owner:cc_owner_dev_password@localhost:5432/context_commander?sslmode=disable
./bin/ccd migrate

export CC_DATABASE_URL=postgres://cc_app:cc_app_dev_password@localhost:5432/context_commander?sslmode=disable
./bin/ccd bootstrap --email you@example.com --name "Your Name"
```

### Running entirely in Docker

`make dev-up` only starts Postgres/MinIO — everyday Go dev and `make test` don't need the app
itself containerized. To run `ccd` (server + embedded web UI) in Docker too, opt into the `app`
profile explicitly:

```bash
cd deploy
cp .env.example .env               # then fill in CC_ROOT_KEY, e.g. `openssl rand -base64 32`
docker compose up -d postgres      # if not already running
docker compose --profile app run --rm \
  -e CC_DATABASE_URL=postgres://cc_owner:cc_owner_dev_password@postgres:5432/context_commander?sslmode=disable \
  app migrate
docker compose --profile app run --rm app bootstrap --email you@example.com --name "Your Name"
docker compose --profile app up -d --build app
```

The UI is then at <http://127.0.0.1:8490>. Rerun the last command (with `--build`) after pulling
code changes.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow and review discipline, and
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for community expectations. Please report security issues
privately — see [SECURITY.md](SECURITY.md), not a public issue.

## License

[GNU AGPL-3.0](LICENSE). This does **not** restrict commercial use or running a competing service —
it is a network copyleft license: anyone who runs a modified version of Context Commander over a
network must make the corresponding source of their modifications available to its users. See
[`docs/adr/0005-agpl-3.0-license.md`](docs/adr/0005-agpl-3.0-license.md) for the full rationale.
