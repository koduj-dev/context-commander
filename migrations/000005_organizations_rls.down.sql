DROP POLICY organization_members_delete ON organization_members;
DROP POLICY organization_members_update ON organization_members;
DROP POLICY organization_members_insert_by_member ON organization_members;
DROP POLICY organization_members_insert_bootstrap ON organization_members;
DROP POLICY organization_members_select ON organization_members;
ALTER TABLE organization_members DISABLE ROW LEVEL SECURITY;

DROP POLICY organizations_update ON organizations;
DROP POLICY organizations_insert ON organizations;
DROP POLICY organizations_select ON organizations;
ALTER TABLE organizations DISABLE ROW LEVEL SECURITY;

DROP FUNCTION cc_org_has_no_members(uuid);
DROP FUNCTION cc_has_org_access(text, uuid);
DROP FUNCTION cc_principal_user_id(text);
