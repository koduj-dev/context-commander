DROP POLICY scoped ON audit_log;
CREATE POLICY project_scoped ON audit_log
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- Organization-scoped rows have no representation in the project-only
-- vocabulary and no project to attribute them to; they are dropped rather
-- than left unreadable behind the restored policy.
DELETE FROM audit_log WHERE organization_id IS NOT NULL;

ALTER TABLE audit_log DROP CONSTRAINT audit_log_one_scope;
DROP INDEX audit_log_organization_id_ts_idx;
ALTER TABLE audit_log DROP COLUMN organization_id;
