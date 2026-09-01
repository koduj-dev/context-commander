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

### Engineering docs parity with docker-commander

Same "sibling project" doc style, adopted as the repo actually grows into it:

- [ ] `docs/testing.md` — test tiers (unit + testcontainers-go Postgres
  integration per `docs/architecture-cookbook.md` "Testování" / the `test`
  Makefile target) — worth writing now that every domain has an integration
  test suite worth describing.
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

- [ ] `docs/testing.md`, `docs/README.md` manual index.
- [ ] Packaging release wiring (`release.yml`, man page) — once CC is near a
  shippable state; the scaffolding itself already exists (see above).
- [ ] Anything the maintainer wants hardened or extended next — no
  identity-defining domain gap remains, so this is now maintainer's call
  rather than a design-implied sequence.
