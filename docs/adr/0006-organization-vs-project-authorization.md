# 0006. Organization role never implies project content access; visibility never implies authorization

## Status

Accepted

## Context

Two related ways review round 2 found the project-authorization-boundary invariant could quietly
grow a second, hidden authority:

- **§9**: if `Organization OWNER` implicitly meant "can read every artifact in every project in the
  organization," that's a second authorization model living beside `project_members`, undermining
  "Project is the basic authorization boundary" (`docs/architecture-cookbook.md` §4).
- **§14**: `projects.visibility` (`private`/`organization`/`public`) is easy to reach for as a
  shortcut for "who can read this" in application code, but sharing is already fully defined by
  `project_members`. Two overlapping sources of truth drift apart.

## Decision

- `AuthzService.Can()` decisions for project content (`artifact.*`, `handoff.*`, ...) are decided
  **exclusively** by `project_members` (+ RLS as its Postgres-backend mirror, ADR 0002). Nothing
  about `organization_members` participates in that decision.
- `OrganizationMembership` roles (`OWNER/ADMIN/MEMBER`) govern organization administration:
  namespace administration, organization-scope skills/policies, and *project creation inside the
  organization's namespace* — not access to a project's content once created.
- If a future need arises for "org owner can see all projects" (e.g. compliance export), it is
  implemented as an explicit, separately-named capability (e.g.
  `organization.admin_read_all_projects`) checked and audited on its own — never as an implicit
  side effect of the `OWNER` role.
- `projects.visibility` is discovery metadata only: whether the project is listed/findable
  (`private` = not discoverable, `organization` = listed in the org directory, `public` = globally
  listed). It is never read by `AuthzService.Can()`.

## Consequences

- A test asserting "Organization OWNER without explicit project membership gets DENY on
  `artifact.read`" is part of the Phase 1 negative test suite, not optional.
- Code review checklist: any new capability check that reads `organization_members` to authorize
  *project content* access is a bug, not a shortcut.
