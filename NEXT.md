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
now built, with HTTP/MCP/CLI adapters, plus a web management UI. What's
left is engineering-docs parity, release/packaging wiring, and whatever the
maintainer decides to harden or extend next.

---

## 🔭 Open work

### CI/pipeline hardening (blocked on going public)

`ci` workflow (`lint`/`test`/`gitleaks`) is green again as of 2026-09-02 (see
CHANGELOG). Left over, blocked on the planned move to a public repo:

- [ ] **Branch protection / rulesets on `main`** — `koduj-dev` is on the
  GitHub Free org plan; branch protection on a *private* repo 403s
  ("Upgrade to Pro"). Free once the repo goes public. Add required status
  checks (`lint`, `test`, `gitleaks`) at that point.
- [ ] **Native GitHub secret scanning / push protection** — same story,
  requires GitHub Advanced Security on a private repo, free automatically on
  public. `gitleaks` in CI covers the gap meanwhile.
- [ ] **Restrict `allowed_actions` from `all`** — currently any Action from
  any source can run in this repo's workflows; worth narrowing to verified
  creators once public and visible to more eyes.
- [ ] `CODEOWNERS` — not critical with a single maintainer, but cheap to add.

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

- **Real cursor pagination for list endpoints.** `internal/audit.Query` got
  a real keyset cursor (`before_id`, 2026-09-02) wired through to the web
  UI's Audit tab "Load more" button — the pattern to copy for the rest.
  `history.Service.ListHistoryObjects` is still just capped at
  `MaxListLimit` (1000 rows/call) rather than unbounded, and
  `events.Service.List` caps `limit` the same pre-audit way — neither has a
  cursor yet, and `artifacts`/`handoffs`/`secrets` listing have no
  pagination at all. A conversation/project with more than one page's worth
  of rows currently has no way to see the rest of those. Worth doing
  properly (keyset cursor, not offset) before any of these lists can grow
  past a single page in practice.

---

## ❓ Open questions

- **Single-binary deployment.** docker-commander ships as one binary
  (`modernc.org/sqlite`, no CGO, no external services). CC needs Postgres
  (RLS-based multi-tenancy) and MinIO/S3 (artifact storage), so `ccd` alone
  isn't enough to run it — maintainer would like it to be, eventually.
  Genuinely hard: Postgres can't be embedded the way SQLite can, and RLS is
  load-bearing for the whole authz model (see cookbook). Would need either
  an embedded-Postgres story (e.g. bundling/managing a Postgres process) or
  rethinking multi-tenancy for a non-RLS backend — not a small change.
  Raised 2026-09-02, not scoped yet.
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
adapter catch-up) shipped in a single pass — see `CHANGELOG.md`. The web UI
v1 shipped the same day, and its four follow-ups (gates/skills write
actions, member listing, self-service PAT issuance, audit pagination)
shipped 2026-09-02 — see `CHANGELOG.md`. What's left is smaller and mostly
independent:

- [ ] `docs/README.md` manual index.
- [ ] Packaging release wiring (`release.yml`, man page) — once CC is near a
  shippable state; the scaffolding itself already exists (see above).
- [ ] Anything the maintainer wants hardened or extended next — no
  identity-defining domain gap remains, so this is now maintainer's call
  rather than a design-implied sequence.
