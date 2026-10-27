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

- **No web UI work** — the stated trade ("no web UI before the CLI/MCP core
  loop is proven"). Superseded on 2026-09-13 by the green-field rebuild
  described under "UI" below, which is now built.
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
- [x] ~~Diagnostics: "why does this context pack contain exactly these
      items?"~~ Closed 2026-09-14: the interface's Context tab renders the
      assembled pack with its budget report — what was left out, how big it
      was and why — and can reassemble it under a different limit. The
      server half (`BudgetReport`/`TruncationNote`) shipped with
      `context.bootstrap`; this is the surface that makes it readable.
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
- [ ] **Directory sign-in: LDAP and OIDC.** The deployment the maintainer
      describes is: a company installs this, hands out access authorized
      through OAuth/LDAP, and agents connect over MCP.
      [ADR 0019](docs/adr/0019-browser-authentication-and-directory-identity.md)
      decides the whole shape and the local half is built — argon2id
      passwords, browser sessions as `client_credentials` rows, first-run
      setup through the browser, and `GET /v1/auth/methods` so the sign-in
      screen renders what the server actually offers. What remains is the
      two directory backends, and they are deliberately the same shape:
      authenticate, resolve `user_identities(provider, subject)`, provision
      on first success, then issue a session exactly as a password login
      does. Everything after the identity is established is already shared
      code.
      - [x] ~~LDAP~~ Closed 2026-10-06: `internal/directory` (`--ldap-url` and
        the flags beside it), provisioning in
        `identity.ProvisionDirectoryUser`, and `GET /v1/auth/methods`
        reporting it so the sign-in screen asks for an email **or** a
        username. Tested against a real slapd (`deploy/ldap/`, brought up by
        `--profile ldap` or by the package's own container).
      - [x] ~~OIDC~~ Closed 2026-10-27: authorization code + PKCE with
        discovery (`internal/directory/oidc.go`, `--oidc-issuer` and the flags
        beside it), tested against a real Keycloak. No SAML — a second
        protocol whose security-critical core is XML signature verification,
        for deployments this product does not have yet.
      - [ ] **Entra ID cannot sign in yet.** It does not send
        `email_verified`, and an unverified address is refused because
        provisioning links to existing accounts by email. Trusting Entra's
        addresses (it verifies them for its own tenant users, not for guests)
        is a per-provider decision, and the switch for it should name the
        provider rather than turn the check off for all of them.
      - [ ] One OIDC provider per installation. `Deps.OIDC` and the sign-in
        screen already take a list; only the flags configure one. **Before a
        second one is allowed**, the callback must defend against provider
        mix-up: today the provider comes only from the cookie and the
        RFC 9207 `iss` response parameter is not checked (review, 2026-10-27).
      - [x] ~~Who may sign up through OIDC~~ Decided 2026-10-27
        ([ADR 0020](docs/adr/0020-oidc-sign-up-is-off-by-default.md)): nobody,
        unless `--oidc-allow-signup`; `--oidc-allowed-domains` limits sign-in
        to verified addresses in the listed domains.
      - Remaining review findings (2026-10-27), all low, none exploitable as
        configured today:
        - The `cc_oidc` cookie is unsigned, so a sibling subdomain able to
          plant cookies can pair a victim's browser with the attacker's
          attempt (login CSRF). An HMAC over it with a server-held key closes
          that; the code comment states the gap.
        - A key-fetch failure during verification (provider down right after
          a rotation) is reported as a rejected sign-in and recorded as one,
          rather than as unavailable — go-oidc does not type that error.
        - Identities are keyed on the operator's `--oidc-name`, not the
          issuer: changing the issuer but keeping the name lets the new
          issuer's subjects resolve to the old one's links. Storing `iss`
          beside the link would remove it; it needs a migration.
        - A second `/start` (another tab, or a cross-site navigation) replaces
          the first attempt, whose callback then reads as expired. Nuisance.
        - A callback in a browser already signed in starts a second session
          without ending the first, as password sign-in does.
        - A mismatched state writes a security event with no limit of its
          own — the same class as failed password sign-ins, and the per-IP
          limit that would bound both is the backlog's trusted-proxy item.
      - Group-to-role mapping is a separate decision. A directory proves who
        someone is, not what they may do; authorization stays in project and
        organization membership and `installation_admins`.
      - [x] ~~An administrator setting an initial password for a user they
        create.~~ Closed 2026-10-09: optional `password` on
        `POST /v1/installation/users`, set in the same transaction as the user
        and its principal, plus `PUT /v1/installation/users/{id}/password` for
        an account that already exists. Both need `CapCredentialManage` on top
        of creating — setting someone's password is the power to sign in as
        them, which is what issuing them a token already was.
      - [ ] No "must change at next sign-in" flag. It needs a column, a check
        on every authenticated request and a screen of its own, and half of it
        is worse than none: a flag nothing enforces reads as a promise the
        product does not keep.
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

`docs/context-commander-ui-spec.md` (2026-09-02) describes a target that
predates the rebuild; where it and the built interface disagree, the built
one is what exists. It was deliberately parked behind Phase 1 above, per "no
web UI before the CLI/MCP core loop is proven":

> **The web UI is a green-field rebuild** (maintainer, 2026-09-13). Not a
> rework of what exists — the existing `web/` is discarded whole and the new
> one starts from nothing: nothing adapted, nothing migrated, nothing
> rewritten in place. Two things are settled before the first commit: the
> component kit and screens in `docs/mockups/` are the design, and
> **internationalisation is built in from that first commit** (English,
> German, Spanish, and ready for one nobody has picked yet) — retrofitting it
> later is a second rewrite.
>
> The items below are inputs to that rebuild, not patches to apply to what
> exists — wiring two buttons into a UI that is about to be deleted is work
> thrown away twice.
>
> What the rework inherits, all of it server-side and already shipped:
> handoff claim/close and a "current handoff" query; `skill.get`, a
> searchable registry (`skill.search`) and `skill.promote`; a conversations
> list; organization members and an organization audit trail; the security
> event timeline. The existing tabs predate every one of those and work
> around several of them by asking the user to paste a UUID.

**The rebuild is done** (2026-09-13). `web/` was deleted and rebuilt from
nothing: React 19 + Vite, no CSS framework, no i18n library, the component kit
ported from `docs/mockups/assets/mockup.css` and kept in step by
`web/scripts/check-design-sync.mjs`. English is the source language; German
and Spanish are complete and `npm run i18n:check` fails on a dropped
placeholder, an orphaned key or a plural with no `other` form. Per-reader
localisation (date order, 12/24 h, time zone, UTC alongside, GB vs GiB) is in
from the first commit.

What it covers: sign-in, dashboard ("is anything waiting on you", assembled
from `current` per project), projects with creation, and per project —
context pack with its budget report, artifacts with a versioned editor,
skills with install/override/pin, policies with gate bind/unbind, secrets,
members with add, conversations with lineage, activity (audit and events),
settings. Plus the skill registry with publish and per-skill detail,
organizations with creation and membership, administration (admins, identity
creation, credential lookup, security log) and personal access tokens.

What it deliberately does not offer, because the API has no write for it:
removing a project member or changing a role, renaming or deleting a project,
changing visibility, revoking installation admin. Each of those says so on
the screen rather than showing a control that would fail.

- [x] ~~No user or service-principal directory.~~ Closed 2026-09-14:
  `GET /installation/users` and `/installation/service-principals` list them,
  admin-gated and paginated, each row carrying both ids — the user id an
  organization membership names and the principal id a project membership
  names. Organization member lists join the user row, so they return a person
  rather than a UUID. MCP has the same two reads plus `me`, `project.list`,
  `organization.list` and `organization.list_members`.
- [x] ~~No `GET /skills/{id}`.~~ Closed 2026-09-24: `skills.Service.Get`,
  `GET /v1/skills/{skillID}` and MCP `skill.info`. Every reader holding a
  skill id had to page the catalogue and match, which is a scan standing in
  for a lookup and returns nothing once the match falls past the requested
  page.
- [x] ~~No cross-project reads.~~ Server half closed 2026-09-24:
  `handoffs.Service.ListWaiting`, `GET /v1/me/handoffs` and MCP
  `handoff.waiting` answer "is anything waiting on me" in one request, one
  row per project, each carrying the project path and the summary. RLS scopes
  the rows and `RoleCan(role, CapHandoffRead)` filters them — today every
  project role holds that capability, which is exactly why the filter is
  written down rather than left implied.
  - [x] ~~The dashboard still asks per project.~~ Closed 2026-10-09: it makes
    one request and joins the project names against `/me/projects`, which it
    already loads. A DOM test counts the requests, because the screen looks
    identical either way and nothing else would notice a loop creeping back.
- [ ] **Agent ownership that means something.** `service_principals` now
  records `created_by` and the directory shows it (migration 000026), but
  that is provenance: an immutable note of who registered the agent, granting
  it nothing. Raised by the maintainer 2026-10-06 as "agents could have an
  owner listed somewhere, because one has to be created with some
  authorization from a user", which the column answers. What it does not
  answer, and what needs a decision before it needs a column:
  - **Transfer.** When the person who registered an agent leaves, the record
    becomes a fact about history and nobody is accountable for the agent. An
    owner that can be reassigned is a different column with a different name.
  - **Whether ownership may confer access.** Today an agent is its own
    principal and reaches a project only through its own membership
    (ADR 0004). Letting it inherit an owner's rights would make every agent
    as powerful as whoever created it, and would make revoking a person's
    access insufficient to revoke their agents'. If that is ever wanted it is
    an ADR, not a migration.
  - **What an admin actually asks**, which is neither: "what can this agent
    reach?" That is its project memberships, and nothing lists them for a
    principal other than the caller — `/me/projects` is self-only.
    **Decided by the maintainer 2026-10-27: an installation admin does not
    get this.** The admin tier stays as blind to users' data as it can be —
    the boundary migration 000024 draws, counts and never paths. If "what can
    this agent reach" is ever answered, it is answered for whoever is
    accountable for the agent, not for the admin tier.

- [x] ~~**Service principal names are not unique.**~~ Closed 2026-10-27:
  unique per registrar (`created_by`), case-insensitively — the scope the
  maintainer chose. Unattributed agents fall outside the rule; existing
  duplicates were renamed with an id suffix (migration 000027).
- [ ] Project templates — agreed in the mockups (`templates.html`), no
  schema and no endpoint.

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
- [x] ~~`CODEOWNERS`~~ Added 2026-10-27 (`.github/CODEOWNERS`, everything to
  the sole maintainer). It only assigns review until branch protection can
  require it.

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

- **Project templates.** Decided with the maintainer 2026-09-13: a template
  is a **snapshot of a project** — its skill bindings, its gate bindings and
  its `context/` notes — captured by a button on an existing project, and
  applied when a new project is created from it. Deliberately not a
  template *editor*: you shape a real project until it is right, then save
  what it became.
  - Two operations, no third: **save this project as a template**, and
    **create a project from a template**. No forking a project into another
    project, and no separate authoring surface to maintain.
  - What a template must carry: the bindings (which already have a scope
    vocabulary and an install operation, so applying one is mostly a list of
    calls made on the creator's behalf, with the same authorization — you
    may only template what you may already bind) and a copy of the
    `context/` artifacts. What it must NOT carry: handoffs, history,
    conversations, secrets or events. Those belong to the work that
    happened, not to the shape of a project.
  - **The copy needs provenance.** An artifact that arrives in a new project
    with no record of where it came from is how two projects start quietly
    diverging from the same text with nobody owning either. Record the
    template and its version on what it creates.
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
- **The UI has to speak more than English.** Required by the maintainer
  2026-09-13: at minimum English, German and Spanish, built so an unknown
  language can be added later without reworking screens — Czech being the
  obvious near-term one.
  - Externalise every string from the first commit of the rework. Retrofitting
    i18n is a rewrite; doing it from the start is a discipline.
  - Format dates, times and numbers through the locale, never by
    concatenation. "12 Sep 2026" is an English sentence, not a date.
  - **German is roughly 30% longer than English, and this design is dense.**
    That is the real cost: fixed-width labels, single-line badges and tight
    table headers are exactly what breaks first. The mockup kit has been
    hardened against it (nothing is pinned to a width that only fits
    English), and any new component must pass the same bar — see
    `docs/mockups/foundations.html`.
  - Keep the door open for scripts that are not Latin and for right-to-left,
    without building for them now: no text baked into images, no layout that
    depends on a string's length, no icon carrying meaning alone.
- **Per-user localization, not just translation.** A language is only half of
  it. The other half is how a person wants values written, and it belongs to
  the account rather than the browser, so it follows them between machines and
  clients:
  - date format (ISO, DD/MM, MM/DD, or follow the language), 12/24-hour
    clock, **time zone**, and whether audit and security screens also show
    UTC — correlating an incident with someone in another zone is exactly
    when a local-only timestamp costs an hour;
  - number format (`1,284.50` / `1 284,50` / `1.284,50`) and **storage
    units**: `2.40 GB` and `2.24 GiB` are the same 2,400,000,000 bytes, and
    which one a person reads is their call, not the product's.
  - Needs a home in the schema — a preferences row per user — and an endpoint
    to read and write it. Nothing like it exists today.
  - **Localization is a UI concern and stops at the UI.** What leaves over
    HTTP and MCP stays in one standard machine format — RFC3339 timestamps,
    integer byte counts, canonical enum values — because its reader is a
    model, not a person, and it does not need to be pretty. A server that
    returned `"2.4 GB"` or `"13 Sep 2026"` would have made a presentation
    choice on the user's behalf, in a language it guessed and a timezone it
    does not know, and would have made the payload harder for a model to
    parse at the same time. The current API already does this; it must keep
    doing it, and no localization work may leak into it.
  - Mocked in `docs/mockups/preferences.html`.
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

What the server can now measure by itself (2026-10-27, administration →
Usage, `GET /v1/installation/trends`): time from project start to the first
takeover by another client, time to claim, weekly active people and agents.
What it cannot: resume success, manual context transfers and context
efficiency — those still need the experiment below.

- [ ] `principal_activity_days` grows by one row per active principal per day
  and nothing prunes it. The screen reads at most 52 weeks; a retention bound
  (and who runs it — there is no scheduler) belongs with the purge workflow
  above.

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
- ~~**Who may become a user by signing in through OIDC?**~~ Decided
  2026-10-27 by the maintainer: sign-up off by default, domains restrictable
  — [ADR 0020](docs/adr/0020-oidc-sign-up-is-off-by-default.md).
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

Independent of the above and pickable any time, but none of it is free of a
decision any more (the cheap ones — the `docs/README.md` index, organization
member lists, `GET .../conversations` — shipped): the two agent items under
"UI" above (an admin will not see an agent's projects — decided; unique
names — done) and project templates. CI hardening unblocks itself the day the
repo goes public.
