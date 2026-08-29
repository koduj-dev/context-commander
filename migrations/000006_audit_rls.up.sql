-- Closes two gaps found while finishing internal/audit:
--
-- 1. audit_log/encrypted_audit_payload had no RLS at all — any
--    authenticated principal could read any project's audit trail.
-- 2. encrypted_audit_payload was missing project_key_version: without it,
--    a row encrypted under project key N can never be told apart from one
--    encrypted under key N+1 after a rotation (docs/adr/0005), so it could
--    never actually be decrypted correctly — same column
--    artifact_version_envelopes already carries for the same reason. The
--    table has no real rows yet (audit.WriteWithPayload does not exist
--    before this change), so this is a safe additive NOT NULL column, not
--    a backfill.

ALTER TABLE encrypted_audit_payload ADD COLUMN project_key_version int NOT NULL;
ALTER TABLE encrypted_audit_payload ADD PRIMARY KEY (audit_log_id);

ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON audit_log
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- encrypted_audit_payload has no project_id of its own — it inherits
-- audit_log's scoping through the FK, same shape as
-- artifact_version_envelopes inheriting from artifact_versions, except
-- here via an explicit join since project_id isn't denormalized onto this
-- table (audit_log_id is already the join key an attacker can't forge).
ALTER TABLE encrypted_audit_payload ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON encrypted_audit_payload
  USING (
    EXISTS (
      SELECT 1 FROM audit_log a
      WHERE a.id = encrypted_audit_payload.audit_log_id
        AND cc_has_project_access(current_setting('app.current_principal_id', true), a.project_id)
    )
  );
