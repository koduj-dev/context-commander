# What's next

**This file is only about work that has not been done, and the product
direction that decides its order.** What already exists is described where it
belongs and nowhere else:

- **What the design decided, and why** → [`docs/architecture-cookbook.md`](docs/architecture-cookbook.md) and [`docs/adr/`](docs/adr/)
- **What shipped, and when** → [CHANGELOG.md](CHANGELOG.md)
- **Things that bit us** → [docs/gotchas.md](docs/gotchas.md)
- **How this machine is set up** → [docs/dev-environment.md](docs/dev-environment.md)

If an item below ships, delete it — the changelog and the ADRs are the record.
Keeping shipped work here makes the roadmap grow instead of shrink.

> Merged 2026-09-11 from the former `next.md` (product strategy, 2026-09-01)
> and this file (technical backlog). Two files differing only in case are a
> hazard on case-insensitive filesystems, and the strategy was what decided
> the backlog's order anyway. This is now the single roadmap file.

---

## 🎯 Positioning

Context Commander is a **self-hosted, vendor-neutral layer for handing project
context between different coding agents** — not an agent orchestrator, not a
general knowledge-management system, not "memory for AI". See cookbook §1 for
the full definition and §3 for the non-goals.

The strongest entry use case is deliberately narrow:

> Agent A finishes work and stores a verifiable handoff plus shared context;
> Agent B, on a different client, picks it up minutes later with no manual
> copy-paste and no loss of provenance.

The boundary that keeps this honest: **Context Commander owns durable, shared,
auditable state; the client owns reasoning, planning, and doing the work.**

Strategic alternatives considered on 2026-09-01 and their verdicts:

| Direction | Verdict |
|---|---|
| Shared context + handoff between agents | **Primary** |
| Multi-agent orchestrator/scheduler | Not now — conflicts with the declared non-goals |
| Enterprise governance/control plane | Later monetization layer; nothing to govern without an active workflow |
| General "AI memory"/vector database | No — dilutes the differentiator, makes relevance/search the product |
| Skill registry + policy marketplace | Only after the core loop works and there is distribution |

### Primary user

Not an enterprise administrator. A technical user or small engineering team
that runs more than one coding agent, switches between sessions/machines/
vendors, keeps re-explaining project state, doesn't want sensitive context in
another SaaS, and will tolerate a self-hosted Docker install *if the first
value arrives fast*. Their job-to-be-done: *"when I switch agents or hand work
to a colleague, I want to safely restore state, decisions, work in progress and
the next step without manually reconstructing a conversation."*

The platform/security lead is a secondary persona — gates, org scope, audit
export and credential management matter to them later, but that persona does
not create daily usage.

### The risk this roadmap exists to correct

Domain breadth got ahead of user experience. The design (cookbook + 17 ADRs) is
settled and every domain it describes is built, with HTTP/MCP/CLI adapters and
a web management UI — but there is still **no single operation that assembles
the right starting context for a new agent**, a skill can be installed but its
content can't be read back, a handoff can be created but never claimed or
closed, and several lists have no cursor. The most important product change is
not another subsystem: it is assembling the existing parts into one short,
safe, measurable story — **store state → hand off work → resume exactly**.

---

## 🔭 Open work

### Phase 0 — trustworthy foundation (release blocker)

The remaining P1 findings from the 2026-09-01 review. Each was re-verified
against `main` on 2026-09-11; the ones that shipped are gone from this list.

- [ ] **Concurrent writes to the same secret lose one.**
  `internal/secrets.LocalEncryptedBackend.Store` does `SELECT id, version` and
  then an `INSERT ... ON CONFLICT DO UPDATE` in the same transaction with no
  `FOR UPDATE` and no advisory lock, so two writers rotating the same secret
  both read version *N*, both write *N+1*, and one value is silently dropped.
  The AAD stays consistent (both bound *N+1*), so this is a lost update, not a
  decryption failure — which is exactly why nothing surfaces it. Serialize on
  `(project_id, name)`.
- [ ] **Unified keyset pagination** — see Backlog below. Listed here too
  because without it we cannot promise history stays reachable under real use,
  and that is a release-blocking claim, not a nice-to-have.

Already fixed, kept only so the next reader doesn't re-derive it: MCP sessions
are bound to the authenticated principal per HTTP request
(`adapters/mcp/mcp.go`, the server is constructed closed over `cred.PrincipalID`);
organization-scope gate bindings are derived from the caller's org membership,
not a client parameter (`internal/gates/admin.go`); PAT list/revoke shipped
and `identity.ValidateExpiresAt` rejects a non-future expiry; granting OWNER
now requires OWNER at both project and organization scope, checked in
`internal/members.Service`/`internal/organizations.Service` rather than in each
adapter (see CHANGELOG).

### Phase 1 — the first genuinely usable vertical slice

Goal: two clients complete a full cross-client handoff. **No web UI work is
needed in this phase** — a good CLI, good MCP tools and a ten-minute quickstart
carry more information than another page.

- [ ] **Read a skill's content (`skill.get`).** `internal/skills.Service`
  exposes `Publish`/`Install`/`Override`/`Pin`/`Unpin`/`ListEffective` and
  nothing that returns the bytes. `SkillBinding` carries a `StorageKey`, so
  the content is in `storage.ObjectStore` and simply unreachable through any
  adapter. An agent can therefore install a skill it can never load — the
  single most glaring hole in the current surface.
- [ ] **Aggregated `context.bootstrap` / `context.pull`.** One well-described
  MCP operation that assembles the starting context, with detail tools kept
  alongside it, so an agent doesn't need to know the internal domain model
  before its first useful step. Not a new source of truth — an aggregated
  **read model** over existing domains: project identity and the caller's
  role; the current/open handoff; selected artifacts by manifest or
  convention; the *content* of effective skills, not just their UUIDs; recent
  relevant events plus a cursor to continue from; and an explicit size budget
  with a statement of what was truncated.
- [ ] **Handoff lifecycle.** `internal/handoffs.Service` hardcodes
  `'open'` on create and has no transition anywhere — there is no claim or
  close endpoint, which is why the web UI's Handoffs tab only ever shows and
  creates. Define open → claimed/acknowledged → closed, plus a "current
  handoff" query.
- [ ] **One supported Docker Compose quickstart** — server, PostgreSQL,
  persistence; bootstrap and client connection must be copy-paste. The
  `Dockerfile` + `profiles: ["app"]` service (see Packaging) is the raw
  material; the quickstart itself isn't written.
- [ ] **Pick and test two concrete MCP client implementations**, and keep
  their configuration as an executable smoke test or at least a versioned
  fixture. Guarantee two named clients rather than a vague "works with any
  MCP".
- [ ] **E2E scenario:** client A writes, client B reads/takes over with no
  shared token, a revoked token fails.
- [ ] **Align docs with the storage backend that is actually wired.**
  `adapters/cli/serve.go` only ever constructs `filesystem.New(storageDir)` —
  `internal/storage/s3` has no caller in `serve`, so the MinIO service in
  `deploy/docker-compose.yml` isn't used by the running app at all. Either
  wire S3 and integration-test it, or mark it experimental and say so in the
  README and cookbook §8.1.

### Phase 2 — relevance and daily use

Only after the core loop is proven:

- [ ] Declarative project manifest naming "must-read" artifacts and their budgets.
- [ ] Metadata/full-text retrieval filtered by type, author, time, provenance.
- [ ] Compaction/summarization as a derived history object, never overwriting
      the source (cookbook §10.3 already decided this shape).
- [ ] Subscriptions, or reliable cursor polling, for changes during a session.
- [ ] Diagnostics: "why does this context pack contain exactly these items?"
- [ ] Project export/import for backup and migration.

Add vector search only if measurement shows metadata and text search are
insufficient. It is not a precondition for first value.

### Phase 3 — team and enterprise layer

Only on top of a core loop people actually use:

- [ ] Org-level credential management, policy, audit export.
- [ ] SSO/OIDC and short-lived credentials.
- [ ] Retention rules, legal hold, controlled purge workflow.
- [ ] Signed/trusted skill packages and an approval workflow.
- [ ] Operational telemetry that doesn't leak content.

### UI — remaining spec surface

`docs/context-commander-ui-spec.md` (2026-09-02) describes a larger target than
what exists. Phases 1–3 of it shipped (design system, Handoffs/Conversations/
Events tabs, Administration page — see `CHANGELOG.md`). Deliberately parked
behind Phase 1 above, per "no web UI before the CLI/MCP core loop is proven":

- [ ] Organizations / Skill Registry top-level nav, and the remaining project
  tabs (Integrations, Settings) — not yet planned.
- [ ] **`GET .../conversations` list endpoint** — surfaced while building the
  Conversations tab: `internal/history` only exposes create and
  operate-on-a-known-id, and external sessions have no list endpoint either.
  The tab works around it the way Skills' install form does ("no browse
  endpoint" → paste an id), so a project with existing conversations has no
  way to discover them from the UI.
- [ ] **Organization "list members" endpoint** — the gap closed for projects
  (`projects.Store.ListMembers`, `GET .../members`) doesn't exist for
  `internal/organizations`; add exists, list doesn't. Small, same pattern to
  mirror — candidate for whichever phase touches Organizations UI.

### CI/pipeline hardening (blocked on going public)

`ci` (`lint`/`test`/`gitleaks`) is green as of 2026-09-02. Left over, blocked
on the planned move to a public repo:

- [ ] **Branch protection / rulesets on `main`** — `koduj-dev` is on GitHub
  Free; branch protection on a *private* repo 403s ("Upgrade to Pro"). Free
  once public. Add required status checks (`lint`, `test`, `gitleaks`) then.
- [ ] **Native GitHub secret scanning / push protection** — same story,
  requires Advanced Security while private, free on public. `gitleaks` covers
  the gap meanwhile.
- [ ] **Restrict `allowed_actions` from `all`** — any Action from any source
  can currently run here; narrow to verified creators once public.
- [ ] `CODEOWNERS` — not critical with a single maintainer, but cheap.

### Engineering docs parity with docker-commander

- [ ] `docs/README.md` as a manual index — now that the cookbook, ADRs,
  gotchas, dev-environment and brand docs all exist, an index is no longer
  premature.

### Packaging and distribution

- [x] OS packaging scaffolding (`deploy/nfpm.yaml`, `deploy/packaging/`).
- [x] Dev Docker image (`Dockerfile`) + opt-in `app` service in
  `deploy/docker-compose.yml` — see README "Running entirely in Docker". Not a
  release artifact (no registry publish, no version tagging beyond `docker` as
  a placeholder `-X main.version=`).
- [ ] **Not yet wired to a release workflow** — no `release.yml` builds or
  publishes the packages (OS packages or the Docker image), and there's no man
  page. Revisit once CC is near a shippable state.

---

## 📦 Backlog

- **Adding or removing a project/organization member writes no audit row.**
  Surfaced while closing the ADMIN→OWNER escalation: granting a role is
  exactly the kind of privilege change `internal/audit` exists to record, and
  "trace the origin of a change without SQL" is a stated v0.1 metric below —
  but neither `internal/members.Service.Add` nor
  `organizations.Service.AddMember` calls `audit.Write`. Cheap: one
  `audit.Write` in the same transaction as the insert, the pattern
  `internal/artifacts.Service.Write` already uses. Deliberately left out of
  the escalation fix to keep that change to the security boundary.
- **Real cursor pagination for list endpoints.** `internal/audit.Query` got a
  real keyset cursor (`before_id`, 2026-09-02) wired through to the web UI's
  Audit tab "Load more" — the pattern to copy.
  `history.Service.ListHistoryObjects` is still just capped at `MaxListLimit`
  (1000 rows/call), `events.Service.List` caps `limit` the same pre-audit way,
  and `artifacts`/`handoffs`/`secrets` listing have no pagination at all. A
  project with more than one page of rows has no way to see the rest. Keyset,
  not offset. Promoted into Phase 0 above as a release blocker.

---

## 🎯 Definition of Done for v0.1

v0.1 is done when:

- every P1 and scope-related P2 finding is fixed **and has a regression test**;
- a new user completes the supported quickstart with no manual SQL;
- two separate credentials and two clients complete the reference handoff
  scenario;
- the second client reads the actual content of artifacts *and* effective
  skills;
- every growing list endpoint has a limit and a cursor;
- PATs can be listed and revoked, and revocation takes effect immediately;
- a backup/restore procedure for database and storage is documented for at
  least the reference deployment;
- the docs don't describe an inactive storage backend or a future feature as
  done.

## 📏 Metrics and validation

For the first design partners, measure outcomes, not feature count:

- **Time to first handoff** — quickstart to takeover by the second client;
  target under 10 minutes.
- **Resume success** — the second client correctly describes state,
  constraints and next step with no follow-up copy-paste.
- **Manual context transfers** — text pasted between clients; target zero in
  the reference scenario.
- **Context efficiency** — how much of what was loaded was actually useful.
- **Recovery** — the user can revoke a token and trace the origin of a change
  without SQL.
- **Retention** — several design partners use it weekly, not just install once.

Suggested validation experiment: take one medium-sized real repository with a
half-finished task; let client A implement the first part and create a handoff;
give client B access to the Context Commander project only, *not* A's
transcript; measure time to correct continuation, missing information and
manual interventions; repeat with 3–5 design partners. Every missing datum is
first answered with a better model/context pack, not a new general platform
feature.

---

## ❓ Open questions

- **First guaranteed client pair.** Pick after a short compatibility spike;
  guarantee two concrete implementations instead of a generic "works with any
  MCP". (Raised 2026-09-01.)
- **Handoff vs. context pack as the headline object.** Recommendation: sell the
  outcome ("handoff"), keep the context pack as the mechanism. (Raised
  2026-09-01.)
- **Filesystem vs. S3 in v0.1.** Recommendation: one very well tested
  filesystem quickstart; mark S3 experimental until it is genuinely wired into
  `serve` and integration-tested. (Raised 2026-09-01 — see the Phase 1 item.)
- **Name and positioning.** Keep "Context Commander", but narrow the subtitle
  to "shared project context for coding agents"; avoid sounding like an
  orchestrator. (Raised 2026-09-01.)
- **Single-binary deployment.** docker-commander ships as one binary
  (`modernc.org/sqlite`, no CGO, no external services). CC needs Postgres
  (RLS-based multi-tenancy) and MinIO/S3 (artifact storage), so `ccd` alone
  isn't enough to run it — the maintainer would like it to be, eventually.
  Genuinely hard: Postgres can't be embedded the way SQLite can, and RLS is
  load-bearing for the whole authz model (see cookbook). Would need either an
  embedded-Postgres story or rethinking multi-tenancy for a non-RLS backend —
  not a small change. Raised 2026-09-02, not scoped. The `Dockerfile`/`app`
  compose service is the "one `docker compose up`" answer for now — still two
  containers plus MinIO, not the single-binary-no-deps story this question is
  about.
- **Packaging: copy-adapt vs. a shared template repo.** Parked on
  rule-of-three — only two sibling `koduj-dev`-style repos exist
  (docker-commander, context-commander) and their packaging differs
  meaningfully. Revisit if a third project needs the same shape. (Decided
  2026-08-28.)
- **Release process weight.** docker-commander's `git-workflow` skill (release
  branches, CI gate, PR template, tag→`release.yml`) is calibrated to a project
  already shipping releases. CC adopted only the commit-message convention and
  the CHANGELOG-under-`[Unreleased]` habit; revisit once CC has a release
  cadence. (Decided 2026-08-28.)

## 🧭 Deliberately not doing

- **No agent runtime, planner, scheduler or task queue of our own.**
- **No further domain types** until the current ones form a finished workflow.
- **No large web UI** before the CLI/MCP core loop is validated.
- **Not selling this as general "memory for AI".**
- **No public skill marketplace** before trust, signing and distribution are
  solved.
- **No multi-region/hosted SaaS optimization** until the self-hosted install
  has active design partners.
- **Endpoint count is not a progress metric.**
- **Skill content encryption at installation/organization scope.** The key
  hierarchy (root key → project key → DEK, ADR 0005) has no installation-level
  key primitive, so `internal/skills.Service.Publish` stores non-project-scoped
  skill content unencrypted rather than building a two-tier trust story. See
  [docs/gotchas.md](docs/gotchas.md). Open until an installation-level key
  primitive exists to anchor it to.

---

## 🗺️ Working priority order

**Confirm with the maintainer before starting.** In order:

1. **Phase 0** — secret write serialization, keyset pagination.
   Release-blocking, each with a regression test. (ADMIN→OWNER escalation:
   done, see CHANGELOG.)
2. **Phase 1** — `skill.get`, `context.bootstrap`, handoff lifecycle,
   quickstart, two named MCP clients, E2E scenario, storage-backend honesty.
3. **Phase 2 onward** — only after the core loop is measured with real design
   partners.

Independent of the above, cheap, and pickable any time: `docs/README.md` index,
organization "list members", `GET .../conversations`. CI hardening unblocks
itself the day the repo goes public.
