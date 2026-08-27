-- Fixes two RLS gaps found while wiring the first real cc_app-authenticated
-- "create a project, become its first OWNER" flow (the CLI's `project
-- create` + `artifact write` path) — never exercised by earlier tests,
-- which all seeded project_members via the RLS-exempt cc_owner pool, not
-- through cc_app the way a real caller does.
--
-- Gap 1: migration 000001's `project_members` policy had no explicit
-- WITH CHECK, so Postgres reuses USING as WITH CHECK too — meaning INSERT
-- was gated by "you're already a member of this project," which is
-- impossible for a project's very first member. Every project creation
-- through cc_app would have failed.
--
-- Gap 2: `projects` had no RLS at all — any authenticated principal could
-- `SELECT * FROM projects` and see every project's name/slug/namespace
-- regardless of membership.

DROP POLICY project_scoped ON project_members;

CREATE POLICY project_members_select ON project_members FOR SELECT
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- A plain `NOT EXISTS (SELECT 1 FROM project_members WHERE project_id = ...)`
-- inside the bootstrap WITH CHECK below would itself be filtered by
-- project_members_select's own RLS — so a non-member attacker's subquery
-- would ALWAYS see zero rows for someone else's already-owned project (not
-- because it has no members, but because RLS hides them from the attacker),
-- making "no members" look true when it isn't. Confirmed with an actual
-- adversarial test, not just reasoned about: an early version of this
-- migration let exactly that through. Same fix as cc_has_project_access —
-- a SECURITY DEFINER helper, owned by cc_owner, so the existence check runs
-- against the real table state, not the caller's RLS-filtered view of it.
CREATE FUNCTION cc_project_has_no_members(p_project_id uuid)
RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT NOT EXISTS (SELECT 1 FROM project_members WHERE project_id = p_project_id);
$$;

REVOKE ALL ON FUNCTION cc_project_has_no_members(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_project_has_no_members(uuid) TO cc_app;

-- Bootstrap allowance: a principal may insert a membership row for
-- THEMSELVES when the project currently has zero members — the only way a
-- brand-new project can ever get its first member. Once any member exists,
-- cc_project_has_no_members() is false for that project forever after, so
-- this clause can fire at most once per project and cannot be reused to add
-- a second member without going through the policy below.
CREATE POLICY project_members_insert_bootstrap ON project_members FOR INSERT
  WITH CHECK (
    principal_id = current_setting('app.current_principal_id', true)::uuid
    AND cc_project_has_no_members(project_id)
  );

-- An existing member may add another principal. Coarse on purpose — RLS is
-- defense-in-depth here, not the primary check; a future project-membership
-- domain service gates this with AuthzService.Can(CapMemberManage) the same
-- way internal/artifacts.Service does for artifact capabilities
-- (docs/adr/0014), which is where role-specific restriction belongs.
CREATE POLICY project_members_insert_by_member ON project_members FOR INSERT
  WITH CHECK (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

CREATE POLICY project_members_update ON project_members FOR UPDATE
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

CREATE POLICY project_members_delete ON project_members FOR DELETE
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- projects: INSERT must stay unconditional — nobody can be a member of a
-- project before it exists — but SELECT/UPDATE/DELETE require membership,
-- same as every other project-scoped table.
ALTER TABLE projects ENABLE ROW LEVEL SECURITY;

CREATE POLICY projects_select ON projects FOR SELECT
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), id));

CREATE POLICY projects_insert ON projects FOR INSERT
  WITH CHECK (true);

CREATE POLICY projects_update ON projects FOR UPDATE
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), id));

CREATE POLICY projects_delete ON projects FOR DELETE
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), id));
