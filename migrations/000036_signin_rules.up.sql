-- Sign-in rules — docs/adr/0024-sign-in-rules-managed-by-an-administrator.md.
--
-- An installation admin lists the domains and the individual addresses that
-- may sign in through OIDC, and names for each the organizations a newly
-- created account joins. Installation-scoped, so no RLS: the same trust model
-- as installation_admins (migrations/000017) — internal/signin checks the
-- caller is an admin before every read and write (docs/adr/0014).

CREATE TABLE signin_rules (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  -- 'domain' admits every verified address in it; 'address' admits one.
  kind       text NOT NULL CHECK (kind IN ('domain', 'address')),
  -- Lowercased by the service; the CHECK is the backstop that keeps a
  -- mixed-case row from silently never matching.
  value      text NOT NULL CHECK (value = lower(value) AND length(value) BETWEEN 3 AND 320),
  created_by uuid REFERENCES users (id),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (kind, value)
);

CREATE TABLE signin_rule_organizations (
  rule_id         uuid NOT NULL REFERENCES signin_rules (id) ON DELETE CASCADE,
  -- An organization that is deleted takes its grants with it rather than
  -- leaving a rule that points at nothing.
  organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  PRIMARY KEY (rule_id, organization_id)
);

GRANT SELECT, INSERT, DELETE ON signin_rules TO cc_app;
GRANT SELECT, INSERT, DELETE ON signin_rule_organizations TO cc_app;

-- Every organization, for the admin's picker. organizations has RLS that
-- shows a caller only their own; an admin choosing where new accounts land
-- needs the names of all of them. Names only, which the storage list
-- (migrations/000030) already shows an admin. internal/signin calls this
-- only after its admin check.
CREATE FUNCTION cc_signin_organizations()
RETURNS TABLE (id uuid, name text, created_at timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT o.id, o.name, o.created_at FROM organizations o ORDER BY o.name, o.id;
$$;
REVOKE ALL ON FUNCTION cc_signin_organizations() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_signin_organizations() TO cc_app;

-- Makes a just-created user a MEMBER of the organizations its matching rules
-- name. Runs inside provisioning's transaction, which has no principal yet,
-- so organization_members' RLS would refuse the insert: hence SECURITY
-- DEFINER, and hence it decides the match itself from the address rather
-- than taking organization ids from its caller.
--
-- domain_trusted is false when the address alone may not stand for the
-- domain — a Google account outside any Workspace (docs/adr/0024) — and then
-- only address rules grant anything.
CREATE FUNCTION cc_signin_grant_memberships(p_user uuid, p_email text, p_domain_trusted boolean)
RETURNS SETOF uuid
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  addr text := lower(p_email);
  dom  text := split_part(lower(p_email), '@', 2);
  org  uuid;
BEGIN
  FOR org IN
    SELECT DISTINCT ro.organization_id
    FROM signin_rules r
    JOIN signin_rule_organizations ro ON ro.rule_id = r.id
    WHERE (r.kind = 'address' AND r.value = addr)
       OR (p_domain_trusted AND r.kind = 'domain' AND r.value = dom)
  LOOP
    INSERT INTO organization_members (organization_id, user_id, role)
    VALUES (org, p_user, 'MEMBER')
    ON CONFLICT DO NOTHING;
    IF FOUND THEN
      INSERT INTO audit_log (organization_id, action, resource_type, resource_id, result)
      VALUES (org, 'organization.member.add', 'organization_member', p_user, 'allow');
      RETURN NEXT org;
    END IF;
  END LOOP;
END;
$$;
REVOKE ALL ON FUNCTION cc_signin_grant_memberships(uuid, text, boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_signin_grant_memberships(uuid, text, boolean) TO cc_app;
