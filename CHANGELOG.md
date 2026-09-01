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
