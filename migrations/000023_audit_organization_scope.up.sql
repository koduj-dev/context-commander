-- Audit rows for organization-scoped changes.
--
-- audit_log has always been project-scoped: one nullable project_id and an
-- RLS policy of "cc_has_project_access(project_id)" with no separate
-- WITH CHECK, which Postgres therefore reuses as the insert check. Adding a
-- member to an ORGANIZATION has no project, so cc_has_project_access(NULL)
-- is false and the row could not be inserted at all — which is why
-- organizations.Service.AddMember has never written one, and why the
-- backlog's "cheap: one audit.Write" was true only for the project half.
--
-- Organizations are a peer scope to projects (docs/adr/0006), not something
-- below them, so the audit table gets the same treatment: its own column
-- and its own access clause.
ALTER TABLE audit_log ADD COLUMN organization_id uuid REFERENCES organizations (id);

CREATE INDEX audit_log_organization_id_ts_idx ON audit_log (organization_id, ts);

-- Exactly one scope per row. A row belonging to both would be readable
-- through either, which makes "who can see this audit entry" ambiguous;
-- a row belonging to neither would be readable by nobody and is therefore
-- write-only noise.
ALTER TABLE audit_log ADD CONSTRAINT audit_log_one_scope
  CHECK ((project_id IS NULL) <> (organization_id IS NULL));

DROP POLICY project_scoped ON audit_log;
CREATE POLICY scoped ON audit_log
  USING (
    (project_id IS NOT NULL AND cc_has_project_access(current_setting('app.current_principal_id', true), project_id))
    OR (organization_id IS NOT NULL AND cc_has_org_access(current_setting('app.current_principal_id', true), organization_id))
  );
