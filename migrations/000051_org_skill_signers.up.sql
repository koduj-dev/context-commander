-- Signing keys per organization (docs/adr/0040, amended 2026-11-20).
--
-- A key with no organization is trusted installation-wide, as before. A key
-- with one is trusted only for that organization's skills and for skills of
-- projects whose namespace belongs to it. A public key still belongs to one
-- signer only, so signed_by always names a single owner; names are unique
-- within an organization, and among installation keys.

ALTER TABLE skill_signers ADD COLUMN organization_id uuid REFERENCES organizations (id);
ALTER TABLE skill_signers DROP CONSTRAINT skill_signers_name_key;
CREATE UNIQUE INDEX skill_signers_installation_name_idx ON skill_signers (name) WHERE organization_id IS NULL;
CREATE UNIQUE INDEX skill_signers_organization_name_idx ON skill_signers (organization_id, name) WHERE organization_id IS NOT NULL;
