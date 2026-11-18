DROP FUNCTION IF EXISTS cc_delete_user(uuid);
ALTER TABLE users DROP COLUMN IF EXISTS deleted_at;
