-- The renames the up migration made are not undone: the original names are
-- not recorded anywhere, and restoring a duplicate is not a recovery.
DROP INDEX service_principals_registrar_name_key;
