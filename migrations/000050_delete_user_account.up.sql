-- Deleting a user account (docs/adr/0042): the person's data and access go,
-- what they wrote stays with the projects it belongs to.
--
-- The users and principals rows stay, because every project-scoped record
-- names its author by principal and those references must keep resolving.
-- What identifies the person goes: the email and the name are replaced by a
-- tombstone, and every way to act as them is removed.
ALTER TABLE users ADD COLUMN deleted_at timestamptz;

-- One function, SECURITY DEFINER, because an administrator deleting someone
-- else must reach memberships in projects the administrator cannot see, and
-- because the whole removal has to happen in one statement's transaction or
-- not at all. It checks for itself who may call it.
--
-- SQLSTATEs, beside migration 000047's:
--   CCD01  neither the account's owner nor an installation administrator
--   CCD03  the account is already deleted, or does not exist
--   CCD04  the account is the last OWNER of a project or organization
--   CCD05  the account is the installation's last administrator
CREATE FUNCTION cc_delete_user(p_user_id uuid)
RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  me uuid := cc_current_principal();
  my_user uuid;
  target uuid;
  old_email text;
  blocking text;
BEGIN
  SELECT user_id INTO my_user FROM principals WHERE id = me;
  IF my_user IS NULL OR (my_user <> p_user_id AND NOT EXISTS (SELECT 1 FROM installation_admins WHERE user_id = my_user)) THEN
    RAISE EXCEPTION 'only the account''s owner or an installation administrator can delete it' USING ERRCODE = 'CCD01';
  END IF;

  SELECT email INTO old_email FROM users WHERE id = p_user_id AND deleted_at IS NULL FOR UPDATE;
  IF old_email IS NULL THEN
    RAISE EXCEPTION 'no such account, or it is already deleted' USING ERRCODE = 'CCD03';
  END IF;
  SELECT id INTO target FROM principals WHERE user_id = p_user_id;

  -- Nobody may be left without an OWNER. Projects already scheduled for
  -- deletion do not count: they are going anyway.
  SELECT string_agg(n.slug || '/' || p.slug, ', ' ORDER BY n.slug, p.slug) INTO blocking
  FROM project_members pm
  JOIN projects p ON p.id = pm.project_id
  JOIN namespaces n ON n.id = p.namespace_id
  WHERE pm.principal_id = target AND pm.role = 'OWNER' AND p.deletion_requested_at IS NULL
    AND NOT EXISTS (SELECT 1 FROM project_members o WHERE o.project_id = pm.project_id AND o.role = 'OWNER' AND o.principal_id <> target);
  IF blocking IS NOT NULL THEN
    RAISE EXCEPTION 'the last OWNER of: %; make someone else OWNER or delete those projects first', blocking USING ERRCODE = 'CCD04';
  END IF;
  SELECT string_agg(o.name, ', ' ORDER BY o.name) INTO blocking
  FROM organization_members om JOIN organizations o ON o.id = om.organization_id
  WHERE om.user_id = p_user_id AND om.role = 'OWNER'
    AND NOT EXISTS (SELECT 1 FROM organization_members x WHERE x.organization_id = om.organization_id AND x.role = 'OWNER' AND x.user_id <> p_user_id);
  IF blocking IS NOT NULL THEN
    RAISE EXCEPTION 'the last OWNER of the organization(s): %; make someone else OWNER first', blocking USING ERRCODE = 'CCD04';
  END IF;
  IF EXISTS (SELECT 1 FROM installation_admins WHERE user_id = p_user_id)
     AND NOT EXISTS (SELECT 1 FROM installation_admins a JOIN users u ON u.id = a.user_id
                     WHERE a.user_id <> p_user_id AND u.deleted_at IS NULL) THEN
    RAISE EXCEPTION 'the installation''s last administrator; grant another one first' USING ERRCODE = 'CCD05';
  END IF;

  DELETE FROM work_presence WHERE principal_id = target;
  DELETE FROM project_members WHERE principal_id = target;
  DELETE FROM organization_members WHERE user_id = p_user_id;
  DELETE FROM installation_admins WHERE user_id = p_user_id;
  DELETE FROM user_passwords WHERE user_id = p_user_id;
  DELETE FROM user_identities WHERE user_id = p_user_id;
  UPDATE client_credentials SET revoked_at = now() WHERE principal_id = target AND revoked_at IS NULL;
  -- An invitation still waiting for this address would let whoever signs up
  -- with it next walk into the project.
  UPDATE project_invitations SET revoked_at = now()
   WHERE email = lower(old_email) AND accepted_at IS NULL AND revoked_at IS NULL;
  UPDATE users SET email = 'deleted-' || id || '@deleted.invalid', display_name = 'Deleted user', deleted_at = now()
   WHERE id = p_user_id;
END;
$$;

REVOKE ALL ON FUNCTION cc_delete_user(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_delete_user(uuid) TO cc_app;
