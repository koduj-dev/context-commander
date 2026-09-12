DROP POLICY skill_versions_scoped ON skill_versions;
ALTER TABLE skill_versions DISABLE ROW LEVEL SECURITY;

DROP POLICY skills_scoped ON skills;
ALTER TABLE skills DISABLE ROW LEVEL SECURITY;

REVOKE UPDATE (scope, scope_ref) ON skills FROM cc_app;

DROP INDEX skills_name_lower_idx;
DROP INDEX skills_scope_idx;
ALTER TABLE skills DROP CONSTRAINT skills_scope_ref_check;
ALTER TABLE skills DROP CONSTRAINT skills_scope_check;
ALTER TABLE skills DROP COLUMN scope_ref;
ALTER TABLE skills DROP COLUMN scope;
