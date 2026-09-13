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
	// CapOrgGrantOwner is OWNER-only, and split out of
	// CapOrgManageMembers for the same reason
	// internal/auth.CapMemberGrantOwner is split out of
	// CapMemberManage: otherwise an ADMIN can add an account they
	// control as OWNER and inherit ownership through it, which is an
	// escalation dressed up as ordinary member management.
	CapOrgGrantOwner Capability = "org.grant_owner"

	// CapOrgAuditRead mirrors auth.CapAuditRead and is withheld from
	// RoleMember for the same reason it is withheld from a project's
	// CONTRIBUTOR/READER: the audit trail reveals who did what to whom,
	// which is more sensitive than the membership it describes. On a
	// one-person installation the distinction costs nothing — the only
	// member is an OWNER. On a real organization it is the difference
	// between "everyone can see that Alice removed Bob" and "the people
	// who run the organization can".
	CapOrgAuditRead Capability = "org.audit_read"
)

var roleCapabilities = map[Role]map[Capability]bool{
	RoleMember: setOf(CapOrgRead),
	// ADMIN manages members but cannot mint another OWNER — the absence
	// of CapOrgGrantOwner here is the whole point of the capability.
	RoleAdmin: setOf(CapOrgRead, CapOrgManageMembers, CapOrgAuditRead),
	RoleOwner: setOf(CapOrgRead, CapOrgManageMembers, CapOrgGrantOwner, CapOrgAuditRead),
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
