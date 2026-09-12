// Package skills defines SkillVersion (immutable package) and SkillBinding
// (installation/pinning) — see
// docs/adr/0012-skill-registry-supply-chain.md.
package skills

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Scope is where a SkillBinding (or GateBinding, internal/gates) applies —
// see docs/architecture-cookbook.md §12.1: scopes compose, they do not
// replace one another.
type Scope string

const (
	ScopeInstallation Scope = "installation"
	ScopeOrganization Scope = "organization"
	ScopeProject      Scope = "project"
)

var (
	ErrNotFound  = errors.New("skills: not found")
	ErrForbidden = errors.New("skills: forbidden")

	// ErrNotOverridable is returned by Service.Override when the
	// organization/installation binding it would shadow has
	// overridable = false (docs/adr/0012's "guards against e.g. a project
	// quietly shadowing a mandated security-review skill").
	ErrNotOverridable = errors.New("skills: shadowed binding is not overridable")

	// ErrScopeRefRequired is returned by Service.Install when scope is
	// "organization" or "project" but no scope_ref was given — a client
	// input mistake, not a server fault, so it must map to 400 rather than
	// the default 500 an unwrapped fmt.Errorf would fall through to.
	ErrScopeRefRequired = errors.New("skills: organization/project scope requires scope_ref")

	// ErrSkillVersionMismatch is returned by Service.Install/Override when
	// skillVersionID does not belong to skillID — a client input mistake
	// (or a supply-chain-relevant one), not a server fault, so it must map
	// to 400 rather than the default 500 an unwrapped fmt.Errorf/database
	// constraint violation would fall through to. The database also
	// enforces this via skill_versions' composite (id, skill_id) key
	// (migrations/000014) as defense in depth.
	ErrSkillVersionMismatch = errors.New("skills: skill_version_id does not belong to skill_id")
)

// Skill is the stable identity a name/author/origin refers to across every
// SkillVersion ever published under it. ADR 0012 only names SkillVersion
// and SkillBinding as the two top-level types; this row is what a
// SkillVersion's skill_id actually points at — without it, "one skill, many
// versions" has nowhere to hang the name.
type Skill struct {
	ID        uuid.UUID
	Name      string
	Author    string
	Origin    string
	CreatedAt time.Time
}

// FileManifestEntry describes one file inside a SkillVersion's packaged
// content — informational only in v1 (the package itself is one opaque
// ObjectStore blob; nothing extracts individual files from it yet).
type FileManifestEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Hash string `json:"hash"`
}

// SkillVersion is immutable once inserted (ADR 0012: "skill.publish creates
// a new SkillVersion. skill.update ... has no code path that can mutate a
// published SkillVersion's content"). Content lives in storage.ObjectStore
// under StorageKey, unencrypted — see docs/gotchas.md for why.
type SkillVersion struct {
	ID                   uuid.UUID
	SkillID              uuid.UUID
	Version              int
	Checksum             string
	StorageKey           string
	Files                []FileManifestEntry
	DeclaredCapabilities []string
	TrustStatus          string
	Signature            []byte
	CreatedAt            time.Time

	// Provenance (migrations/000020). PublishedInProject is what
	// Service.GetVersion authorizes reads against; nil means a row
	// predating that migration, read as installation-wide. PublishedBy is
	// trust metadata — ADR 0012 frames this as a distribution/trust
	// platform, and "who published this" belongs with the checksum and the
	// signature.
	PublishedInProject *uuid.UUID
	PublishedBy        *uuid.UUID
}

// SkillBinding is what "project X uses skill Y" (or organization/
// installation) actually means — ADR 0012. Always pinned to one concrete
// SkillVersionID in v1: there is no version-constraint resolver yet, so
// Pinned is carried for forward compatibility but every resolution goes
// through SkillVersionID regardless of its value.
type SkillBinding struct {
	ID             uuid.UUID
	Scope          Scope
	ScopeRef       *uuid.UUID
	SkillID        uuid.UUID
	SkillVersionID uuid.UUID
	Pinned         bool
	Managed        bool
	Overridable    bool
	InstalledBy    *uuid.UUID
	InstalledAt    time.Time
}
