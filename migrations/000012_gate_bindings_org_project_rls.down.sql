DROP POLICY gate_bindings_delete ON gate_bindings;
DROP POLICY gate_bindings_update ON gate_bindings;
DROP POLICY gate_bindings_insert ON gate_bindings;
DROP POLICY gate_bindings_select ON gate_bindings;

CREATE POLICY gate_bindings_scoped ON gate_bindings
  USING (
    scope = 'installation'
    OR (scope = 'organization' AND cc_has_org_access(current_setting('app.current_principal_id', true), scope_ref))
    OR (scope = 'project' AND cc_has_project_access(current_setting('app.current_principal_id', true), scope_ref))
  );

DROP FUNCTION cc_has_org_project_access(text, uuid);
