DROP POLICY project_scoped ON encrypted_audit_payload;
ALTER TABLE encrypted_audit_payload DISABLE ROW LEVEL SECURITY;

DROP POLICY project_scoped ON audit_log;
ALTER TABLE audit_log DISABLE ROW LEVEL SECURITY;

ALTER TABLE encrypted_audit_payload DROP CONSTRAINT encrypted_audit_payload_pkey;
ALTER TABLE encrypted_audit_payload DROP COLUMN project_key_version;
