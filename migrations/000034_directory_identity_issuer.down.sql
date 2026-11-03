REVOKE UPDATE (issuer) ON user_identities FROM cc_app;
ALTER TABLE user_identities DROP COLUMN IF EXISTS issuer;
