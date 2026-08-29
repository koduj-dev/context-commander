-- Secret reference model — docs/architecture-cookbook.md §15,
-- docs/adr/0011-encryption-classification.md's SECRET class: a secret is
-- never stored as ordinary CONTENT PAYLOAD inline in another resource, only
-- referenced via a secret:// URI resolved through internal/secrets. This
-- table is the storage for LocalEncryptedBackend, the v1 implementation of
-- internal/secrets.Backend — that interface is deliberately backend-agnostic
-- (a future Vault-backed implementation would not use this table at all).
--
-- Unlike artifact_versions, secret values are mutable in place (rotation,
-- deletion) — there is no immutable version history here, only a `version`
-- counter used solely to bind each ciphertext to its own AEAD associated
-- data (docs/adr/0005 §AAD) so a stale wrapped_dek/nonce pair can never be
-- mismatched against a newer ciphertext.
CREATE TABLE secret_values (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id          uuid NOT NULL REFERENCES projects (id),
  name                text NOT NULL,
  version             int NOT NULL DEFAULT 1,
  ciphertext          bytea NOT NULL,
  nonce               bytea NOT NULL,
  wrapped_dek         bytea NOT NULL,
  project_key_version int NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, name),
  FOREIGN KEY (project_id, project_key_version) REFERENCES project_keys (project_id, key_version)
);

CREATE INDEX secret_values_project_id_idx ON secret_values (project_id);

ALTER TABLE secret_values ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON secret_values
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- Not immutable like artifact_versions: rotation (UPDATE) and revocation
-- (DELETE) are both legitimate operations on a secret value.
GRANT SELECT, INSERT, UPDATE, DELETE ON secret_values TO cc_app;
