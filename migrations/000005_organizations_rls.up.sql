-- Closes a gap found while finishing internal/organizations: migration
-- 000001 created `organizations`/`organization_members` with no RLS
-- policy at all — any authenticated principal could read/write any
-- organization's membership regardless of whether they belonged to it.
--
-- Mirrors 000004's project_members bootstrap fix exactly (same class of
-- bug, same fix): organization_members is keyed by user_id, not
-- principal_id (docs/adr/0006 — org membership is a User concept, not a
-- Principal one; service principals are never organization members), so
-- resolving "does the calling principal belong to this org" needs one
-- extra hop through `principals` first.

-- Resolves the calling principal (identified by RLS's
-- app.current_principal_id GUC, as text — see 000001's
-- cc_has_project_access for why text, not uuid) to the user_id it acts for,
-- or NULL if the principal doesn't exist or is a service principal (which
-- can never be an organization member).
CREATE FUNCTION cc_principal_user_id(p_principal_id_text text)
RETURNS uuid
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  p_principal_id uuid;
  p_user_id uuid;
BEGIN
  IF p_principal_id_text IS NULL OR p_principal_id_text = '' THEN
    RETURN NULL;
  END IF;

  BEGIN
    p_principal_id := p_principal_id_text::uuid;
  EXCEPTION WHEN invalid_text_representation THEN
    RETURN NULL;
  END;

  SELECT user_id INTO p_user_id FROM principals WHERE id = p_principal_id AND kind = 'user';
  RETURN p_user_id;
END;
$$;

REVOKE ALL ON FUNCTION cc_principal_user_id(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_principal_user_id(text) TO cc_app;

CREATE FUNCTION cc_has_org_access(p_principal_id_text text, p_org_id uuid)
RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT EXISTS (
    SELECT 1 FROM organization_members
    WHERE organization_id = p_org_id AND user_id = cc_principal_user_id(p_principal_id_text)
  );
$$;

REVOKE ALL ON FUNCTION cc_has_org_access(text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_has_org_access(text, uuid) TO cc_app;

-- Same non-recursive-RLS trap as 000004's cc_project_has_no_members: a
-- plain NOT EXISTS subquery here would itself be filtered by
-- organization_members_select's own RLS, making "no members" look true for
-- an org that already has members an attacker simply isn't allowed to see.
-- A SECURITY DEFINER helper sidesteps that by reading the real table state.
CREATE FUNCTION cc_org_has_no_members(p_org_id uuid)
RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT NOT EXISTS (SELECT 1 FROM organization_members WHERE organization_id = p_org_id);
$$;

REVOKE ALL ON FUNCTION cc_org_has_no_members(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_org_has_no_members(uuid) TO cc_app;

-- organizations: INSERT stays unconditional (nobody can be a member of an
-- organization before it exists), SELECT/UPDATE require membership.
ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;

CREATE POLICY organizations_select ON organizations FOR SELECT
  USING (cc_has_org_access(current_setting('app.current_principal_id', true), id));

CREATE POLICY organizations_insert ON organizations FOR INSERT
  WITH CHECK (true);

CREATE POLICY organizations_update ON organizations FOR UPDATE
  USING (cc_has_org_access(current_setting('app.current_principal_id', true), id));

-- organization_members: same bootstrap-then-member-managed shape as
-- project_members (000004).
ALTER TABLE organization_members ENABLE ROW LEVEL SECURITY;

CREATE POLICY organization_members_select ON organization_members FOR SELECT
  USING (cc_has_org_access(current_setting('app.current_principal_id', true), organization_id));

CREATE POLICY organization_members_insert_bootstrap ON organization_members FOR INSERT
  WITH CHECK (
    user_id = cc_principal_user_id(current_setting('app.current_principal_id', true))
    AND cc_org_has_no_members(organization_id)
  );

CREATE POLICY organization_members_insert_by_member ON organization_members FOR INSERT
  WITH CHECK (cc_has_org_access(current_setting('app.current_principal_id', true), organization_id));

CREATE POLICY organization_members_update ON organization_members FOR UPDATE
  USING (cc_has_org_access(current_setting('app.current_principal_id', true), organization_id));

CREATE POLICY organization_members_delete ON organization_members FOR DELETE
  USING (cc_has_org_access(current_setting('app.current_principal_id', true), organization_id));
