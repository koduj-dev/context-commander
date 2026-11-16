-- Deleting a project, and holding one (docs/adr/0036).
--
-- An OWNER asks for deletion; from that moment the project is gone for
-- everyone: cc_has_project_access, which every project-scoped policy calls,
-- answers false for it, so it disappears from every list and every read at
-- once, without a change to any of them. Its OWNERs can still see that it is
-- pending and cancel it until purge_after; after that, `ccd purge` removes
-- it for good. A project under legal hold cannot be scheduled for deletion,
-- and a hold can only be set by an operator.

ALTER TABLE projects
  ADD COLUMN deletion_requested_at timestamptz,
  ADD COLUMN deletion_requested_by uuid REFERENCES principals (id),
  ADD COLUMN purge_after           timestamptz,
  ADD COLUMN legal_hold            boolean NOT NULL DEFAULT false,
  ADD COLUMN legal_hold_reason     text,
  ADD CONSTRAINT projects_deletion_consistency CHECK (
    (deletion_requested_at IS NULL) = (purge_after IS NULL)
    AND (deletion_requested_at IS NULL) = (deletion_requested_by IS NULL)),
  ADD CONSTRAINT projects_hold_blocks_deletion CHECK (NOT (legal_hold AND deletion_requested_at IS NOT NULL));

-- Nothing in the application updates a project row, and these columns must
-- not be writable by it: deletion goes through the functions below, and a
-- hold through the operator's connection.
REVOKE UPDATE ON projects FROM cc_app;

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
    SELECT 1 FROM project_members pm JOIN projects p ON p.id = pm.project_id
    WHERE pm.principal_id = p_principal_id AND pm.project_id = p_project_id
      AND p.deletion_requested_at IS NULL
  );
END;
$$;

-- The caller, as WithPrincipal set it; NULL outside one.
CREATE FUNCTION cc_current_principal() RETURNS uuid
LANGUAGE plpgsql STABLE
AS $$
BEGIN
  RETURN NULLIF(current_setting('app.current_principal_id', true), '')::uuid;
EXCEPTION WHEN invalid_text_representation THEN
  RETURN NULL;
END;
$$;

-- Errors carry their own SQLSTATE so the application can name them:
--   CCD01  the caller is not an OWNER of the project
--   CCD02  the project is under legal hold
--   CCD03  the project is not in the state the call needs
CREATE FUNCTION cc_request_project_deletion(p_project_id uuid, p_grace interval)
RETURNS timestamptz
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  me uuid := cc_current_principal();
  held boolean;
  pending timestamptz;
  out_purge timestamptz;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM project_members WHERE project_id = p_project_id AND principal_id = me AND role = 'OWNER') THEN
    RAISE EXCEPTION 'only an OWNER can delete a project' USING ERRCODE = 'CCD01';
  END IF;
  SELECT legal_hold, deletion_requested_at INTO held, pending FROM projects WHERE id = p_project_id FOR UPDATE;
  IF held THEN
    RAISE EXCEPTION 'the project is under legal hold' USING ERRCODE = 'CCD02';
  END IF;
  IF pending IS NOT NULL THEN
    RAISE EXCEPTION 'the project is already scheduled for deletion' USING ERRCODE = 'CCD03';
  END IF;
  UPDATE projects SET deletion_requested_at = now(), deletion_requested_by = me, purge_after = now() + p_grace
   WHERE id = p_project_id RETURNING purge_after INTO out_purge;
  RETURN out_purge;
END;
$$;

CREATE FUNCTION cc_cancel_project_deletion(p_project_id uuid)
RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  me uuid := cc_current_principal();
BEGIN
  IF NOT EXISTS (SELECT 1 FROM project_members WHERE project_id = p_project_id AND principal_id = me AND role = 'OWNER') THEN
    RAISE EXCEPTION 'only an OWNER can cancel a deletion' USING ERRCODE = 'CCD01';
  END IF;
  UPDATE projects SET deletion_requested_at = NULL, deletion_requested_by = NULL, purge_after = NULL
   WHERE id = p_project_id AND deletion_requested_at IS NOT NULL AND purge_after > now();
  IF NOT FOUND THEN
    RAISE EXCEPTION 'the project is not scheduled for deletion, or its grace period is over' USING ERRCODE = 'CCD03';
  END IF;
END;
$$;

-- The caller's projects that wait for purge: only those it OWNs, and only
-- what a cancel button needs. Row-level security hides these projects
-- otherwise, which is the point.
CREATE FUNCTION cc_pending_project_deletions()
RETURNS TABLE (id uuid, namespace_slug text, slug text, name text,
               requested_at timestamptz, requested_by uuid, purge_after timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT p.id, n.slug, p.slug, p.name, p.deletion_requested_at, p.deletion_requested_by, p.purge_after
  FROM projects p
  JOIN namespaces n ON n.id = p.namespace_id
  JOIN project_members pm ON pm.project_id = p.id
  WHERE pm.principal_id = cc_current_principal() AND pm.role = 'OWNER' AND p.deletion_requested_at IS NOT NULL
  ORDER BY p.purge_after
$$;

REVOKE ALL ON FUNCTION cc_current_principal() FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_request_project_deletion(uuid, interval) FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_cancel_project_deletion(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_pending_project_deletions() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_current_principal() TO cc_app;
GRANT EXECUTE ON FUNCTION cc_request_project_deletion(uuid, interval) TO cc_app;
GRANT EXECUTE ON FUNCTION cc_cancel_project_deletion(uuid) TO cc_app;
GRANT EXECUTE ON FUNCTION cc_pending_project_deletions() TO cc_app;

-- What `ccd purge` removed, kept after everything else is gone: which
-- project, who asked and when. Names and paths only, never content. Not
-- readable by the application; an operator reads it.
CREATE TABLE project_purges (
  project_id   uuid PRIMARY KEY,
  path         text NOT NULL,
  name         text NOT NULL,
  requested_by uuid,
  requested_at timestamptz,
  purged_at    timestamptz NOT NULL DEFAULT now(),
  reason       text NOT NULL
);
