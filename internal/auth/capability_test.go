package auth_test

import (
	"testing"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/projects"
)

func TestRoleCanMatrix(t *testing.T) {
	cases := []struct {
		role projects.Role
		cap  auth.Capability
		want bool
	}{
		{projects.RoleReader, auth.CapArtifactRead, true},
		{projects.RoleReader, auth.CapArtifactWrite, false},
		{projects.RoleReader, auth.CapProjectDelete, false},
		{projects.RoleReader, auth.CapMemberManage, false},

		{projects.RoleContributor, auth.CapArtifactWrite, true},
		{projects.RoleContributor, auth.CapArtifactDelete, true},
		{projects.RoleContributor, auth.CapProjectUpdate, false},
		{projects.RoleContributor, auth.CapProjectDelete, false},
		{projects.RoleContributor, auth.CapMemberManage, false},

		{projects.RoleAdmin, auth.CapProjectUpdate, true},
		{projects.RoleAdmin, auth.CapMemberManage, true},
		{projects.RoleAdmin, auth.CapArtifactWrite, true},
		{projects.RoleAdmin, auth.CapProjectDelete, false}, // ownership-level, OWNER only

		{projects.RoleOwner, auth.CapProjectDelete, true},
		{projects.RoleOwner, auth.CapProjectUpdate, true},
		{projects.RoleOwner, auth.CapArtifactWrite, true},

		{"NOT_A_ROLE", auth.CapArtifactRead, false}, // unknown role -> deny, not panic
	}

	for _, tc := range cases {
		got := auth.RoleCan(tc.role, tc.cap)
		if got != tc.want {
			t.Errorf("RoleCan(%s, %s) = %v, want %v", tc.role, tc.cap, got, tc.want)
		}
	}
}

func TestEveryRoleCanReadItsOwnProject(t *testing.T) {
	for _, role := range []projects.Role{
		projects.RoleReader, projects.RoleContributor, projects.RoleAdmin, projects.RoleOwner,
	} {
		if !auth.RoleCan(role, auth.CapProjectRead) {
			t.Errorf("role %s should have %s", role, auth.CapProjectRead)
		}
	}
}
