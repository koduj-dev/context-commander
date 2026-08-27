# 0004. Principal (User | ServicePrincipal) is a real project member, not just provenance

## Status

Accepted

## Context

Review round 1 introduced `Principal` as an abstraction over `User` and `ServicePrincipal` so a
CI/nightly agent could have provenance without pretending to be a human
(`docs/architecture-cookbook.md` §6.3). Round 1's schema only used `Principal` in `created_by_*`
columns, though — `project_members` still had `user_id`. Round 2 (blocker A) pointed out that means
a `ServicePrincipal` can never actually be a project member, never gets project-scoped RBAC, and
never passes RLS. It was provenance metadata wearing the name of a principal, not a real one.

## Decision

- `principals` is a small dispatch table: `kind` (`user` | `service`) plus exactly one of
  `user_id` / `service_principal_id`, enforced by a `CHECK` constraint.
- `client_credentials.principal_id`, `project_members.principal_id`,
  `artifact_versions.created_by_principal`, and every future `created_by_*` column reference
  `principals(id)` — never `users(id)` directly.
- `project_members` uses the same four roles (`OWNER/ADMIN/CONTRIBUTOR/READER`) for both kinds of
  principal. A CI agent added as `CONTRIBUTOR` is authorized exactly like a human `CONTRIBUTOR`.
- `AuthzService.Can(ctx, principal Principal, projectID, capability)` takes a `Principal`, never a
  bare user ID, and RLS's `app.current_principal_id` ([0003](0003-set-local-transaction-context.md))
  is the principal ID, not a user ID.
- Organization membership (`organization_members`) stays `user_id`-scoped — service principals
  administering an organization is not a need this schema has to solve yet, and organization role
  is deliberately **not** a project-content authorization path in the first place
  (see [0006](0006-organization-vs-project-authorization.md)).

## Consequences

- Any code path that resolves "who can access this project" must go through `principals`, not
  `users`, or a `ServicePrincipal` silently falls through as unauthorized even when correctly
  granted a role.
- Bootstrap ([0009](0009-pat-format-and-bootstrap.md)) creates a `User`, wraps it in a `Principal`,
  then issues a `ClientCredential` against that principal — never a bare user row with implicit
  access.
