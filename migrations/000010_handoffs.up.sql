-- Handoff — structured hand-off state between agents, docs/architecture-cookbook.md
-- §9: "Agent A can leave enough structured state for Agent B to continue
-- without sharing vendor-specific conversation history." The body
-- (summary/completed work/next actions/references) is CONTENT PAYLOAD
-- (docs/adr/0011-encryption-classification.md) — encrypted as one JSON blob
-- through the same project-key envelope internal/secrets.LocalEncryptedBackend
-- uses (migrations/000009_secrets.up.sql), not the ObjectStore path artifacts
-- use, since a handoff body is small and always read back as a whole.
--
-- status/id/timestamps stay plaintext (INDEXABLE/OPERATIONAL METADATA, ADR
-- 0011) so List can filter without decrypting every row.
CREATE TABLE handoffs (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id           uuid NOT NULL REFERENCES projects (id),
  created_by_principal uuid NOT NULL REFERENCES principals (id),
  created_by_client    uuid REFERENCES client_credentials (id),
  -- Opaque pointer into internal/history.ExternalSession — no FK: Handoffs
  -- and History are independent domains shipped in the same phase, and a
  -- handoff may reference a session recorded by a client History never
  -- ingests. Cross-checking this ID's validity, if ever needed, is a
  -- caller-side concern.
  source_session_id    uuid,
  status               text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
  ciphertext            bytea NOT NULL,
  nonce                 bytea NOT NULL,
  wrapped_dek           bytea NOT NULL,
  project_key_version   int NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (project_id, project_key_version) REFERENCES project_keys (project_id, key_version)
);

CREATE INDEX handoffs_project_id_idx ON handoffs (project_id);

ALTER TABLE handoffs ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON handoffs
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- status is the only field ever meant to change after creation; body/
-- envelope columns have no UPDATE code path (Service has no Update method).
GRANT SELECT, INSERT, UPDATE ON handoffs TO cc_app;
