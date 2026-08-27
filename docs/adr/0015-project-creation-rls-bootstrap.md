# 0015. RLS for project creation: `projects` needs its own policy, `project_members` needs a bootstrap INSERT path

## Status

Accepted

## Context

Wiring the first genuinely end-to-end "authenticate as `cc_app` → create a project → become its
first `OWNER`" flow (the `ccd project create` CLI command, ahead of Phase 4's adapters) surfaced
two gaps that every earlier test had missed, because every earlier test seeded `project_members`
through the RLS-exempt `cc_owner` pool (`db.WithoutPrincipal`), never through `cc_app` the way a
real caller does:

1. **`projects` had no RLS at all** (migration 000001). Any authenticated principal could
   `SELECT * FROM projects` and see every project's name, slug, and namespace, regardless of
   membership — a metadata leak, even though the content tables (`artifacts`, ...) were correctly
   protected.
2. **`project_members`'s policy had no explicit `WITH CHECK`.** Postgres reuses `USING` as
   `WITH CHECK` when none is given, so `INSERT` was gated by
   `cc_has_project_access(current_principal, project_id)` — "you're already a member of this
   project." That's impossible for a project's *first* member: at the moment that row is being
   inserted, no membership row for that project exists yet, so the check always fails. Every
   project creation through `cc_app` would have failed with a bare RLS violation.

## Decision

**`project_members`** splits into per-command policies (migration 000004) instead of one blanket
`USING`-only policy:

- `SELECT` / `UPDATE` / `DELETE`: the existing `cc_has_project_access(...)` check.
- `INSERT`, two permissive policies (Postgres OR's multiple permissive policies for the same
  command):
  - **Bootstrap**: `principal_id = current_principal AND NOT EXISTS (SELECT 1 FROM project_members
    WHERE project_id = NEW.project_id)` — a principal may add *themselves* only while the project
    has zero members. This can fire at most once per project, ever: the moment it succeeds, the
    `NOT EXISTS` becomes permanently false for that project.
  - **By an existing member**: `cc_has_project_access(...)` — deliberately coarse (any member, not
    role-checked) because RLS here is defense-in-depth, not the primary authority; a future
    project-membership domain service gates *who* may add *whom* via
    `AuthzService.Can(CapMemberManage)`, the same pattern `internal/artifacts.Service` already
    uses (`docs/adr/0014-authz-check-in-domain-service.md`).

**`projects`** gets RLS enabled with membership-gated `SELECT`/`UPDATE`/`DELETE` and an
unconditional `INSERT` (`WITH CHECK (true)`) — a project can't have a member-based ownership check
on creation because, again, no membership row exists for it yet. The caller is expected to add
itself as `OWNER` in the same transaction that creates the project; nothing else can ever read,
modify, or add further members to a project stuck with zero members, so an abandoned mid-transaction
create is self-limiting, not a stuck-open door.

**`namespaces`, `organizations`, `organization_members`, `users`, `principals` remain without
RLS**, deliberately: a namespace slug is meant to be a known, addressable handle (like a username),
not project content: `project.resolve("filip/es-migration")` has to work without the caller
already being a member of the target. Broader user-directory visibility (any authenticated
principal can currently `SELECT` basic `users`/`principals` rows) is a known, separately-scoped
question, not addressed by this ADR.

## Consequences

- `internal/projects.Store.Create` + `internal/projects.Store.AddMember` must run in the *same*
  transaction for project creation — a project committed without an immediate self-membership
  insert is permanently inaccessible to everyone, by design.
- The negative test suite gains a positive-path counterpart: creating a project and adding oneself
  as `OWNER` **through `cc_app`**, not just the existing negative (non-member/`READER` → `DENY`)
  tests. A test suite that only ever seeds fixtures through `cc_owner` cannot catch this class of
  bug — it has to exercise the real write path at least once.
- **`INSERT ... RETURNING` needs the SELECT policy to pass too, not just `INSERT`'s `WITH CHECK`
  — confirmed against a real Postgres, the fix above wasn't enough on its own.** Even with the
  bootstrap `WITH CHECK` policies in place, `projects.Store.Create` and `projects.Store.AddMember`
  still failed with "new row violates row-level security policy" as soon as they added `RETURNING`
  back to read the inserted row. The `WITH CHECK` policies pass; `RETURNING`'s implicit visibility
  check (equivalent to the table's `SELECT`/`USING` policies) does not — and the self-referential
  membership check inside `cc_has_project_access()` cannot see the very row its own statement is
  in the middle of inserting, even within the same transaction, because a `STABLE` function's
  snapshot doesn't include the enclosing command's not-yet-completed effects. Fix: `Create` and
  `AddMember` don't use `RETURNING` at all — every field is already known client-side (a
  client-generated `uuid.New()` ID, the input values, `time.Now().UTC()` for `created_at`), so
  there was nothing `RETURNING` was actually buying. Generalize this: **any `INSERT ... RETURNING`
  on an RLS-protected table whose policy could plausibly be false for a brand-new row must not
  rely on `RETURNING`** — construct the returned struct from known inputs instead.
