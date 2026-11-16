DROP TABLE IF EXISTS project_purges;
DROP FUNCTION IF EXISTS cc_pending_project_deletions();
DROP FUNCTION IF EXISTS cc_cancel_project_deletion(uuid);
DROP FUNCTION IF EXISTS cc_request_project_deletion(uuid, interval);
DROP FUNCTION IF EXISTS cc_current_principal();

CREATE OR REPLACE FUNCTION cc_has_project_access(p_principal_id_text text, p_project_id uuid)
RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  p_principal_id uuid;
BEGIN
  IF p_principal_id_text IS NULL OR p_principal_id_text = '' THEN
    RETURN false;
  END IF;
  BEGIN
    p_principal_id := p_principal_id_text::uuid;
  EXCEPTION WHEN invalid_text_representation THEN
    RETURN false;
  END;
  RETURN EXISTS (
    SELECT 1 FROM project_members
    WHERE principal_id = p_principal_id AND project_id = p_project_id
  );
END;
$$;

GRANT UPDATE ON projects TO cc_app;
ALTER TABLE projects
  DROP CONSTRAINT IF EXISTS projects_hold_blocks_deletion,
  DROP CONSTRAINT IF EXISTS projects_deletion_consistency,
  DROP COLUMN IF EXISTS legal_hold_reason,
  DROP COLUMN IF EXISTS legal_hold,
  DROP COLUMN IF EXISTS purge_after,
  DROP COLUMN IF EXISTS deletion_requested_by,
  DROP COLUMN IF EXISTS deletion_requested_at;
