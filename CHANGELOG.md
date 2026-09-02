# Changelog

All notable changes to Context Commander are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project uses
[semantic versioning](https://semver.org/).

## [Unreleased]

Nothing released yet — accumulate entries here as work lands. At the first
release, this section is renamed `## [x.y.z] — YYYY-MM-DD` with a matching
link definition at the bottom of the file.

### Added

- **Dockerized dev deployment:** a root `Dockerfile` (distroless, static,
  embeds the committed `web/dist` — no Node stage) plus an opt-in `app`
  service in `deploy/docker-compose.yml`, gated behind `profiles: ["app"]`
  so `make dev-up`/`make test` keep only starting Postgres/MinIO. Serves on
  `:8490` (not `8470`, to leave room next to docker-commander on the same
  host) with `--insecure-allow-cleartext` since `0.0.0.0` inside the
  container isn't loopback — Docker's own network isolation (the port is
  published to `127.0.0.1` only) is the actual boundary, same as
  postgres/minio already have. See README "Running entirely in Docker".
- **Web UI v1 follow-ups:** the four items deliberately left out of v1
  (`NEXT.md`) now ship.
  - Gates/Skills tabs gained write actions (bind/unbind, publish/install/
    override/pin/unpin) — the HTTP endpoints already existed; this was a
    frontend-only change (`web/src/pages/project/Gates.tsx`, `Skills.tsx`).
    Also fixed a latent bug found while touching it: the Skills tab read a
    `skill_name` field the backend's `skillBindingResponse` never had, so
    every installed skill's name silently rendered blank.
  - `internal/projects.Store.ListMembers` plus `GET /v1/projects/{ns}/{slug}/members`
    and an MCP `project.list_members` tool — the Members tab was previously
    add-only, with no way to see who's already a member. Gated by
    `auth.CapMemberRead` (granted to every role, unlike `CapMemberManage`),
    so the Members tab now shows the list to a READER too, not just
    ADMIN/OWNER.
  - Self-service PAT issuance, listing, and revocation — `POST/GET
    /v1/me/tokens`, `DELETE /v1/me/tokens/{id}`, a new `/tokens` page in the
    web UI. Deliberately self-only: `internal/identity` has no authz layer
    and `client_credentials` has no RLS, so every handler uses
    `principalIDFrom(ctx)` as the sole target and never a principal id from
    the request body — `RevokeCredential`'s `WHERE id=$1 AND
    principal_id=$2` is what actually enforces this, not a separate
    ownership check. `internal/identity.ValidateExpiresAt` rejects a
    past `expires_at`.
  - Real keyset pagination for the audit log: `internal/audit.QueryOptions.BeforeID`
    (`AND id < $N`, matching `ORDER BY ts DESC, id DESC`), exposed as
    `?before_id=` and driving a "Load more" button on the Audit tab —
    replacing the old hard 500-row cap with no way to see further back.

- **Web management UI (v1):** a React/Vite frontend (`web/`), embedded into
  the `ccd` binary via `go:embed` and served as the SPA fallback of `ccd
  serve` (skip with `--dev` to run `cd web && npm run dev` separately
  instead). Authenticates with a pasted PAT, same as the CLI — there's no
  password/session backend (ADR 0009 keeps PAT issuance CLI-only). Covers a
  project/organization dashboard and, per project, Members (add, role-gated),
  Artifacts (browse/read/version history), Secrets (list/create/delete),
  Skills and Gates (read-only), and an Audit log (ADMIN/OWNER-only, matching
  `CapAuditRead`) with filters.
- Three small HTTP endpoints and domain methods the UI needed and nothing
  had before: `GET /v1/me` (resolve the caller's own identity —
  `identity.Store.GetUser`/`GetServicePrincipal`), `GET /v1/me/projects` and
  `GET /v1/me/organizations` (`projects.Store.ListForPrincipal`,
  `organizations.Store.ListForUser`/`Service.ListMine`), backed by new
  indexes on `project_members(principal_id)` and
  `organization_members(user_id)` (`migrations/000016`). The audit log HTTP
  response also now carries `principal_id`/`actor_email` (batch-resolved via
  `identity.Store.GetUsersForPrincipals`) — it used to drop `Record`'s
  `PrincipalID` entirely, so there was no way to see who did what.
- Domain packages: Handoffs (§9), History/session lineage (§10), Skill
  Registry (§12, ADR 0012), Gates (§13), Secrets (§5.4) — each with a domain
  service, migrations, and HTTP/MCP/CLI adapters.
- Events (§11) delivery: `Service.List` as a poll-only surface over the
  transactional outbox — deliberately no push/wake-up (§11.2) — plus its
  HTTP/MCP/CLI adapters.
- Organizations hardening: `organizations.Capability`/`RoleCan` as a type
  distinct from `internal/auth.Capability` so org-admin authorization can
  never merge with project-content authorization (ADR 0006), plus the
  `TestOrganizationOwnerHasNoImplicitProjectAccess` negative test ADR 0006
  requires.
- Audit expansion (`internal/audit`) with a real service and integration
  tests.
- `docs/testing.md` — the two real test tiers, why integration tests run
  against the RLS-subject `cc_app` role, and where adversarial/negative
  tests live.

### Fixed

- **CI:** `ci` workflow (`lint`, `gitleaks`) failed on every push since
  2026-08-27. `golangci-lint-action@v6` installed the last v1.x binary
  (v1.64.8, built with go1.24), which refuses to run against `go.mod`'s
  `go 1.27.0` and can't load the v2-format `.golangci.yml`; bumped to
  `@v9`, which installs golangci-lint v2. `gitleaks/gitleaks-action@v2`
  requires a paid `GITLEAKS_LICENSE` for org-owned repos; replaced it with a
  direct `gitleaks` CLI install/run step (same open-source scanner, no
  license). The real scan then flagged fixture credentials in
  `secretscanner_test.go`/`engine_integration_test.go`/
  `service_integration_test.go` as false positives — added `.gitleaks.toml`
  allowlisting those three test files. Also bumped `actions/checkout` and
  `actions/setup-go` to v7 (clears the Node 20 deprecation warnings).
- **Security:** organization-scope gate bindings were hidden by RLS from a
  principal who belonged to a project under the organization but had no
  separate `organization_members` row, so a mandatory organization-wide gate
  could silently fail to run for project-only members and service principals.
  Migration `000012` makes organization bindings readable through project
  membership while retaining the original, narrower mutation rules; regression
  tests verify both that the gate executes and that this added read visibility
  cannot be used to delete the binding (`migrations/000012_*`,
  `internal/gates/engine_integration_test.go`,
  `internal/artifacts/service_integration_test.go`).
- **Security:** `skills.Service.Install` and `gates.AdminService.Bind`/`Unbind`
  authorized a project-scope mutation using only the caller-supplied anchor
  project's capability, never checking the capability against `scopeRef` —
  the project the binding was actually written into or deleted from. A
  principal with the right capability on any one project they belong to
  could install a skill into, or unbind a gate from, any other project they
  hold even a READER membership in. `checkScopeTarget` now authorizes
  `scopeRef` directly (`internal/skills/service.go`, `internal/gates/admin.go`).
- **Security:** `artifacts.Service.Write` passed `orgID = nil` to
  `gates.Engine.Evaluate` unconditionally, so an organization-scope gate
  binding (e.g. a mandatory secret scanner) never matched and silently never
  ran on any artifact write. `Write` now resolves the project's real
  organization id before evaluating gates.
- **Security:** `POST /projects/{ns}/{slug}/members` and `project.add_member`
  called `projects.Store.AddMember` directly after only proving the caller
  could resolve the project, so any project member — including a READER —
  could add an arbitrary principal as `OWNER`. Both adapters now check
  `AuthzService.Can(CapMemberManage)` before the mutation
  (`adapters/http/projects.go`, `adapters/mcp/tools_project.go`).
- **Security:** `skills.Service.Override`'s non-overridable check queried
  `skill_bindings` under the caller's own RLS context, which hid an
  organization-scope binding from a project-only admin who was never a
  separate `organization_members` row — so the check silently saw zero rows
  and let the override through, bypassing ADR 0012's `overridable=false`
  guarantee. `Override`/`ListEffective` now derive the project's organization
  server-side (never as adapter-supplied input) and migration `000013` makes
  organization bindings readable through project membership, mirroring
  `000012`'s split for `gate_bindings` (`internal/skills/service.go`,
  `migrations/000013_*`).
- **Security:** a `gate_bindings` row naming an unregistered gate provider
  (a typo, a removed provider, an incomplete deployment) was silently skipped
  during evaluation and returned `ALLOW` — a mandatory security gate could be
  disabled by accident with no error anywhere. `AdminService.Bind` now
  rejects unknown gate names at bind time, `Engine.Evaluate` fails closed
  (errors, never `ALLOW`) on one, and `ValidateRegisteredBindings` checks
  enabled installation-scope bindings at `serve` startup
  (`internal/gates/engine.go`, `internal/gates/admin.go`,
  `internal/gates/gates.go`).
- **Security:** `ccd serve` defaulted to `:8080` with plain `ListenAndServe`,
  so bearer tokens and decrypted project content traveled in cleartext on
  any host that could reach the port. The default is now a loopback bind
  (`127.0.0.1:8080`); a non-loopback `--addr` is refused unless
  `--tls-cert`/`--tls-key` (enabling `ListenAndServeTLS`) or an explicit
  `--insecure-allow-cleartext` override is given (`adapters/cli/serve.go`).
- **Security:** `skill_bindings` had independent `skill_id`/`skill_version_id`
  foreign keys with no relationship between them, so `Install`/`Override`
  could pair one skill's identity with another skill's `SkillVersion` — and
  `ListEffective` groups/shadows by the claimed `skill_id`, handing a
  consumer content that never went through the identity/policy it evaluated.
  `skill_versions` now has a composite `(id, skill_id)` key and
  `skill_bindings` a matching composite FK (migration `000014`); the service
  also validates the pairing itself, returning `ErrSkillVersionMismatch`
  (`internal/skills/service.go`, `migrations/000014_*`).
- **Security:** `skills.Service.Publish` committed the `skill_versions` row
  before uploading its content to `ObjectStore`; a failed upload left a
  permanent, installable version pointing at content that was never written,
  with no retry or repair path. `Publish` now uploads under an immutable
  UUID-based key first and only commits the version row once that succeeds —
  a failed second-phase commit leaves an unreferenced storage object instead
  of a broken installable version (`internal/skills/service.go`).
- **Security:** `history.Service`'s session/history-object mutations accepted
  a caller-supplied `conversation_id`, `parent_external_session_id`,
  `external_session_id`, or `derived_from` with no check that the referenced
  row belonged to the same project (or conversation) — a contributor to one
  project could reference, and probe the existence of, another project's
  lineage. Composite foreign keys now require project (and, where
  conversation-scoped, conversation) to match; a violation maps to
  `ErrInvalidReference` instead of an opaque constraint error
  (`internal/history/service.go`, `migrations/000015_*`).
- Several JSON request bodies (`projects`, `secrets`, `history`,
  `organizations`, `handoffs`) were decoded with no size limit at all, and
  the ones that had one used a bare `io.LimitReader` inconsistently; the
  history endpoints buffered a full conversation's ciphertext and every
  decrypted body in one transaction with no cap, and `events.Service.List`
  passed a caller-supplied `limit` straight to SQL. All JSON bodies now go
  through a shared `http.MaxBytesReader`-backed decoder, `history` gained a
  domain content limit and a bounded `ListHistoryObjects` query, event
  listing is capped the same way `internal/audit` already was, and the HTTP
  server now sets `ReadTimeout`/`WriteTimeout`/`IdleTimeout`
  (`adapters/http/errors.go`, `internal/history/*.go`,
  `internal/events/service.go`, `adapters/cli/serve.go`).
- `secrets.Service.Create` accepted names its own `secrets.Parse` would
  later reject (e.g. a name containing `/`), producing a `Reference` string
  that couldn't round-trip and that HTTP `DELETE` couldn't address either.
  Name validation is now centralized in `secrets.ValidateName` and enforced
  by `Create`/`Delete` (`internal/secrets/reference.go`,
  `internal/secrets/service.go`).
