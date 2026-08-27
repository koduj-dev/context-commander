DROP POLICY IF EXISTS projects_delete ON projects;
DROP POLICY IF EXISTS projects_update ON projects;
DROP POLICY IF EXISTS projects_insert ON projects;
DROP POLICY IF EXISTS projects_select ON projects;
ALTER TABLE projects DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS project_members_delete ON project_members;
DROP POLICY IF EXISTS project_members_update ON project_members;
DROP POLICY IF EXISTS project_members_insert_by_member ON project_members;
DROP POLICY IF EXISTS project_members_insert_bootstrap ON project_members;
DROP POLICY IF EXISTS project_members_select ON project_members;
DROP FUNCTION IF EXISTS cc_project_has_no_members(uuid);

CREATE POLICY project_scoped ON project_members
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));
