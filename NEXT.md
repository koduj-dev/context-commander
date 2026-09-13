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

Domain breadth got ahead of user experience. The design (cookbook + 17 ADRs)
was settled and every domain it describes was built, with HTTP/MCP/CLI adapters
and a web management UI — but there was no single operation that assembled the
right starting context for a new agent, a skill could be installed but its
content never read back, and a handoff could be created but never claimed or
closed. The correction was not another subsystem: it was assembling the
existing parts into one short, safe, measurable story — **store state → hand
off work → resume exactly**.

As of 2026-09-11 that story runs (Phase 1, below). The risk it was written
against now inverts: the temptation is to keep adding to a loop nobody has
measured. Hence the priority order at the bottom — **measure before Phase 2**.

---

## 🔭 Open work

### Phases 0 and 1 — done

Both shipped; see CHANGELOG for what landed and `docs/quickstart.md` for the
result. Kept as a heading only so the phase numbering below still reads in
order — delete it once Phase 2 starts.

Phase 1's outcome in one line: **two clients complete a full cross-client
handoff**, proven end to end in `adapters/http/e2e_handoff_test.go`.
`context.bootstrap` (`internal/contextpack`) assembles the starting context
in one call, `skill.get` returns a skill's actual bytes, handoffs move
open → claimed → closed with a `current` query, `deploy/quickstart/` is a
copy-paste install, and Claude Code + Codex CLI are guaranteed by fixtures
under `deploy/mcp-clients/` that a smoke test parses and dials. S3 is now
marked experimental everywhere rather than implied to work (cookbook §8.1,
README, the package doc).

Two things Phase 1 deliberately did NOT do, so the next reader doesn't
re-derive them:

- **No web UI work.** The Handoffs tab still only shows and creates — it
  cannot claim or close, even though the endpoints now exist. That was the
  stated trade ("no web UI before the CLI/MCP core loop is proven"); wiring
  the tab to the new endpoints is now small and listed under UI below.
- **No `context.pull`.** The roadmap named "`context.bootstrap` /
  `context.pull`" but asked for *one* well-described operation. Continuation
  is `event.list` with the cursor the pack already returns, so a second
  half-specified operation would have been surface without a question to
  answer.

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

- [ ] **CRA (Regulation (EU) 2024/2847) applicability and the rest of Annex
      I.** Annex I (2)(l) — recording security-relevant internal activity
      with a user opt-out — is now implemented (`internal/security`,
      migration 000019, `--security-log`). Not yet looked at: whether the
      CRA applies to this project at all (the free-and-open-source exemption
      turns on monetization and on whether a legal entity acts as an
      "open-source steward" — a question for the maintainer, not for the
      code), a coordinated vulnerability disclosure policy beyond
      SECURITY.md, an SBOM, and the Annex VII technical documentation
      describing what is logged and how. Reporting obligations for actively
      exploited vulnerabilities applied from 2026-09-11; full application is
      2027-12-11.
- [ ] Org-level credential management, policy, audit export.
- [ ] **SSO/OIDC (and LDAP) — and the UI assumes it.** The deployment the
      maintainer describes is: a company installs this, hands out access
      authorized through OAuth/LDAP, and agents connect over MCP. What
      exists is Personal Access Tokens and nothing else — the web UI's sign
      in is "paste a PAT". The UI rework should keep its auth surface
      swappable rather than build a PAT-shaped login it will have to
      unpick, but the gap is real and it is here, not in the UI.
- [ ] **Storage quotas per user/organization.** Raised by the maintainer
      while scoping the UI: an organization deployment will want a ceiling
      on what accumulates, and nothing measures or limits anything today —
      artifacts, skill packages and history objects all grow without bound.
      Needs accounting per scope (the object store and the encrypted tables
      both), a limit, and an answer for what happens at the ceiling
      (refuse the write is the only honest one; silent eviction of a
      project's own context would be worse than the disk filling). Not
      small, and worth doing before a real organization uses it rather than
      after.
- [ ] Retention rules, legal hold, controlled purge workflow.
- [ ] Signed/trusted skill packages and an approval workflow.
- [ ] Operational telemetry that doesn't leak content.

### UI — remaining spec surface

`docs/context-commander-ui-spec.md` (2026-09-02) describes a larger target than
what exists. Phases 1–3 of it shipped (design system, Handoffs/Conversations/
Events tabs, Administration page — see `CHANGELOG.md`). Deliberately parked
behind Phase 1 above, per "no web UI before the CLI/MCP core loop is proven":

> **The web UI is being reworked end to end** (maintainer, 2026-09-13), so
> the items below are inputs to that rework rather than patches to apply to
> what exists. Wiring two buttons into a UI that is about to be replaced is
> work thrown away twice.
>
> What the rework inherits, all of it server-side and already shipped:
> handoff claim/close and a "current handoff" query; `skill.get`, a
> searchable registry (`skill.search`) and `skill.promote`; a conversations
> list; organization members and an organization audit trail; the security
> event timeline. The existing tabs predate every one of those and work
> around several of them by asking the user to paste a UUID.

- [ ] Organizations / Skill Registry top-level nav, and the remaining project
  tabs (Integrations, Settings) — not yet planned.

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

- **Project templates.** Raised by the maintainer while reviewing the UI
  mockups, and not in any earlier scope. The shape that would fit what
  already exists: a template is a **named bundle of bindings**, not a
  project copy — a set of skill bindings, a set of gate bindings, and
  optionally a few conventional artifacts under `context/`. Creating a
  project from one applies those bindings at creation; it copies no
  handoffs, no history and no content, because those belong to the work,
  not to the shape of the project.
  - Fits the grain: everything in the bundle already has a scope vocabulary
    and an install operation. A template is mostly a list of things to call
    on behalf of the creator, plus authorization — you may only template
    what you may already bind.
  - The trap to avoid: a template that *forks* an existing project's
    content. That is a copy with no provenance, and it is how two projects
    start silently diverging from a shared artifact nobody owns.
  - Not mocked. The mockups deliberately show nothing the backend cannot
    do, and a template screen would be the largest promise on the board.
- **[security] Per-IP rate limiting needs a trusted-proxy configuration.**
  `internal/ratelimit` limits per credential and bounds authentications in
  flight, which is everything that can be done correctly today. Per-IP
  limiting cannot: `serve` deliberately does not trust `X-Forwarded-For`
  (no trusted-proxy config exists, and the header is spoofable without
  one), while the supported deployment is a loopback bind behind a reverse
  proxy — so every request carries the proxy's address and a per-IP bucket
  would be one shared bucket that any caller could exhaust to lock out the
  rest. Needs `--trusted-proxies` (a CIDR allowlist) plus
  `middleware.RealIP` gated on it, and only then a per-IP limit on
  unauthenticated traffic.
- **Serving artifact content as a rendered page.** Observed while scoping
  the UI: the store is content-type aware and holds arbitrary bytes, so
  "give any LLM a tool to publish an HTML page" is already most of the way
  built — `artifact.write` with `content_type: text/html`. The missing half
  is serving it, and that half is the dangerous one: rendering
  caller-supplied HTML from the same origin as the API hands an attacker the
  session of anyone who opens it. It needs a separate origin (or a sandboxed
  iframe with a strict CSP and no credentials), which is a deployment
  decision as much as a code one. Worth doing; not worth doing casually.
- **[security] Keep RLS honest as tables are added.** Every table with a
  policy now has a test that fails when the policy is removed — `artifacts`,
  `artifact_versions`, `handoffs`, `conversations`, `secret_values`,
  `domain_events`, `audit_log`, `skill_bindings`, `gate_bindings`,
  `project_keys`, `skills`/`skill_versions`, plus `project_members`/
  `projects`/`organizations` from before. The shape that works: count the
  table through `cc_app` with a principal who should see nothing and NO
  filter of your own — a query with `WHERE project_id = $1` in it passes
  with RLS switched off and proves only the service layer.
  The standing check, worth running whenever a policy is added or changed:
  grant `cc_app` `BYPASSRLS` in `deploy/postgres/init/01-roles.sql`, run the
  suite, and see what still passes. It used to be everything but nine tests.

---

## 🎯 Definition of Done for v0.1

v0.1 is done when:

- [x] every P1 and scope-related P2 finding is fixed **and has a regression
      test** (Phase 0);
- [x] a new user completes the supported quickstart with no manual SQL
      (`docs/quickstart.md`, `deploy/quickstart/` — walked end to end on
      2026-09-11, which is how the `/data` ownership bug in the Docker image
      was found);
- [x] two separate credentials and two clients complete the reference handoff
      scenario (`adapters/http/e2e_handoff_test.go`);
- [x] the second client reads the actual content of artifacts *and* effective
      skills (`context.bootstrap`, `skill.get`);
- [x] every growing list endpoint has a limit and a cursor (Phase 0);
- [x] PATs can be listed and revoked, and revocation takes effect immediately
      (Phase 0; the E2E scenario asserts the revoked token stops working
      while the other client's keeps working);
- [x] a backup/restore procedure for database and storage is documented for
      at least the reference deployment (`docs/quickstart.md`);
- [x] the docs don't describe an inactive storage backend or a future feature
      as done (S3 marked experimental in cookbook §8.1, README, and the
      package doc).

**Every box is ticked as of 2026-09-11.** What that does *not* mean: it was
written as a checklist of capabilities, and capabilities are not adoption.
Nothing here has been used by anyone but its author. Before calling v0.1
released, take the measurements below against a real repository — the
checklist says the loop exists, the metrics say whether it works.

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

- ~~**First guaranteed client pair.**~~ Decided 2026-09-11: **Claude Code and
  Codex CLI**. Both reach the server over Streamable HTTP with a bearer PAT,
  so one transport covers both, and both configure from a small committed
  file a test can parse — see `deploy/mcp-clients/`.
- **Handoff vs. context pack as the headline object.** Recommendation: sell the
  outcome ("handoff"), keep the context pack as the mechanism. (Raised
  2026-09-01.)
- ~~**Filesystem vs. S3 in v0.1.**~~ Decided 2026-09-11 as recommended: the
  filesystem backend is the supported one and the quickstart ships no
  object-storage container; S3 is marked experimental in cookbook §8.1, the
  README, and `internal/storage/s3`'s package doc. Reopens the day someone
  wires it into `serve` — with an integration test against a real endpoint,
  which is the part that does not exist today.
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

1. ~~**Phase 0**~~ — done. ADMIN→OWNER escalation, the secret and project-key
   write races, and unified keyset pagination all shipped with regression
   tests (see CHANGELOG).
2. ~~**Phase 1**~~ — done 2026-09-11. `skill.get`, `context.bootstrap`,
   handoff lifecycle, quickstart, two named MCP clients, E2E scenario,
   storage-backend honesty.
3. **Measure before building Phase 2.** The core loop now exists; nothing in
   Phase 2 should be started before the metrics below have been taken against
   a real repository with real design partners. Every missing datum is first
   answered with a better context pack, not a new subsystem — and the pack is
   now the thing to tune (what it selects, what it costs, what it truncates),
   which is exactly what the Phase 2 list is mostly about.
4. **Phase 3 onward** — only on top of a core loop people actually use.

Independent of the above, cheap, and pickable any time: `docs/README.md` index,
organization "list members", `GET .../conversations`, the two UI items above,
and the member-audit backlog entry. CI hardening unblocks itself the day the
repo goes public.
