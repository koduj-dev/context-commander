package organizations

// Capability is organization-scoped, deliberately its own type rather than
// reusing internal/auth.Capability — auth.Capability's roleCapabilities
// table is keyed by projects.Role, and docs/adr/0006 requires organization
// authorization to never merge with project-content authorization. Keeping
// the types apart makes that boundary a compile-time fact, not just a
// convention: nothing in internal/auth can accidentally end up deciding an
// organization-admin question, and nothing here can end up deciding a
// project-content one.
type Capability string

const (
	CapOrgRead          Capability = "org.read"
	CapOrgManageMembers Capability = "org.manage_members"
)

var roleCapabilities = map[Role]map[Capability]bool{
	RoleMember: setOf(CapOrgRead),
	RoleAdmin:  setOf(CapOrgRead, CapOrgManageMembers),
	RoleOwner:  setOf(CapOrgRead, CapOrgManageMembers),
}

func setOf(caps ...Capability) map[Capability]bool {
	m := make(map[Capability]bool, len(caps))
	for _, c := range caps {
		m[c] = true
	}
	return m
}

// RoleCan is the pure role -> capability decision, no I/O — mirrors
// internal/auth.RoleCan's split between the pure decision and
// Service.checkOrgCapability's membership lookup.
func RoleCan(role Role, cap Capability) bool {
	return roleCapabilities[role][cap]
}
