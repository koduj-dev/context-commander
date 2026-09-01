# Changelog

All notable changes to Context Commander are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project uses
[semantic versioning](https://semver.org/).

## [Unreleased]

Nothing released yet — accumulate entries here as work lands. At the first
release, this section is renamed `## [x.y.z] — YYYY-MM-DD` with a matching
link definition at the bottom of the file.

### Added

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
