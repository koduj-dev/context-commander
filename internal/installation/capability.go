package installation

// Capability is installation-scoped, deliberately its own type rather than
// reusing internal/auth.Capability or internal/organizations.Capability —
// same reasoning as docs/adr/0006 gives for keeping organization and
// project authorization apart: nothing in internal/auth or
// internal/organizations can accidentally end up deciding an
// installation-admin question, and nothing here can end up deciding a
// project-content or organization-content one.
type Capability string

const (
	// CapAdminRead lists existing installation admins.
	CapAdminRead Capability = "installation.admin_read"
	// CapUserCreate creates a User (+ Principal) outside the CLI-only
	// bootstrap path — the "admin-driven user creation" NEXT.md flags.
	CapUserCreate Capability = "installation.user_create"
	// CapServicePrincipalCreate registers a ServicePrincipal (+ Principal)
	// — agent registration. internal/identity.Store.CreateServicePrincipal
	// existed with zero callers before this package.
	CapServicePrincipalCreate Capability = "installation.service_principal_create"
	// CapCredentialManage issues/lists/revokes a PAT for *any* principal —
	// distinct from the self-only /v1/me/tokens family
	// (adapters/http/tokens.go), which never accepts a principal id from
	// the request body.
	CapCredentialManage Capability = "installation.credential_manage"
	// CapSkillManage writes installation-scope skill_bindings — unblocks
	// internal/skills.ErrUnsupportedScope for a real installation admin.
	CapSkillManage Capability = "installation.skill_manage"
	// CapGateManage writes installation-scope gate_bindings — unblocks
	// internal/gates.ErrUnsupportedScope the same way.
	CapGateManage Capability = "installation.gate_manage"
)

// roleCapabilities holds one role today (RoleAdmin has everything) — kept
// as a table, not a single "IsAdmin -> true" shortcut, so a future narrower
// role slots in as one more map entry, not a rewrite of every call site.
var roleCapabilities = map[Role]map[Capability]bool{
	RoleAdmin: setOf(
		CapAdminRead, CapUserCreate, CapServicePrincipalCreate,
		CapCredentialManage, CapSkillManage, CapGateManage,
	),
}

func setOf(caps ...Capability) map[Capability]bool {
	m := make(map[Capability]bool, len(caps))
	for _, c := range caps {
		m[c] = true
	}
	return m
}

// RoleCan is the pure role -> capability decision, no I/O — mirrors
// internal/auth.RoleCan / internal/organizations.RoleCan.
func RoleCan(role Role, cap Capability) bool {
	return roleCapabilities[role][cap]
}
