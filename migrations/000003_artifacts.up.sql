-- Phase 3: artifacts, immutable versions, mutable crypto envelopes, and the
-- transactional outbox — docs/adr/0010-immutable-artifact-version.md,
-- docs/adr/0013-transactional-outbox-and-storage-lifecycle.md.

CREATE TABLE artifacts (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id           uuid NOT NULL REFERENCES projects (id),
  logical_path         text NOT NULL,
  content_type         text NOT NULL,
  current_version      int NOT NULL DEFAULT 0,
  created_by_principal uuid NOT NULL REFERENCES principals (id),
  created_by_client    uuid REFERENCES client_credentials (id),
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  -- Soft delete: history stays for audit; see PurgeArtifact for the actual
  -- physical cleanup path (docs/adr/0013's tombstone-then-cleanup).
  deleted_at           timestamptz,
  UNIQUE (project_id, logical_path)
);

CREATE INDEX artifacts_project_id_idx ON artifacts (project_id) WHERE deleted_at IS NULL;

ALTER TABLE artifacts ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON artifacts
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- Immutable once inserted (docs/adr/0010) — cc_app gets no UPDATE grant at
-- all, so "someone adds an UPDATE code path later" fails at the database,
-- not just at code review.
CREATE TABLE artifact_versions (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id           uuid NOT NULL REFERENCES projects (id), -- denormalized for RLS + the envelope FK below
  artifact_id          uuid NOT NULL REFERENCES artifacts (id),
  version              int NOT NULL,
  content_hash         text NOT NULL, -- SHA-256 of PLAINTEXT, computed before encryption
  storage_key          text NOT NULL, -- ObjectStore key holding the ciphertext
  size_bytes           bigint NOT NULL,
  parent_version       int,
  created_by_principal uuid NOT NULL REFERENCES principals (id),
  created_by_client    uuid REFERENCES client_credentials (id),
  created_at           timestamptz NOT NULL DEFAULT now(),
  UNIQUE (artifact_id, version)
);

CREATE INDEX artifact_versions_artifact_id_idx ON artifact_versions (artifact_id);

ALTER TABLE artifact_versions ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON artifact_versions
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- Mutable crypto wrapping state, kept separate so key rotation never
-- touches artifact_versions (docs/adr/0010). Also insert-only for now: see
-- docs/adr/0005's revised RotateProjectKey — nothing currently UPDATEs this
-- table after insert either, but it's kept structurally distinct in case a
-- future explicit re-encrypt-to-latest-key operation needs to.
CREATE TABLE artifact_version_envelopes (
  artifact_version_id uuid PRIMARY KEY REFERENCES artifact_versions (id),
  project_id          uuid NOT NULL REFERENCES projects (id),
  nonce               bytea NOT NULL,
  wrapped_dek         bytea NOT NULL,
  algorithm           text NOT NULL DEFAULT 'AES-256-GCM',
  project_key_version int NOT NULL,
  updated_at          timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (project_id, project_key_version) REFERENCES project_keys (project_id, key_version)
);

ALTER TABLE artifact_version_envelopes ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON artifact_version_envelopes
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- Transactional outbox (docs/adr/0013). A row is written in the SAME
-- transaction as the domain mutation it describes — never published as a
-- separate step afterwards. seq is the monotonic resume cursor; event_id is
-- for consumer-side dedup only, never for ordering.
CREATE TABLE domain_events (
  seq           bigserial PRIMARY KEY,
  event_id      uuid NOT NULL DEFAULT gen_random_uuid(),
  project_id    uuid NOT NULL REFERENCES projects (id),
  type          text NOT NULL,
  resource_type text NOT NULL,
  resource_id   uuid,
  payload_kind  text NOT NULL DEFAULT 'metadata_only'
                CHECK (payload_kind IN ('metadata_only', 'encrypted_payload_ref')),
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX domain_events_project_id_seq_idx ON domain_events (project_id, seq);

ALTER TABLE domain_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON domain_events
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

GRANT SELECT, INSERT, UPDATE ON artifacts TO cc_app;
GRANT SELECT, INSERT ON artifact_versions TO cc_app; -- no UPDATE: immutable at the privilege level too
GRANT SELECT, INSERT ON artifact_version_envelopes TO cc_app;
GRANT SELECT, INSERT ON domain_events TO cc_app;
GRANT USAGE, SELECT ON domain_events_seq_seq TO cc_app;
