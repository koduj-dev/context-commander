# 0002. Non-recursive RLS via a SECURITY DEFINER helper, plus an explicit DB role model

## Status

Accepted (Postgres backend only — see [0007](0007-metadata-store-abstraction-sqlite.md))

## Context

Row-Level Security is defense-in-depth on top of `AuthzService.Can()`, not a replacement for it
(`docs/architecture-cookbook.md` §4). Two mistakes are easy to make when adding it:

1. A policy on `project_members` that itself does `SELECT project_id FROM project_members WHERE
   ...` is self-recursive.
2. `ENABLE ROW LEVEL SECURITY` alone proves nothing if the runtime role is the table owner —
   owners bypass their own table's RLS by default — or has `BYPASSRLS`.

Review round 2 (blocker B) flagged both. This ADR fixes them together because the fix for one
enables the fix for the other.

## Decision

**Three roles**, defined once in `deploy/postgres/init/01-roles.sql` for local dev and mirrored in
whatever provisioning the production deployment uses:

- `cc_owner` — owns every table and function. Migrations run as this role
  (`make migrate`/`Makefile` `DB_OWNER_URL`). `NOSUPERUSER NOBYPASSRLS`, but exempt from RLS on
  tables it owns (Postgres default — no `FORCE ROW LEVEL SECURITY` on `project_members`).
- `cc_app` — the only role the running Go server connects as. `NOSUPERUSER NOBYPASSRLS`, never an
  owner, granted narrow `SELECT/INSERT/UPDATE/DELETE` per table by each migration that creates one.
- `cc_migrator` is `cc_owner` itself for now; split out later only if a real operational reason
  appears.

**Non-recursive helper**, owned by `cc_owner`, `SECURITY DEFINER`. Takes the principal id as
**`text`, not `uuid`**, and casts internally with a guard — confirmed necessary by testing against a
real Postgres instance (not just reasoned about): `current_setting('app.current_principal_id', true)`
returns `NULL` only the *first* time that custom GUC is ever touched on a backend/connection. After
any `set_config()` call on it — i.e. after the first `WithPrincipal` transaction on a pooled
connection — it returns `''` (empty string, not `NULL`) for every later transaction that doesn't set
it again. Casting `''::uuid` at the `USING` clause directly would hard-error
("invalid input syntax for type uuid") instead of safely denying, on any query that reaches a
project-scoped table outside `WithPrincipal` on an already-used connection:

```sql
CREATE FUNCTION cc_has_project_access(p_principal_id_text text, p_project_id uuid)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = public AS $$
DECLARE
  p_principal_id uuid;
BEGIN
  IF p_principal_id_text IS NULL OR p_principal_id_text = '' THEN
    RETURN false;
  END IF;
  BEGIN
    p_principal_id := p_principal_id_text::uuid;
  EXCEPTION WHEN invalid_text_representation THEN
    RETURN false;
  END;
  RETURN EXISTS (
    SELECT 1 FROM project_members
    WHERE principal_id = p_principal_id AND project_id = p_project_id
  );
END;
$$;
```

Because the function is `SECURITY DEFINER` and owned by `cc_owner`, its internal query against
`project_members` runs *as `cc_owner`* — which is RLS-exempt on that table — so a policy on
`project_members` itself that calls this helper is not recursive. Every other project-scoped table
uses the identical policy shape:

```sql
CREATE POLICY project_scoped ON <table>
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));
```

**Session context is `SET LOCAL`, never `SET`**, and only ever set from one place — see
[0003](0003-set-local-transaction-context.md).

## Consequences

- Adding a new project-scoped table means: `ENABLE ROW LEVEL SECURITY` + the one `CREATE POLICY`
  shape above + a `GRANT` to `cc_app`. No table gets a bespoke policy — **except** the table that
  records membership itself (`project_members`) and any table whose row can be created before
  membership exists (`projects`): those need an explicit `WITH CHECK` split by command, not the
  single blanket `USING` shape above, or the very first `INSERT` for a new project is
  unsatisfiable. See [0015](0015-project-creation-rls-bootstrap.md) — found only once a real
  `cc_app`-authenticated "create a project" path was actually exercised, not by any test that
  seeds fixtures through `cc_owner`.
- The negative test suite (cross-project read → DENY) must run **as `cc_app`**, not as `cc_owner`
  or the initdb superuser, or it proves nothing (review round 2, acceptance criteria).
- `FORCE ROW LEVEL SECURITY` is reserved for tables where even `cc_owner`-run code must not read
  across projects without the helper — candidate: `encrypted_audit_payload`. Not applied by
  default because it would make ordinary migrations (run as `cc_owner`) fight their own RLS.
