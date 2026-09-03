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

### UI redesign per docs/context-commander-ui-spec.md

The maintainer added a full UI spec (`docs/context-commander-ui-spec.md`,
2026-09-02) describing a much larger target than what exists today —
brand/theme system, shared component library, new top-level nav
(Projects/Organizations/Skill Registry/Administration), new project tabs
(Overview/Handoffs/Conversations/Events/Integrations/Settings), and an
Administration surface for admin-driven user creation + agent (Service
Principal) registration. Phased rather than built at once — see
[`docs/plans/2026-09-02-design-system-phase-1.md`](docs/plans/2026-09-02-design-system-phase-1.md)
for the phase 1 plan (design system + component library, no backend/new
pages). All three phases have since shipped — see `CHANGELOG.md`. What's
left of the original spec is Organizations/Skill Registry top-level nav and
the remaining project tabs (Integrations/Settings) — not yet planned.

- [x] **Phase 1 — design system + component library.** Shipped 2026-09-03 —
  see `CHANGELOG.md`.
- [x] **Phase 2 — wire existing-but-unused backend into new project tabs.**
  Shipped 2026-09-03 — see `CHANGELOG.md`. Surfaced a real backend gap while
  building the Conversations tab: **there is no "list conversations"
  endpoint** (`internal/history` only exposes create + operate-on-a-known-id;
  same for external sessions, which have no list endpoint either). The tab
  works around this the same way Skills' install form already does for
  "no browse skills endpoint" — create or paste an id directly — but a
  project with existing conversations has no way to discover them from the
  UI. Worth a real `GET .../conversations` (and maybe `.../sessions`) list
  endpoint in a later pass.
- [x] **Phase 3 backend — installation-admin authorization tier.** Shipped
  2026-09-03 — see `CHANGELOG.md` and `docs/adr/0017-installation-admin-tier.md`.
  Backend only (confirmed with the maintainer before starting): a new
  `internal/installation` package, `POST /v1/installation/users`,
  `POST /v1/installation/service-principals`, cross-principal token
  issue/list/revoke, `GET /v1/installation/admins`, matching CLI
  (`ccd installation ...`) and MCP tools, and `internal/skills`/
  `internal/gates`' installation-scope bindings now go through a real
  capability check instead of the old hard `ErrUnsupportedScope` rejection.
  Granting/revoking installation-admin rights stays CLI/
  `--database-url`-only (`ccd installation grant-admin`/`revoke-admin`) —
  no HTTP/MCP path lets one admin mint another, a deliberate choice, not an
  oversight (ADR 0017).
- [x] **Phase 3 UI — Administration page.** Shipped 2026-09-03 — see
  `CHANGELOG.md`. `GET /v1/me` gained `is_installation_admin` (a second,
  separate transaction from the rest of `getMe`, via the new
  `installation.Service.IsAdmin`, ungated — it's the caller's own status,
  same reasoning as `organizations.Service.ListMine`) — the web UI's only
  signal for whether to show the "Administration" nav entry / route at
  all. `web/src/pages/Administration.tsx`: admins list (read-only — no
  grant/revoke button, by design), create-user form, register-service-
  principal form, and a "load a principal's tokens" section reusing
  `ConfirmDialog` for revoke, same as `AccountTokens.tsx`.
- [ ] **Organization "list members" endpoint** — same gap this session
  just closed for projects (`internal/projects.Store.ListMembers`,
  `GET .../members`) doesn't exist yet for `internal/organizations`;
  `POST /v1/organizations/{id}/members` (add) exists, list doesn't. Small,
  same pattern to mirror — candidate for whichever phase touches
  Organizations UI.

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
- [x] Dev Docker image (`Dockerfile`, repo root) + opt-in `app` service in
  `deploy/docker-compose.yml` (2026-09-02) — see README "Running entirely in
  Docker". Not a release artifact (no registry publish, no version
  tagging beyond `docker` as a placeholder `-X main.version=`) — that's
  the release-workflow item below.
- [ ] **Not yet wired to a release workflow** — no `release.yml` builds or
  publishes the packages (OS packages or the Docker image), and there's no
  man page. Revisit once CC is near a shippable state.

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
  Raised 2026-09-02, not scoped yet. The new `Dockerfile`/`app` compose
  service (same date, see Packaging above) is the "one `docker compose up`"
  answer for now — still two containers (Postgres + `ccd`) plus MinIO, not
  the single-binary-no-deps story this question is actually about.
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
