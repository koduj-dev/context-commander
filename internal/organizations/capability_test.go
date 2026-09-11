package organizations_test

import (
	"testing"

	"github.com/koduj-dev/context-commander/internal/organizations"
)

// TestRoleCanMatrix mirrors internal/auth's capability matrix test for the
// organization-scoped table. The load-bearing row is ADMIN vs.
// CapOrgGrantOwner: ADMIN manages members, but promoting someone to OWNER
// is ownership transfer, not member management.
func TestRoleCanMatrix(t *testing.T) {
	cases := []struct {
		role organizations.Role
		cap  organizations.Capability
		want bool
	}{
		{organizations.RoleMember, organizations.CapOrgRead, true},
		{organizations.RoleMember, organizations.CapOrgManageMembers, false},
		{organizations.RoleMember, organizations.CapOrgGrantOwner, false},

		{organizations.RoleAdmin, organizations.CapOrgRead, true},
		{organizations.RoleAdmin, organizations.CapOrgManageMembers, true},
		{organizations.RoleAdmin, organizations.CapOrgGrantOwner, false},

		{organizations.RoleOwner, organizations.CapOrgRead, true},
		{organizations.RoleOwner, organizations.CapOrgManageMembers, true},
		{organizations.RoleOwner, organizations.CapOrgGrantOwner, true},

		{"NOT_A_ROLE", organizations.CapOrgRead, false}, // unknown role -> deny, not panic
	}

	for _, tc := range cases {
		if got := organizations.RoleCan(tc.role, tc.cap); got != tc.want {
			t.Errorf("RoleCan(%s, %s) = %v, want %v", tc.role, tc.cap, got, tc.want)
		}
	}
}
