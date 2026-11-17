-- Trusted skill packages (docs/adr/0040).
--
-- A skill version is trusted when it is signed by a key an installation
-- administrator registered, or approved by a person allowed to approve it.
-- skill_versions stays immutable: a signature is checked and recorded when
-- the version is inserted, and an approval is a row of its own.

CREATE TABLE skill_signers (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name       text NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 100),
  -- An Ed25519 public key.
  public_key bytea NOT NULL UNIQUE CHECK (length(public_key) = 32),
  added_by   uuid REFERENCES principals (id),
  created_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz
);
GRANT SELECT, INSERT, UPDATE ON skill_signers TO cc_app;

ALTER TABLE skill_versions ADD COLUMN signed_by uuid REFERENCES skill_signers (id);
ALTER TABLE skill_versions ADD CONSTRAINT skill_versions_trust_status_check
  CHECK (trust_status IN ('unverified', 'signed'));
ALTER TABLE skill_versions ADD CONSTRAINT skill_versions_signed_consistency
  CHECK ((trust_status = 'signed') = (signed_by IS NOT NULL AND signature IS NOT NULL));

-- One approval per version; approving is not undone, a bad version is
-- superseded by a new one.
CREATE TABLE skill_version_approvals (
  skill_version_id uuid PRIMARY KEY REFERENCES skill_versions (id),
  approved_by      uuid NOT NULL REFERENCES principals (id),
  approved_at      timestamptz NOT NULL DEFAULT now(),
  note             text CHECK (length(note) <= 2000)
);
GRANT SELECT, INSERT ON skill_version_approvals TO cc_app;
