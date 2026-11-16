-- Which root keys the project keys are wrapped under, for `ccd serve` to
-- refuse to start with the wrong CC_ROOT_KEY.
--
-- project_keys is project-scoped under RLS, and the server reads it as cc_app
-- with no principal at startup, so it sees no rows. Hence SECURITY DEFINER,
-- like cc_installation_stats(), and the narrowest result that answers the
-- question: each distinct root_key_ref with how many project keys use it.
--
-- root_key_ref is a fingerprint, "env:" and the first 16 hex characters of
-- the SHA-256 of the key (internal/crypto/rootkey.go). It identifies a key
-- without revealing it; backups already carry the same fingerprints in
-- root-key-refs.txt. No project id, no wrapped key, no row is returned.
CREATE FUNCTION cc_root_key_refs()
RETURNS TABLE (root_key_ref text, project_keys bigint)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT root_key_ref, count(*) FROM project_keys GROUP BY root_key_ref ORDER BY root_key_ref
$$;

REVOKE ALL ON FUNCTION cc_root_key_refs() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_root_key_refs() TO cc_app;
