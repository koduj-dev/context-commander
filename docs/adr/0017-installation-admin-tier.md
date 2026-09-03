# 0017. Installation-admin authorization tier

## Status

Accepted

## Context

`docs/architecture-cookbook.md` §18/§20 always drew "Installation" as the root container
of the domain model (`Installation > Organization? > Project`, and `Installation` owning
`User`/`ServicePrincipal`/`Skill`/`Gate` directly), but no code ever implemented it: `internal/auth`'s
`roleCapabilities` is keyed solely on `projects.Role`, nothing above organization exists anywhere,
and `identity.CreateServicePrincipal`/`CreatePrincipalForService` had zero adapter callers — agents
could not be registered through any interface, not even the CLI. `internal/skills` and
`internal/gates` both hard-rejected `scope == "installation"` mutations with `ErrUnsupportedScope`
("v1 has no installation-admin capability model ... operator-seeded only"), even though the schema
(`migrations/000007`/`000008`) and RLS (`migrations/000013`) already fully support installation-scope
bindings — only the Service-layer capability check was missing.

`NEXT.md`'s Phase 3 audit (2026-09-02) named three concrete gaps this blocks: admin-driven user
creation, Service Principal (agent) registration, and cross-principal credential management. All
three need an actual authorization-model decision first — "who/what makes a principal an
'installation admin'?" — not just an implementation plan, since an installation admin can create
arbitrary users and register arbitrary service principals: the blast radius of a mistake here is the
whole instance, not one project or organization.

## Decision

**A new `internal/installation` package**, following the exact pattern `internal/organizations`
already established for the org-vs-project split (docs/adr/0006): its own `Capability` string type,
a `roleCapabilities` table, a pure `RoleCan`, and a `Service.checkCapability` that resolves admin
status inside `db.WithPrincipal` and denies on the same non-error/DENY shape `AuthzService.Can` and
`organizations.Service.checkCapability` both use. Kept a distinct Go type rather than folded into
`internal/auth.Capability` or `internal/organizations.Capability` for the same reason ADR 0006 gives:
nothing in either of those packages can accidentally end up deciding an installation-admin question,
and nothing here can end up deciding a project- or organization-content one.

**Table**: `installation_admins(user_id PRIMARY KEY REFERENCES users, role, granted_by, granted_at)`
— `migrations/000017`. Keyed by `user_id`, not `principal_id`: a `ServicePrincipal` can never hold
installation-admin rights, mirroring ADR 0004's "service principals administering an organization is
not a need this schema has to solve yet" — more load-bearing here, since the whole point of this tier
is to let an admin *create* service principals; letting a service principal be one first would be a
privilege-escalation primitive by construction. No RLS: same trust model as `users`/`principals`/
`client_credentials` (all RLS-free since migration 000001) — every caller reaches this table through
`installation.Store`, which every adapter reaches only via `installation.Service`'s capability check,
except for granting the very first admin (next section).

**Granting is CLI/operator-only, never HTTP/MCP.** No authenticated principal can grant the very
first installation admin — nobody exists yet to hold the right to grant it. `ccd bootstrap`'s first
user is therefore granted installation-admin rights automatically (`grantedBy = nil`), and every
later grant goes through `ccd installation grant-admin --email <email>`, a `--database-url`-only
command with the same trust tier as `ccd migrate`/`ccd user create` — deliberately no
`installation.admin_grant` capability, no HTTP endpoint, no MCP tool. This was the one point in the
design explicitly confirmed with the maintainer before implementation: the alternative (an existing
admin grants another admin over HTTP) is a real, usable feature, but it is a self-service
privilege-escalation path this v1 does not need and does not want to have to get right under review
on day one. `ListAdmins`/`CapAdminRead` *is* HTTP/MCP-reachable (any admin can see who else is one) —
that's a read, not a new grant path.

**One role today** (`RoleAdmin`), holding every capability
(`CapAdminRead`/`CapUserCreate`/`CapServicePrincipalCreate`/`CapCredentialManage`/`CapSkillManage`/
`CapGateManage`). `role` is still a CHECK-constrained column and `roleCapabilities` is still a table,
not a single `IsAdmin -> true` shortcut, so a future narrower tier (e.g. a read-only `AUDITOR`) is an
`INSERT`/`UPDATE`-time decision plus one more map entry, not a migration or a rewrite of every call
site.

**Cross-principal credential management is a new, separate feature**, not a relaxation of
`adapters/http/tokens.go`'s existing self-only endpoints. `internal/identity` gained
`RevokeCredentialByID` (no ownership check, `installation.Service.RevokeCredentialFor`'s only caller)
alongside the existing ownership-checked `RevokeCredential` — the self-service path is unchanged and
still never accepts a principal id from a request body.

**`internal/skills` and `internal/gates`' `ScopeInstallation` handling changed from an unconditional
`ErrUnsupportedScope` to a real check**: both `Service`/`AdminService` now take an
`*installation.Store` and call `IsAdmin` directly against the already-open transaction in
`checkScopeTarget` (matching how they already call `orgs.GetMembership`/`projects.GetMembership`
directly rather than opening a nested transaction) — non-admins get the ordinary `ErrForbidden` every
other denied scope check already returns, admins get a real installation-scope binding.
`ErrUnsupportedScope` is removed from both packages; nothing produces it anymore.

## Consequences

- Every `installation.Service` method takes both `principalID` (RLS session context) and
  `callerUserID` (which the table is keyed by) — the same shape `organizations.Service` uses and for
  the same reason: the two aren't derivable from each other without a lookup the caller already has.
  Every adapter (HTTP/CLI/MCP) reuses the existing `resolveCallerUserID` helper already written for
  organizations, unchanged.
- `skills.NewService` and `gates.NewAdminService` both gained a required `*installation.Store`
  parameter — every call site (production wiring in `adapters/cli/serve.go`/`skill.go`/`gate.go`, and
  every integration test that constructed one directly) had to be updated in the same change.
- A negative test asserting "non-admin caller gets `ErrForbidden` at installation scope" and a
  positive test asserting "a real installation admin can actually write an installation-scope
  binding through the Service" are both part of the test suite now, for both `internal/skills` and
  `internal/gates` — the old test only proved the hard rejection, never that the feature could work
  at all.
- `ccd bootstrap`'s output now also states the created user is an installation admin — a behavior
  change an operator running it manually will see, not a silent side effect.
- Nothing in this package or its wiring lets one installation admin create another over HTTP or MCP.
  If that turns out to be needed, it is a new, explicitly-named capability (e.g.
  `installation.admin_grant`) added and reviewed on its own — never an implicit side effect of
  `CapUserCreate` or any other existing capability, same discipline ADR 0006 requires for
  organization-vs-project.
