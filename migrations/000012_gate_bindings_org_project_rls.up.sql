-- Organization-scope gates apply to every project in the organization, so
-- evaluating a gate must not depend on the acting user also having a separate
-- organization_members row. Project membership is principal-scoped (and can
-- include service principals), while organization membership is user-scoped;
-- requiring the latter made an org-wide gate silently invisible to otherwise
-- authorized project writers.

CREATE FUNCTION cc_has_org_project_access(p_principal_id_text text, p_org_id uuid)
RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = public
AS $$
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
    SELECT 1
      FROM project_members pm
      JOIN projects p ON p.id = pm.project_id
      JOIN namespaces n ON n.id = p.namespace_id
     WHERE pm.principal_id = p_principal_id
       AND n.organization_id = p_org_id
  );
END;
$$;

REVOKE ALL ON FUNCTION cc_has_org_project_access(text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_has_org_project_access(text, uuid) TO cc_app;

-- The old all-commands policy used the same predicate for reads and writes.
-- Split it so project membership can make an organization binding visible to
-- the gate engine without also granting permission to mutate organization
-- bindings. AdminService remains the primary role-aware authorization layer;
-- these policies preserve RLS's scope-isolation defense in depth.
DROP POLICY gate_bindings_scoped ON gate_bindings;

CREATE POLICY gate_bindings_select ON gate_bindings FOR SELECT
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

CREATE POLICY gate_bindings_insert ON gate_bindings FOR INSERT
  WITH CHECK (
    scope = 'installation'
    OR (scope = 'organization' AND cc_has_org_access(current_setting('app.current_principal_id', true), scope_ref))
    OR (scope = 'project' AND cc_has_project_access(current_setting('app.current_principal_id', true), scope_ref))
  );

CREATE POLICY gate_bindings_update ON gate_bindings FOR UPDATE
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

CREATE POLICY gate_bindings_delete ON gate_bindings FOR DELETE
  USING (
    scope = 'installation'
    OR (scope = 'organization' AND cc_has_org_access(current_setting('app.current_principal_id', true), scope_ref))
    OR (scope = 'project' AND cc_has_project_access(current_setting('app.current_principal_id', true), scope_ref))
  );
