package auth

import "github.com/koduj-dev/context-commander/internal/projects"

// Capability is a fine-grained internal permission. Roles map to a set of
// capabilities so future policy can get more granular without renaming the
// four user-facing roles — see docs/architecture-cookbook.md §7.1.
type Capability string

const (
	CapProjectRead   Capability = "project.read"
	CapProjectUpdate Capability = "project.update"
	CapProjectDelete Capability = "project.delete"

	CapMemberRead   Capability = "member.read"
	CapMemberManage Capability = "member.manage"

	CapArtifactRead   Capability = "artifact.read"
	CapArtifactWrite  Capability = "artifact.write"
	CapArtifactDelete Capability = "artifact.delete"

	CapSkillRead    Capability = "skill.read"
	CapSkillWrite   Capability = "skill.write"
	CapSkillInstall Capability = "skill.install"
	// CapSkillPublish is a project-anchored capability: cookbook §12.2
	// frames publishing a new Skill/SkillVersion as something an agent
	// does while working on a specific project ("this turns project
	// knowledge discovered during work into reusable context"), so it is
	// checked against that project, not against any org/installation
	// admin concept this codebase does not otherwise have.
	CapSkillPublish Capability = "skill.publish"
	// CapSkillOverride guards internal/skills.Service.Override — creating
	// a project binding that shadows an organization/installation one
	// (docs/adr/0012).
	CapSkillOverride Capability = "skill.override"

	// CapGateManage guards internal/gates gate_bindings mutations
	// (bind/unbind/configure). Evaluating gates on a hook is not itself a
	// capability-gated action by the calling agent.
	CapGateManage Capability = "gate.manage"

	CapEventRead    Capability = "event.read"
	CapEventPublish Capability = "event.publish"

	CapIntegrationRead   Capability = "integration.read"
	CapIntegrationManage Capability = "integration.manage"

	// CapAuditRead is deliberately ADMIN/OWNER-only (not granted to
	// CONTRIBUTOR/READER) — the audit trail can reveal who did what to
	// whom, which is more sensitive than the content it describes.
	CapAuditRead Capability = "audit.read"

	// CapSecretRead/CapSecretWrite are deliberately withheld from READER:
	// secrets are live operational credentials an integration needs to do
	// work, not project content — a read-only observer role has no reason
	// to be able to resolve a plaintext token. CONTRIBUTOR gets both,
	// same tier as artifact/skill read+write, because using a secret
	// (e.g. calling an external API with it) is ordinary contributor work
	// (docs/architecture-cookbook.md §15).
	CapSecretRead  Capability = "secret.read"
	CapSecretWrite Capability = "secret.write"

	CapHandoffRead  Capability = "handoff.read"
	CapHandoffWrite Capability = "handoff.write"

	CapHistoryRead  Capability = "history.read"
	CapHistoryWrite Capability = "history.write"
)

// roleCapabilities is the single source of truth for what each role can do.
// OWNER implicitly has everything; the other roles are listed explicitly so
// a reviewer can see the full grant at a glance instead of reasoning about
// exceptions.
var roleCapabilities = map[projects.Role]map[Capability]bool{
	projects.RoleReader: setOf(
		CapProjectRead, CapMemberRead, CapArtifactRead, CapSkillRead,
		CapEventRead, CapIntegrationRead,
		CapHandoffRead, CapHistoryRead,
	),
	projects.RoleContributor: setOf(
		CapProjectRead, CapMemberRead, CapArtifactRead, CapSkillRead,
		CapEventRead, CapIntegrationRead,
		CapArtifactWrite, CapArtifactDelete, CapSkillWrite, CapSkillInstall,
		CapEventPublish,
		CapSecretRead, CapSecretWrite,
		CapSkillPublish,
		CapHandoffRead, CapHandoffWrite, CapHistoryRead, CapHistoryWrite,
	),
	projects.RoleAdmin: setOf(
		CapProjectRead, CapMemberRead, CapArtifactRead, CapSkillRead,
		CapEventRead, CapIntegrationRead,
		CapArtifactWrite, CapArtifactDelete, CapSkillWrite, CapSkillInstall,
		CapEventPublish,
		CapProjectUpdate, CapMemberManage, CapIntegrationManage,
		CapAuditRead,
		CapSecretRead, CapSecretWrite,
		CapSkillPublish, CapSkillOverride, CapGateManage,
		CapHandoffRead, CapHandoffWrite, CapHistoryRead, CapHistoryWrite,
		// No CapProjectDelete — ownership-level action, OWNER only
		// (docs/architecture-cookbook.md §7.1: "no ownership transfer
		// unless explicitly permitted").
	),
	projects.RoleOwner: setOf(
		CapProjectRead, CapMemberRead, CapArtifactRead, CapSkillRead,
		CapEventRead, CapIntegrationRead,
		CapArtifactWrite, CapArtifactDelete, CapSkillWrite, CapSkillInstall,
		CapEventPublish,
		CapProjectUpdate, CapMemberManage, CapIntegrationManage,
		CapAuditRead,
		CapSecretRead, CapSecretWrite,
		CapSkillPublish, CapSkillOverride, CapGateManage,
		CapHandoffRead, CapHandoffWrite, CapHistoryRead, CapHistoryWrite,
		CapProjectDelete,
	),
}

func setOf(caps ...Capability) map[Capability]bool {
	m := make(map[Capability]bool, len(caps))
	for _, c := range caps {
		m[c] = true
	}
	return m
}

// RoleCan is the pure role -> capability decision, with no I/O — easy to
// unit test exhaustively. AuthzService.Can additionally resolves which role
// (if any) a principal holds on a project before calling this.
func RoleCan(role projects.Role, cap Capability) bool {
	return roleCapabilities[role][cap]
}
