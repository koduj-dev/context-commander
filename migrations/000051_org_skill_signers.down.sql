-- Organization keys cannot become installation keys silently: going back
-- revokes nothing and trusts nothing new, it refuses while any exist.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM skill_signers WHERE organization_id IS NOT NULL) THEN
    RAISE EXCEPTION 'organization signing keys exist; remove them before migrating down';
  END IF;
END $$;
DROP INDEX IF EXISTS skill_signers_organization_name_idx;
DROP INDEX IF EXISTS skill_signers_installation_name_idx;
ALTER TABLE skill_signers ADD CONSTRAINT skill_signers_name_key UNIQUE (name);
ALTER TABLE skill_signers DROP COLUMN organization_id;
