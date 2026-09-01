-- project_members and organization_members are PK'd (project_id,
-- principal_id) / (organization_id, user_id) — a "list every project/org
-- this principal belongs to" query (needed by the web UI's dashboard) can't
-- use either composite PK's btree, since principal_id/user_id isn't the
-- leading column. Add the missing secondary indexes.

CREATE INDEX project_members_principal_id_idx ON project_members (principal_id);
CREATE INDEX organization_members_user_id_idx ON organization_members (user_id);
