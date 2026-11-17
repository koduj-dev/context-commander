DROP TABLE IF EXISTS skill_version_approvals;
ALTER TABLE skill_versions DROP CONSTRAINT IF EXISTS skill_versions_signed_consistency;
ALTER TABLE skill_versions DROP CONSTRAINT IF EXISTS skill_versions_trust_status_check;
ALTER TABLE skill_versions DROP COLUMN IF EXISTS signed_by;
DROP TABLE IF EXISTS skill_signers;
