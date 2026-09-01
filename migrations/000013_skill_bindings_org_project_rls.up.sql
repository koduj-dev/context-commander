-- Same problem as gate_bindings (migrations/000012): the old all-commands
-- policy used cc_has_org_access for organization-scope skill_bindings rows,
-- which requires a separate organization_members row. A project member who
-- is not also an organization member could not see the organization's
-- (possibly non-overridable) binding of a skill, so
-- internal/skills.Service.Override's "is this skill non-overridable at a
-- wider scope" check silently saw zero rows and let the override through —
-- bypassing docs/adr/0012's overridable=false guarantee.

-- Split the policy so project membership can make an organization binding
-- visible to the skills service without also granting permission to mutate
-- organization bindings. Service-layer capability checks
-- (internal/skills.Service.checkScopeTarget) remain the primary
-- authorization layer; these policies preserve RLS's scope-isolation
-- defense in depth. Reuses cc_has_org_project_access, already defined by
-- migrations/000012.
DROP POLICY skill_bindings_scoped ON skill_bindings;

CREATE POLICY skill_bindings_select ON skill_bindings FOR SELECT
  USING (
    scope = 'installation'
    OR (
      scope = 'organization'
      AND (
        cc_has_org_access(current_setting('app.current_principal_id', true), scope_ref)
        OR cc_has_org_project_access(current_setting('app.current_principal_id', true), scope_ref)
      )
    )
    OR (scope = 'project' AND cc_has_project_access(current_setting('app.current_principal_id', true), scope_ref))
  );

CREATE POLICY skill_bindings_insert ON skill_bindings FOR INSERT
  WITH CHECK (
    scope = 'installation'
    OR (scope = 'organization' AND cc_has_org_access(current_setting('app.current_principal_id', true), scope_ref))
    OR (scope = 'project' AND cc_has_project_access(current_setting('app.current_principal_id', true), scope_ref))
  );

CREATE POLICY skill_bindings_update ON skill_bindings FOR UPDATE
  USING (
    scope = 'installation'
    OR (scope = 'organization' AND cc_has_org_access(current_setting('app.current_principal_id', true), scope_ref))
    OR (scope = 'project' AND cc_has_project_access(current_setting('app.current_principal_id', true), scope_ref))
  )
  WITH CHECK (
    scope = 'installation'
    OR (scope = 'organization' AND cc_has_org_access(current_setting('app.current_principal_id', true), scope_ref))
    OR (scope = 'project' AND cc_has_project_access(current_setting('app.current_principal_id', true), scope_ref))
  );

CREATE POLICY skill_bindings_delete ON skill_bindings FOR DELETE
  USING (
    scope = 'installation'
    OR (scope = 'organization' AND cc_has_org_access(current_setting('app.current_principal_id', true), scope_ref))
    OR (scope = 'project' AND cc_has_project_access(current_setting('app.current_principal_id', true), scope_ref))
  );
