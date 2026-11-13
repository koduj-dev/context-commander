-- Project invitations — docs/adr/0030-project-invitations.md.
--
-- Someone who may manage a project's members invites an email address with
-- a role. The invitation is accepted, by the server, the next time a person
-- with that address signs in through the browser: on their first sign-in
-- for a new account, on their next one for an existing account. The inviter
-- is never told whether the address already has an account here.

CREATE TABLE project_invitations (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id           uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  email                text NOT NULL CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 320 AND email LIKE '%_@_%'),
  role                 text NOT NULL CHECK (role IN ('OWNER', 'ADMIN', 'CONTRIBUTOR', 'READER')),
  invited_by_principal uuid NOT NULL REFERENCES principals (id),
  created_at           timestamptz NOT NULL DEFAULT now(),
  expires_at           timestamptz NOT NULL,
  accepted_at          timestamptz,
  accepted_by_principal uuid REFERENCES principals (id),
  revoked_at           timestamptz,
  CHECK (expires_at > created_at)
);
-- One live invitation per address per project: inviting again replaces it
-- (internal/invitations revokes the old one in the same transaction).
CREATE UNIQUE INDEX project_invitations_live_idx ON project_invitations (project_id, email)
  WHERE accepted_at IS NULL AND revoked_at IS NULL;
CREATE INDEX project_invitations_email_idx ON project_invitations (email)
  WHERE accepted_at IS NULL AND revoked_at IS NULL;

ALTER TABLE project_invitations ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON project_invitations
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));
-- No DELETE: a revoked or accepted invitation is a record of who was let in.
GRANT SELECT, INSERT, UPDATE ON project_invitations TO cc_app;

-- Whether an address holds a live invitation anywhere. The sign-in callback
-- asks before anyone is signed in, so RLS would hide every row: SECURITY
-- DEFINER, and it answers a boolean, nothing about which project.
CREATE FUNCTION cc_has_pending_invitation(p_email text)
RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT EXISTS (
    SELECT 1 FROM project_invitations
    WHERE email = lower(p_email) AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()
  );
$$;
REVOKE ALL ON FUNCTION cc_has_pending_invitation(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_has_pending_invitation(text) TO cc_app;

-- Accepts every live invitation for the address of p_principal's user. The
-- address is read here from users, never taken from the caller, so nothing
-- can accept an invitation meant for someone else. A principal that is
-- already a member keeps the role it has: an invitation adds access, it
-- never changes a role someone set since. Returns the projects joined.
CREATE FUNCTION cc_accept_invitations(p_principal uuid)
RETURNS SETOF uuid
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  addr text;
  inv  record;
BEGIN
  SELECT lower(u.email) INTO addr
  FROM principals p JOIN users u ON u.id = p.user_id
  WHERE p.id = p_principal;
  IF addr IS NULL THEN
    RETURN;
  END IF;
  FOR inv IN
    SELECT id, project_id, role FROM project_invitations
    WHERE email = addr AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()
    FOR UPDATE
  LOOP
    UPDATE project_invitations SET accepted_at = now(), accepted_by_principal = p_principal WHERE id = inv.id;
    INSERT INTO project_members (project_id, principal_id, role)
    VALUES (inv.project_id, p_principal, inv.role)
    ON CONFLICT DO NOTHING;
    IF FOUND THEN
      INSERT INTO audit_log (project_id, principal_id, action, resource_type, resource_id, result)
      VALUES (inv.project_id, p_principal, 'member.add', 'project_member', p_principal, 'allow');
      RETURN NEXT inv.project_id;
    END IF;
  END LOOP;
END;
$$;
REVOKE ALL ON FUNCTION cc_accept_invitations(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_accept_invitations(uuid) TO cc_app;
