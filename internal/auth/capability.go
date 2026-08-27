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

	CapEventRead    Capability = "event.read"
	CapEventPublish Capability = "event.publish"

	CapIntegrationRead   Capability = "integration.read"
	CapIntegrationManage Capability = "integration.manage"
)

// roleCapabilities is the single source of truth for what each role can do.
// OWNER implicitly has everything; the other roles are listed explicitly so
// a reviewer can see the full grant at a glance instead of reasoning about
// exceptions.
var roleCapabilities = map[projects.Role]map[Capability]bool{
	projects.RoleReader: setOf(
		CapProjectRead, CapMemberRead, CapArtifactRead, CapSkillRead,
		CapEventRead, CapIntegrationRead,
	),
	projects.RoleContributor: setOf(
		CapProjectRead, CapMemberRead, CapArtifactRead, CapSkillRead,
		CapEventRead, CapIntegrationRead,
		CapArtifactWrite, CapArtifactDelete, CapSkillWrite, CapSkillInstall,
		CapEventPublish,
	),
	projects.RoleAdmin: setOf(
		CapProjectRead, CapMemberRead, CapArtifactRead, CapSkillRead,
		CapEventRead, CapIntegrationRead,
		CapArtifactWrite, CapArtifactDelete, CapSkillWrite, CapSkillInstall,
		CapEventPublish,
		CapProjectUpdate, CapMemberManage, CapIntegrationManage,
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
