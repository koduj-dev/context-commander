DROP POLICY skill_bindings_delete ON skill_bindings;
DROP POLICY skill_bindings_update ON skill_bindings;
DROP POLICY skill_bindings_insert ON skill_bindings;
DROP POLICY skill_bindings_select ON skill_bindings;

CREATE POLICY skill_bindings_scoped ON skill_bindings
  USING (
    scope = 'installation'
    OR (scope = 'organization' AND cc_has_org_access(current_setting('app.current_principal_id', true), scope_ref))
    OR (scope = 'project' AND cc_has_project_access(current_setting('app.current_principal_id', true), scope_ref))
  );
