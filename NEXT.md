# What's next

**This file is only about work that has not been done.** What already exists is
described where it belongs and nowhere else:

- **What the design decided, and why** → [`docs/architecture-cookbook.md`](docs/architecture-cookbook.md) and [`docs/adr/`](docs/adr/)
- **What shipped, and when** → [CHANGELOG.md](CHANGELOG.md)
- **Things that bit us** → [docs/gotchas.md](docs/gotchas.md)
- **How this machine is set up** → [docs/dev-environment.md](docs/dev-environment.md)

If an item below ships, delete it — the changelog and the ADRs are the record.
Keeping shipped work here makes the roadmap grow instead of shrink.

## 🎯 What we want from it

A self-hosted, vendor-neutral shared context and collaboration layer for humans
and AI agents: a common project-scoped workspace to store artifacts, preserve
context, hand work between agents, share skills, and publish events — with
project-level RBAC and no dependency on any single AI vendor. See the cookbook
§1 for the full definition.

The design (cookbook + 16 ADRs) is settled and every domain it describes is
now built, with HTTP/MCP/CLI adapters. What's left is engineering-docs parity,
release/packaging wiring, and whatever the maintainer decides to harden or
extend next.

---

## 🔭 Open work

### Known security gap

- **Organization-scope gate bindings are invisible to a project member who
  isn't also an organization member.** `gate_bindings` RLS
  (`migrations/000008_gates.up.sql`) requires `cc_has_org_access` for
  `scope = 'organization'` rows — i.e. the *writing* principal must have an
  `organization_members` row for that org, independent of their
  `project_members` role on the project actually being written to. Found
  2026-09-01 while regression-testing the `orgID = nil` fix in
  `artifacts.Service.Write` (see CHANGELOG "Fixed"): an org-wide policy
  (e.g. a mandatory secret scanner) silently does not apply to a project
  contributor who was never added to the organization itself, even though
  their project lives under it. Fixing this means changing
  `gate_bindings_scoped`'s USING clause for `scope = 'organization'` to key
  off the *artifact's project's* organization rather than the acting
  principal's own org membership — a migration change, not just Go code, so
  it needs its own review pass rather than folding into the authz-bypass fix
  it was found alongside.

### Engineering docs parity with docker-commander

Same "sibling project" doc style, adopted as the repo actually grows into it:

- [x] `docs/testing.md` — the two real tiers (unit, testcontainers-go Postgres
  integration), why integration tests run against the RLS-subject `cc_app`
  role rather than `cc_owner`, and where the adversarial/negative tests live.
- [ ] `docs/README.md` as a manual index — now that the cookbook, ADRs,
  gotchas, dev-environment and brand docs all exist, a manual index is no
  longer premature.

### Packaging and distribution

- [x] OS packaging scaffolding (`deploy/nfpm.yaml`, `deploy/packaging/`).
- [ ] **Not yet wired to a release workflow** — no `release.yml` builds or
  publishes the packages, and there's no man page. Revisit once CC is near a
  shippable state.

---

## 📦 Backlog

Nothing parked here yet.

---

## ❓ Open questions

- **Packaging: copy-adapt vs. a shared template repo.** Parked on
  rule-of-three — only two sibling `koduj-dev`-style repos exist
  (docker-commander, context-commander) and their packaging will differ
  meaningfully (binary name, config paths, dependency story). Revisit if a
  third project needs the same shape. (Decided 2026-08-28.)
- **Release process weight.** docker-commander's `git-workflow` skill
  (release branches, CI gate, PR template, tag→`release.yml`) is calibrated
  to a project already shipping regular releases. CC adopted only the
  commit-message convention and CHANGELOG-under-`[Unreleased]` habit for now;
  revisit the rest once CC has an actual release cadence. (Decided
  2026-08-28.)

## 🧭 Deliberately not doing

- **Skill content encryption at installation/organization scope.** The key
  hierarchy (root key → project key → DEK, ADR 0005) has no
  installation-level key primitive, so `internal/skills.Service.Publish`
  stores non-project-scoped skill content unencrypted rather than building a
  two-tier trust story for it. See [docs/gotchas.md](docs/gotchas.md). Open
  until an installation-level key primitive exists to anchor it to.

---

## 🗺️ Working priority order

**Proposed 2026-09-01 — not yet confirmed with the maintainer.** Everything
the previous priority order tracked (Handoffs, Events, History,
Organizations hardening, Audit expansion, Skill Registry, Gates, Secrets,
adapter catch-up) shipped in a single pass — see `CHANGELOG.md`. What's left
is smaller and mostly independent:

- [ ] `docs/README.md` manual index.
- [ ] Packaging release wiring (`release.yml`, man page) — once CC is near a
  shippable state; the scaffolding itself already exists (see above).
- [ ] Anything the maintainer wants hardened or extended next — no
  identity-defining domain gap remains, so this is now maintainer's call
  rather than a design-implied sequence.
