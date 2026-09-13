# Changelog

All notable changes to Context Commander are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project uses
[semantic versioning](https://semver.org/).

## [Unreleased]

Nothing released yet — accumulate entries here as work lands. At the first
release, this section is renamed `## [x.y.z] — YYYY-MM-DD` with a matching
link definition at the bottom of the file.

### Added

- **The cheap gaps from the backlog, cleared in one pass.**
  - **Membership changes are audited.** Neither `members.Service.Add` nor
    `organizations.Service.AddMember` wrote an audit row, though granting a
    role is exactly the privilege change `internal/audit` exists to record
    and "trace the origin of a change without SQL" is a stated v0.1 metric.
    The project half was the one-liner the backlog promised. The
    organization half was not: `audit_log` was project-scoped and its RLS
    policy has no separate `WITH CHECK`, so Postgres reused `USING` as the
    insert check and a row with no project could not be written at all.
    Migration 000023 gives `audit_log` an `organization_id`, a CHECK that
    exactly one scope is set, and a policy that grants access through
    either — organizations are a peer scope to projects (docs/adr/0006),
    not something nested below them.
  - **`GET /v1/projects/{ns}/{slug}/conversations`**, plus MCP
    `history.list_conversations` and `ccd history list-conversations`.
    `internal/history` could create a conversation and operate on one whose
    id you already had, and nothing could enumerate them — so the web UI's
    Conversations tab told people to paste an id, and a project's existing
    conversations were undiscoverable.
  - **`GET /v1/organizations/{id}/audit`** — the organization's own trail,
    now that there is something in it. Gated on a new `CapOrgAuditRead`
    which `RoleMember` does **not** hold: seeing who is in an organization
    and seeing who put them there are different questions, and the second is
    the more sensitive one. That is the same line `auth.CapAuditRead`
    already draws for projects, where it is withheld from
    CONTRIBUTOR/READER. On a one-person installation the distinction costs
    nothing — the only member is an OWNER.
  - **`GET /v1/organizations/{id}/members`** —
    `organizations.Store.ListMembers`/`Service.ListMembers`, the counterpart
    projects had and organizations did not. Gated on `CapOrgRead`, which
    every organization role holds, mirroring how a project member may see
    the project's member list.
  - **RLS coverage for every table that has a policy.** `domain_events`,
    `audit_log`, `skill_bindings`, `gate_bindings` and `project_keys` all
    gained a test that counts the table through `cc_app` with a principal
    who should see nothing and no filter of its own — the only shape that can
    observe a policy. Each was verified to fail with `cc_app` granted
    `BYPASSRLS`, which is the standing way to check: before this work that
    switch left all but nine tests in the whole suite passing.
    `project_keys` went to `internal/auth` rather than `internal/crypto`,
    following the latter's own note that its tests are about crypto
    correctness and access is covered there.
  - **[`docs/README.md`](docs/README.md)** — an index of what each document
    is for, and a table of every ADR and what it decided.
- **`ccd sync-skills`** — the client-side half docs/adr/0012 reserved and
  `cmd/ccd/main.go` had a comment promising: it resolves a project's
  effective skills and writes each one as `<dir>/<skill>/SKILL.md` (default
  `.claude/skills`). An MCP client gets skill content through the tools; a
  plain CLI user had no way to land it on disk at all.
  - **Incremental by construction.** A `SkillVersion` is immutable, so the
    version id a binding points at completely answers "do I already have
    this". A manifest written into the skill directory records it, and a
    sync downloads only the skills whose version actually moved — a second
    run over unchanged skills fetches nothing. `--dry-run` prints the plan;
    `--prune` (opt-in, because it deletes) removes skills this command
    wrote that the project no longer binds, and only those.
  - Skill names come from a registry other people publish into and this
    command writes to the caller's filesystem, so the directory name is a
    security boundary: anything outside a conservative allowlist collapses
    to `-`, a name that sanitizes to empty/`.`/`..` becomes the skill id,
    and a sanitized name carries an id suffix so two skills cannot collide
    on one directory. The prune path independently refuses any manifest
    entry that is not a plain child of the target directory. Unit-tested
    against `../../.ssh/authorized_keys` and friends.
- **The skill registry is browsable, and a package now lives at a scope**
  (`docs/adr/0018`, migration 000021). ADR 0012 gave the *binding* a scope
  and the package none, which left two holes at once: nothing said who may
  see a published skill, and there was no way to *find* one — a package was
  reachable only through a UUID that came from somewhere else, so an agent
  could not look for a skill it did not already know about.
  - `Skill` carries `scope`/`scope_ref` with the same shape and constraints
    as `skill_bindings`, and `skill.promote` widens it: project →
    organization → installation. Project scope is the default for a publish
    that does not say otherwise, so publishing is never accidentally a
    public act. The
    installation scope is the marketplace shelf, and putting something on it
    is an authorized act (installation admin) rather than a side effect of
    publishing. Promotion only ever widens — narrowing would break bindings
    made while the skill was visible. Authorized by the same
    `checkScopeTarget` that decides whether a caller may bind *into* a
    scope, because it is the same question.
  - **Publishing names its target scope and needs no project.** A team
    self-hosting this can put an instance-wide rule on the shelf directly
    rather than inventing a project to hang it on — an installation admin
    who belongs to no project is exactly the caller that case has
    (docs/adr/0017). Authorization is always the destination, never the
    caller's standing somewhere else. Reading the registry names no project
    either: an earlier draft demanded `CapSkillRead` on an "anchor" project,
    which every project role holds, so it only proved the caller belonged to
    *some* project — not a fact the registry cares about, and one that
    excluded the very admin a self-hosted instance wants curating its shelf.
  - The REST surface now splits where the concepts do: the registry is
    top-level (`/v1/skills` — publish, search, promote, versions, content)
    and *bindings* stay project-scoped
    (`/v1/projects/{ns}/{slug}/skills/install|override|pin|effective`). A
    package that can live above any project has no business behind a project
    path.
  - **Discovery:** `skill.search` browses and searches by name
    (case-insensitive, LIKE metacharacters escaped), `skill.versions`
    enumerates a skill's versions. Both return exactly what the caller can
    see, across every scope, and neither names a project. Also
    `GET /v1/skills?q=`, `GET /v1/skills/{id}/versions`,
    `POST /v1/skills/promote`, and `ccd skill search|versions|promote`.
  - **Change detection is the checksum**, already carried by every
    `SkillVersion` and computed over the plaintext package. A catalog entry
    reports its newest version's checksum, so a client remembers what it
    loaded and compares, instead of trusting a version number or a timestamp
    to decide whether to re-download. The `skill.search` tool description
    tells an agent to do exactly that.
  - Visibility is enforced by RLS on `skills`, with `skill_versions`
    inheriting through its parent — the identical policy `skill_bindings`
    has had since migration 000007. A deliberate exception to "authorize in
    the domain service" (CONTRIBUTING rule 7), because restating the
    predicate in Go would need to resolve organization membership, which is
    keyed by user and not by the principal a read path carries. Verified
    load-bearing: granting `cc_app` `BYPASSRLS` fails both the
    cross-project read test and the search-visibility test.
  - This replaces the narrowing shipped as a stopgap alongside migration
    000020 ("a binding you can see, or the project you published from"),
    which deferred the product question. The provenance that migration
    recorded stays, as trust metadata.
- **Refusals are now recorded, and callers are rate-limited.** Nothing in
  the server recorded a denial before this: `audit.ResultDeny` was a
  defined constant with **zero call sites**, and the server emitted no logs
  at all, so a failed authentication or a denied capability left no trace
  anywhere. Driven by two independent needs that want the same data —
  reconstructing an incident afterwards, and CRA Annex I (2)(l)'s
  requirement to record security-relevant internal activity with a user
  opt-out.
  - **`security_events` (migration 000019)** — append-only (no UPDATE/DELETE
    grant), recording `authentication_failed`, `authorization_denied` and
    `rate_limited`. Deliberately **not** `audit_log`: that table's RLS
    policy has no separate `WITH CHECK`, so Postgres reuses `USING` as the
    insert check — meaning a row saying "this principal has no access to
    project X" cannot be inserted by that principal, which is exactly the
    row worth keeping. An authentication failure has no principal or
    project at all and no home in a per-project table. Also deliberately
    **without foreign keys** on the recorded ids: a credential id that
    names nothing is the most interesting row here, and an FK would reject
    it.
  - `internal/security.Recorder` writes best-effort in its own transaction
    (a denial produced inside a transaction about to roll back must not
    take its own record with it) and never changes the caller's outcome.
    `internal/security.Service` reads, gated on installation-admin rights
    (docs/adr/0017) — a refusal timeline spans projects the caller cannot
    see, and one credential probing five projects is a pattern no
    per-project view can show. Served at `GET
    /v1/installation/security-events`.
  - Denials are recorded from `auth.AuthzService.Can`, the single place that
    decides ALLOW/DENY for project content (docs/adr/0006) — one hook
    instead of nine `checkCapability` implementations to forget it in. Wired
    through a new optional `auth.DenyRecorder` hook rather than a
    constructor parameter, because `internal/audit` and `internal/security`
    both import `internal/auth`, so the dependency had to be inverted; a nil
    recorder keeps every existing caller and test unchanged.
  - **Structured logging** (`log/slog`, JSON by default, `--log-format=text`
    for a human). The server previously logged nothing whatsoever — the
    `/data` permission bug earlier in this changelog surfaced as a bare 500
    with no diagnostic anywhere.
  - **CRA opt-out:** `--security-log=false` disables recording entirely,
    with a startup warning stating what it costs. All-or-nothing on purpose:
    a half-recorded security timeline reads as complete.
  - **`internal/ratelimit`** — a token bucket per credential (default 100
    rps, burst 200) plus a ceiling on authentications in flight (default
    64). Per credential, **not per IP**: `serve` refuses to trust
    `X-Forwarded-For` and the supported deployment is a loopback bind behind
    a reverse proxy, so a per-IP bucket would put every caller in one shared
    bucket where any one of them could lock out the rest. Per-IP limiting
    needs a trusted-proxy configuration that does not exist yet, and is
    logged in NEXT.md rather than faked. The concurrency ceiling is the part
    that bounds unauthenticated work: `cc_<any valid uuid>_<anything>`
    reaches the credential lookup — a database round trip — before being
    rejected. Both transports share one limiter, so an agent cannot double
    its allowance by switching between REST and MCP. Refusals are 429 with
    `Retry-After` (per credential) and 503 (gate saturated).
- **Phase 1 — the first usable cross-client handoff.** Two agents on two
  different clients can now complete a full handoff without copy-paste and
  without a shared credential. Everything below is one story; the pieces
  were individually buildable before and individually useless.
  - **`context.bootstrap` — one call to a usable starting state.** New
    `internal/contextpack` assembles an aggregated read model over the
    existing domains: project identity, the caller's role *and the
    capabilities it implies*, the handoff to continue from (decrypted), the
    **content** of the project's effective skills, selected artifacts, and
    the tail of the event stream with a cursor to follow along from. It
    owns no storage and no new source of truth — every field is fetched
    through the same domain service (and therefore the same capability
    check, docs/adr/0014) an adapter would call one at a time. Content is
    bounded by an explicit byte budget (256 KiB default, 8 MiB max) and
    whatever did not fit is reported in `budget.truncated` naming the tool
    that fetches it: an agent that cannot distinguish "there is nothing
    else" from "there was more and you didn't get it" acts confidently on a
    partial picture. Items are omitted whole, never cut — half an artifact
    reads exactly like a complete one. A section the caller isn't
    authorized for degrades to a truncation note rather than failing the
    pack. Artifact selection is the handoff's own referenced artifacts plus
    the `context/` logical-path prefix (the "convention" half of the
    manifest that arrives in Phase 2), or an explicit list from the caller.
    Exposed as MCP `context.bootstrap` (which the server's MCP instructions
    now name as the first call to make), `GET
    /v1/projects/{ns}/{slug}/context`, and `ccd context`.
  - **Handoff lifecycle: open → claimed → closed.** `internal/handoffs`
    hardcoded `'open'` on create and had no transition anywhere, which is
    why the web UI's Handoffs tab could only show and create. Migration
    000018 widens the status vocabulary and adds claim/close attribution
    (principal *and* client — the whole point of the domain is that agent A
    and agent B are different clients). New `Service.Claim`/`Close`/
    `Current`: the status precondition lives in the UPDATE's `WHERE`
    clause, so two clients racing to continue the same work serialize on
    the row lock and exactly one wins, the loser getting `ErrAlreadyClaimed`
    (HTTP 409 `already_claimed`) — the race is the normal case in a
    cross-client handoff, not the exotic one. `Current` answers "which
    handoff am I supposed to read?", which a list of UUIDs cannot. Both
    transitions write an audit row and an outbox event in the same
    transaction as the mutation. New: MCP `handoff.current`/`claim`/
    `close`, `GET .../handoffs/current`, `POST .../handoffs/{id}/claim`
    and `/close`, `ccd handoff current|claim|close`.
  - **`skill.get` — read a skill's content.** `internal/skills` exposed
    publish/install/override/pin/list-effective and nothing that returned
    the bytes, so an agent could install a skill it was then unable to
    load. `Service.GetVersion` returns the version's metadata, the Skill
    identity it belongs to, and the packaged content, verifying on every
    read that the bytes still hash to the checksum recorded at publish
    time: a SkillVersion row is immutable and its object write-once, but
    the object store is the half Postgres cannot protect, and handing an
    agent instructions that don't match what was reviewed and pinned is the
    exact supply-chain failure docs/adr/0012 exists to prevent (HTTP 409
    `checksum_mismatch`, distinct from "not found"). Content is now capped
    at 10 MB on both publish and read — the CLI publishes straight from a
    file with no HTTP body limit in the way, so the cap belongs at the
    domain chokepoint. New: MCP `skill.get`, `GET
    .../skills/versions/{id}`, `ccd skill get` (metadata to stderr, content
    to stdout, so `> skill.md` writes the package and nothing else).
  - **A supported Docker Compose quickstart** (`deploy/quickstart/`,
    [`docs/quickstart.md`](docs/quickstart.md)): Postgres, migrations as
    their own service (they run as `cc_owner`, which the server must never
    hold — docs/adr/0002), and the server, so a plain `docker compose up -d`
    yields a migrated database and a running server. Deliberately separate
    from `deploy/docker-compose.yml`, which is the *development*
    environment and stays unchanged. Covers bootstrap, the handoff loop,
    per-agent credentials, and a backup/restore procedure for both stateful
    halves.
  - **Two named MCP clients, not "works with any MCP":** Claude Code and
    Codex CLI, with their configurations committed in
    `deploy/mcp-clients/` and an executable smoke test
    (`adapters/mcp/clients_test.go`) that *parses the committed fixtures*,
    derives the endpoint and Authorization header each client would send,
    and runs the real handshake plus a `context.bootstrap` call with them.
    A test with its own copy of the config would keep passing while the
    file a user actually copies rots.
  - **E2E reference scenario** (`adapters/http/e2e_handoff_test.go`):
    client A writes artifacts, publishes and installs a skill and creates a
    handoff; client B — a different principal with its own credential,
    having seen none of A's conversation — bootstraps, receives the actual
    *content* of A's artifacts and skills, claims the handoff, finishes and
    closes it; a second claim is refused; A's token is revoked and stops
    working immediately while B's keeps working.
- **`auth.CapabilitiesFor`** enumerates a role's capabilities (sorted,
  stable). A description of an existing grant, never a decision — nothing
  authorizes anything by consulting it instead of `Can`/`RoleCan`.
- **Administration page (web UI):** `GET /v1/me` gained
  `is_installation_admin` (a second, separate transaction from the rest of
  `getMe` — nesting it in the first would risk pool exhaustion, same
  reasoning as `internal/skills`/`internal/gates`' `checkScopeTarget` —
  backed by a new, ungated `installation.Service.IsAdmin`: this is the
  caller's own status, not access to someone else's data, same reasoning
  `organizations.Service.ListMine` already uses). It's the web UI's only
  signal for whether to show the "Administration" nav entry/route at all.
  `web/src/pages/Administration.tsx`: a read-only installation-admins
  list (no grant/revoke button — that stays CLI-only, docs/adr/0017), a
  create-user form, a register-service-principal form, and a "load a
  principal's tokens" section (issue/list/revoke, `ConfirmDialog` on
  revoke, same shape as `AccountTokens.tsx`'s self-service page).
- **Installation-admin authorization tier (docs/adr/0017):** a new
  `internal/installation` package closes the last unbuilt piece of the
  identity model — `installation_admins` (migration 000017), keyed by
  `user_id` (a ServicePrincipal can never hold installation-admin rights),
  with its own `Capability`/`RoleCan`, mirroring `internal/organizations`'
  authorization shape (docs/adr/0006). `ccd bootstrap`'s first user is now
  automatically granted installation-admin rights — otherwise nobody could
  ever become one without direct database access. Every later grant is
  `ccd installation grant-admin --email <email>`, a `--database-url`-only
  operator command with no HTTP/MCP equivalent: no installation admin can
  mint another over the network, a deliberate scope decision confirmed
  with the maintainer before implementation, not an oversight.
  - New HTTP endpoints (all installation-admin-gated): `POST
    /v1/installation/users` (admin-driven user creation),
    `POST /v1/installation/service-principals` (agent registration —
    `identity.CreateServicePrincipal`/`CreatePrincipalForService` had zero
    callers before this), `POST`/`GET`/`DELETE
    /v1/installation/principals/{id}/tokens` and `/v1/installation/tokens/{id}`
    (cross-principal credential management, deliberately separate from
    `/v1/me/tokens`'s self-only endpoints — `internal/identity` gained
    `RevokeCredentialByID`, with no ownership check, for this), and
    `GET /v1/installation/admins`. Matching `ccd installation ...` CLI
    subcommands and `installation.*` MCP tools.
  - `internal/skills` and `internal/gates` installation-scope bindings now
    go through a real capability check (`installation.Store.IsAdmin`)
    instead of the old hard-coded `ErrUnsupportedScope` rejection
    ("operator-seeded only") — `skills.NewService`/`gates.NewAdminService`
    both gained a required `*installation.Store` parameter. A non-admin
    gets the ordinary `ErrForbidden` every other denied scope check
    already returns; a real installation admin can now actually create an
    installation-scope skill/gate binding through the API, not just via
    direct-database seeding.
  - Backend only — no web UI yet (`NEXT.md`'s Phase 3 UI is still open).
- **Web UI phase 2 — Handoffs/Conversations/Events project tabs:** wired
  three domains that already had working HTTP/MCP/CLI adapters but no UI
  (`internal/handoffs`, `internal/history`, `internal/events`) into three
  new `ProjectDetail` tabs, built on phase 1's component library.
  - **Handoffs:** list (with a status filter) + detail view, and a create
    form for CONTRIBUTOR+ (`CapHandoffWrite`). The backend never
    transitions a handoff out of `"open"` — there's no claim/close
    endpoint anywhere — so the UI only ever shows/creates, matching what
    the API actually supports.
  - **Conversations:** create-or-load-by-id (there is no "list
    conversations" endpoint, same "no browse endpoint yet" shape already
    used by Skills' install form — see `NEXT.md`), then create external
    sessions, append history objects (content is encrypted server-side;
    the response's base64 content is decoded client-side for preview),
    and view the lineage graph as a flat edge list.
  - **Events:** read-only outbox feed using `after_seq`/ascending
    pagination — the opposite cursor direction from Audit's
    `before_id`/descending keyset pagination, so it's a separate "Load
    more" implementation, not a reused hook.
  - `web/src/lib/api.ts` gained the `Handoff`/`HandoffDetail`/
    `Conversation`/`ExternalSession`/`HistoryObject`/`LineageEdge`/
    `ProjectEvent` types and matching client methods; `web/src/lib/roles.ts`
    gained `canWriteHandoffs`/`canWriteHistory` (CONTRIBUTOR+, mirroring
    `internal/auth`'s `CapHandoffWrite`/`CapHistoryWrite` grants).
- **Web UI design system (phase 1 of `docs/context-commander-ui-spec.md`):**
  brand tokens (`docs/brand/tokens/*`) wired into Tailwind as `cc-*` utility
  classes, live System/Light/Dark theme switching (`web/src/theme/`,
  persisted to `localStorage`), the real brand logo/favicon (`web/public/brand/`)
  replacing the plain-text header, and a shared component library
  (`web/src/components/`: `PageHeader`, `ProjectContextHeader`, `DataTable`,
  `StatusBadge`, `ScopeBadge`, `PrincipalBadge`, `EmptyState`,
  `ConfirmDialog`) retrofitted onto all 11 existing pages plus `Shell.tsx`.
  Destructive actions (delete secret, revoke token, unbind gate) now go
  through a confirm dialog instead of firing immediately. No backend
  changes, no new pages/routes — see
  `docs/plans/2026-09-02-design-system-phase-1.md`.
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

### Changed

- **The integration suite runs in ~20 seconds instead of minutes.**
  `internal/dbtest.New` started a whole Postgres container per test —
  ~2.4 s of image boot plus a full 18-migration run, 209 times across the
  repo, which was roughly eight minutes of the suite spent watching
  Postgres start while the tests themselves took ~0.5 s each. It now starts
  one container per test binary, migrates a template database once, and
  clones it per test (`CREATE DATABASE ... TEMPLATE`, dropped on cleanup),
  which costs ~150 ms. The isolation these tests need is isolation of
  *data*, and that never required a container each. `New`'s signature and
  contract are unchanged, so no test was touched.
  - Verified rather than assumed, since a clone that quietly lost a
    database-level property would make every RLS test in the repo pass for
    the wrong reason: `internal/dbtest` now has its own tests pinning that
    each test gets a separate database, that `cc_owner` still owns the
    tables, that `cc_app` is neither owner nor `BYPASSRLS`, that RLS is
    enabled, and that the clone is fully migrated. End-to-end, breaking
    `project_members`' RLS policy still fails
    `internal/db`'s `TestSetLocalDoesNotLeakAcrossPooledConnections`,
    confirming policies are genuinely enforced on a cloned database.
  - Noted while doing this, not fixed: `internal/handoffs`'
    `TestRLSIsolationBetweenProjects` passes with that table's RLS policy
    replaced by `USING (true)`. It asserts through `Service`, whose
    capability check and `WHERE project_id = $1` already produce the
    expected result, so it does not actually exercise RLS despite its name.
    Pre-existing, unrelated to this change.

- **Unified keyset pagination across every growing list** — the last Phase 0
  release blocker. `artifacts`, `handoffs` and `secrets` had no pagination at
  all (a project with more rows than a screen had no way to reach the rest);
  `history.ListHistoryObjects` was hard-capped at 1000 rows with nothing past
  it reachable; `events` and `audit` each had their own cursor shape. All six
  now share one contract — `limit` + an opaque `cursor` in, rows +
  `next_cursor` out — over HTTP, MCP and the CLI. See cookbook §22.1.
  - **Breaking response shape:** list endpoints return
    `{"items": [...], "next_cursor": "..."}` instead of a bare JSON array. An
    array has nowhere to put "there is more, continue from here", which is the
    whole problem. `next_cursor` is omitted on the last page, so "is there
    more" is a field's presence rather than a guess from the row count.
  - **Breaking query params:** audit's `?before_id=` and events' `?after_seq=`
    are replaced by `?cursor=`. `ccd event list --after-seq` survives as a
    shorthand, because every output line starts with the event's seq and
    resuming from one you just read shouldn't require saving a cursor.
  - New `internal/paging` holds the shared primitives. Cursors are opaque and
    carry their own kind, so one echoed to a differently-ordered endpoint is
    rejected (HTTP 400) rather than reinterpreted as a position it never
    meant.
  - Timestamp-ordered lists key on `(timestamp, id)` as a row-value
    comparison, not on the timestamp alone: two rows can share a `created_at`
    to the microsecond, and a timestamp-only cursor then skips every row but
    one of the colliding group. The regression tests force that collision
    explicitly — writing rows in a tight loop does *not* produce it, and the
    first version of those tests passed against a deliberately broken cursor
    because of that.
  - Services ask the database for `limit + 1` rows, so a cursor is returned
    only when another row actually exists. The web UI's "Load more" follows
    that, replacing a `page.length >= PAGE_SIZE` guess that both hid rows
    behind a missing button and offered a button that returned nothing.
  - Web UI: `useCursorList` + `LoadMore` give Artifacts, Handoffs,
    Conversations and Secrets the "Load more" that only Audit and Events had.
  - Deliberately not paginated, with reasons in cookbook §22.1:
    `skill.list_effective` (composition happens after the read, so paging
    would change which binding wins) and the member lists (bounded sets).

### Fixed

- **A project could never adopt a newer version of a skill.**
  `skill_bindings` had no uniqueness on `(scope, scope_ref, skill_id)`, so
  installing a skill a scope already bound added a *second* row; and
  `ListEffective` composes by scope rank only, so two bindings of equal rank
  left the winner undefined — in practice the older one kept winning.
  Since docs/adr/0012 forbids `skill.publish` from touching a pinned binding
  and no upgrade operation exists, installing again was the only move
  available, and it silently did nothing. Migration 000022 deduplicates
  (keeping the most recently installed row per group) and adds
  `UNIQUE NULLS NOT DISTINCT (scope, scope_ref, skill_id)` — `NULLS NOT
  DISTINCT` because `scope_ref` is NULL for installation scope, which the
  default SQL rule would have exempted from the constraint entirely.
  `Install` and `Override` now upsert, so installing again *moves* the
  binding, which is what the word means to anyone typing it.
  - Found by running `ccd sync-skills` against a live server rather than
    only in tests: it reported "unchanged" for a skill whose new version had
    just been installed, which was correct — the binding really had not
    moved.
- **Skill content could be read across project boundaries.** `skill.get`
  (added in Phase 1) authorized reads against an *anchor* project the caller
  picks, and `skills`/`skill_versions` carry no RLS — so any principal
  holding `CapSkillRead` anywhere, which every project role does, could read
  the content of any version whose UUID it knew, including one published
  inside a project it was not a member of. Confirmed by probe before fixing:
  a READER in an unrelated project read back a runbook published in someone
  else's project.
  - Migration 000020 records `published_in_project` and `published_by` on
    `skill_versions` — provenance `Publish` already had (it takes a
    projectID and checks `CapSkillPublish` on it) and was discarding. The
    rule is now: **you may read a skill's content if you can see a binding
    for it, or if you are a member of the project it was published from.**
    An unreadable version returns not-found rather than forbidden, the same
    reasoning `projects.Resolve` uses for a non-member.
  - Everything that worked still works: an installation-scope binding is
    visible to every principal, so installation-wide skills stay
    installation-wide; effective skills resolve through bindings the project
    can see; and a publisher reads its own package back before anyone has
    installed it. Each of those is pinned by a test, alongside the
    cross-project denial.
  - Deliberately *not* a decision about what the registry is — ADR 0012
    scopes the binding, not the package, and whether packages are
    instance-wide, organization-scoped or explicitly promoted is still open
    (NEXT.md). This records the provenance every one of those answers needs.
- **Four "RLS isolation" tests did not test RLS.** `internal/handoffs`,
  `internal/history` and `internal/secrets` each had a
  `TestRLSIsolationBetweenProjects` that passed unchanged with the table's
  policy replaced by `USING (true)`: they assert through the Service, whose
  capability check and explicit `WHERE project_id = $1` produce the expected
  result on their own. `internal/artifacts` — the domain that actually holds
  project content — had no RLS test at all, while its fixture comment
  claimed its tests "genuinely exercise RLS". All four now count a table
  through `cc_app` with the caller's identity set and no project filter,
  which is the only shape that can observe the policy; each was verified to
  fail with RLS disabled.
  - Found by granting `cc_app` `BYPASSRLS` and running the suite: **only
    nine tests in the entire repo fail** with Row-Level Security effectively
    off. `domain_events`, `audit_log`, `skill_bindings`, `gate_bindings` and
    `project_keys` remain uncovered — logged in NEXT.md rather than fixed
    here.
- **Three defects in the Phase 1 context pack, found in code review before
  merge.** All three shared a shape: the pack told the caller something that
  was not true, in a way the caller could not detect.
  - **Convention-selected artifacts vanished behind unrelated activity.**
    `internal/contextpack` listed artifacts with the general listing —
    ordered by `updated_at` across *every* artifact in the project, default
    limit 100 — and filtered the `context/` prefix afterwards, so a
    must-read file nobody had touched lately fell off the end of that page.
    One old `context/instructions.md` plus a hundred newer unrelated writes
    produced a pack with no instructions in it and an empty
    `budget.truncated` claiming it was complete. The prefix is now filtered
    in SQL by a new `artifacts.Service.ListByPathPrefix` (keyset-paginated
    by logical path, `LIKE` prefix properly escaped so a path containing
    `%` or `_` can't act as a wildcard), and the cap now counts only
    artifacts under the prefix — which is what makes "a project with more
    must-read files than this has outgrown the convention" a true statement
    rather than the misleading comment it was. Hitting it is reported.
  - **The continuation cursor could outrun the pack's own content.** The
    event boundary was captured *last*, after every content section had
    been read in its own transaction, so a write landing mid-assembly was
    missing from the content *and* behind a cursor that would never report
    it — invisible from both directions, the one outcome an agent cannot
    recover from. The boundary is now taken before any content is read: the
    cursor may replay an event the pack already reflects, which costs a
    wasted call, instead of losing one, which costs a wrong answer. Pinned
    by an internal test that lands a write in exactly that window (verified
    to fail against the previous ordering).
  - **Nothing said *who* held a claimed handoff.** The lifecycle recorded
    `claimed_by_principal` and `claimed_by_client`, but MCP returned
    neither, the context pack returned neither, and HTTP omitted the client
    id. A claim whose response was lost therefore left the claimer unable
    to recognize its own work on the retry — `handoff.current` showed
    "claimed" either way, and a principal id would not have settled it
    since two clients can share one. Both ids now travel through HTTP, MCP
    and the pack, and MCP additionally answers the question directly with
    `claimed_by_you`, computed against the client credential the session
    authenticated with. The `handoff.current`/`handoff.claim` tool
    descriptions asserted the opposite ("claimed means another client
    already did") and were corrected — a wrong description steers the model
    as surely as wrong data.
- **The Docker image could not write artifacts at all.** `/data` (the
  filesystem ObjectStore root, `$CC_STORAGE_DIR`) did not exist in the
  image, so Docker created the volume mount point as `root:root` while the
  container runs as `nonroot` (uid 65532) — every artifact and skill write
  failed with permission denied, surfacing as a bare HTTP 500, in a
  distroless image with no shell to `chown` it afterwards. The image now
  ships an empty `/data` owned by 65532:65532 for Docker to seed the volume
  from. Found by actually running the new quickstart rather than reviewing
  it.

- **Concurrent writes to the same secret silently lost one.**
  `internal/secrets.LocalEncryptedBackend.Store` read the current version and
  then upserted version+1 with nothing serializing the two steps, so N
  concurrent rotations all read version *N* and all wrote *N+1*. Confirmed
  before fixing: 8 concurrent rotations left the stored version at 4 instead
  of 9. Nothing surfaced it, because the AEAD associated data stays
  self-consistent (both writers bind *N+1*) and the surviving value decrypts
  normally. Now serialized per `(project_id, name)` with a transaction-scoped
  advisory lock. `SELECT ... FOR UPDATE` would not have covered it: on a
  secret's first write there is no row to lock yet, and that is the worse
  case — two writers that both see no row each mint their own `id` and bind it
  into the AAD, while the upsert's `DO UPDATE` leaves the first inserter's
  `id` on the row, so a ciphertext can end up bound to an `id` the row no
  longer carries and stop decrypting at all.
- **The first two concurrent writes to a new project could both fail.**
  Found while fixing the above, and the broader of the two:
  `internal/crypto.Provider.ensureProjectKey` had the same read-then-insert
  shape, so writes arriving together at a brand-new project all saw
  `ErrNoProjectKey` and all inserted `key_version` 1. All but one failed on
  `project_keys`' unique constraint, surfacing as an opaque SQLSTATE 23505 —
  and because every encrypting domain (artifacts, handoffs, history, secrets)
  provisions the key lazily through this one path, it was not a secrets
  problem at all. Reproduced at 4 failures out of 16 concurrent writers.
  The lock is taken only on the path that would insert, and the key is
  re-read under it, so the common case — a project whose key already exists —
  still takes no lock and no extra round trip. `RotateProjectKey` takes the
  same lock, for the same read-then-insert reason.
  - New shared primitive `db.AdvisoryXactLock` with a `LockClass` registry
    (`internal/db/advisory.go`): advisory locks share one flat, cluster-wide
    key space, so every class used anywhere is declared in one const block,
    with the nesting order (secret value, then project key — never the
    reverse) documented where it can be checked.
  - Regression tests for both, each verified to fail against the unfixed
    code. `docs/gotchas.md` records why the first version of one of them
    passed anyway: a barrier placed around the transaction rather than
    inside it tests connection setup, not concurrency.

- **Security — an ADMIN could escalate to OWNER by adding a principal they
  control.** Both `POST /v1/projects/{ns}/{slug}/members` and the
  `project.add_member` MCP tool accepted any of the four roles once the
  caller passed a bare `auth.CapMemberManage` check — which `RoleAdmin`
  holds, deliberately *without* `CapProjectDelete`. An ADMIN could therefore
  add a second principal as OWNER and exercise ownership through it, which is
  ownership transfer by a longer route. `projects.Store.AddMember` is a plain
  `INSERT` with no `ON CONFLICT`, so an ADMIN could not rewrite their own
  membership row — the escalation needed a second principal, which is why
  nothing surfaced it.
  - Granting OWNER is now its own capability, `auth.CapMemberGrantOwner`,
    held by `RoleOwner` alone and checked on top of `CapMemberManage`.
  - The check lives in a new `internal/members.Service` — the domain
    chokepoint ADR 0014 requires — not in the two adapters that each carried
    their own copy, so a third adapter cannot reintroduce the hole by
    forgetting it. It is a package of its own rather than a service inside
    `internal/projects` because `internal/auth` imports `internal/projects`
    for `projects.Role`; a `projects.Service` holding an `*auth.AuthzService`
    would be an import cycle. `internal/projects` stays the store, with no
    authorization of its own.
  - The same hole existed at organization scope and is closed the same way:
    `organizations.CapOrgGrantOwner` (OWNER-only) gates promoting anyone to
    organization OWNER, on top of `CapOrgManageMembers`, which ADMIN holds.
  - Both services now reject an unrecognized role (`ErrInvalidRole`, HTTP
    400) instead of letting it reach the database, where `RoleCan` would
    later map it to no capabilities at all.
  - Web UI: the role select no longer offers OWNER to a caller who cannot
    grant it (`canGrantOwnerRole` in `web/src/lib/roles.ts`), on both the
    project Members tab and the organization detail page — a display
    decision only; the server re-checks every request.
  - Regression tests at all three levels: the pure capability matrices
    (`internal/auth`, `internal/organizations`), the domain services against
    a real Postgres, and end-to-end over HTTP — including that a denied
    grant writes no row, not merely that it returns 403.

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
