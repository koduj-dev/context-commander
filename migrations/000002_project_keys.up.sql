-- Phase 2: project key hierarchy — docs/adr/0005-key-hierarchy-and-artifact-size-limit.md.
-- Root/installation key never appears here — only what it wrapped.

CREATE TABLE project_keys (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id    uuid NOT NULL REFERENCES projects (id),
  key_version   int NOT NULL,
  -- 12-byte GCM nonce prefix + ciphertext, packed into one column (this
  -- table has no separate nonce column, unlike artifact_version_envelopes
  -- later — see internal/crypto/wrap.go for the pack/unpack format).
  wrapped_key   bytea NOT NULL,
  root_key_ref  text NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, key_version)
);

CREATE INDEX project_keys_project_id_idx ON project_keys (project_id);

-- Project-scoped: same RLS pattern as project_members (docs/adr/0002).
ALTER TABLE project_keys ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON project_keys
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

GRANT SELECT, INSERT ON project_keys TO cc_app;
