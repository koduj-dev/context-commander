package installation_test

import (
	"testing"

	"github.com/koduj-dev/context-commander/internal/installation"
)

func TestRoleCanGrantsAdminEverything(t *testing.T) {
	caps := []installation.Capability{
		installation.CapAdminRead,
		installation.CapUserCreate,
		installation.CapServicePrincipalCreate,
		installation.CapCredentialManage,
		installation.CapSkillManage,
		installation.CapGateManage,
	}
	for _, cap := range caps {
		if !installation.RoleCan(installation.RoleAdmin, cap) {
			t.Errorf("RoleCan(RoleAdmin, %q) = false, want true", cap)
		}
	}
}

func TestRoleCanDeniesUnknownRole(t *testing.T) {
	if installation.RoleCan(installation.Role("NOT_A_ROLE"), installation.CapAdminRead) {
		t.Error("RoleCan for an unknown role = true, want false")
	}
}
