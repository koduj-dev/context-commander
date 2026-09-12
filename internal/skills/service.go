package skills

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/storage"
)

// Service is skills' authorization chokepoint (docs/adr/0014). Every
// mutation needs an anchor project (projectID) to check a capability
// against, even for organization/installation-scope bindings — Publish
// (cookbook §12.2) is framed as something an agent does while working on a
// specific project, so anchoring every mutation to a project the caller
// actually holds a role on is the v1 model. installation-scope bindings are
// the one case that isn't anchored to a project at all: checkScopeTarget
// checks installation.RoleAdmin via installationAdmins instead — see
// docs/adr/0017.
type Service struct {
	pool               *db.Pool
	authz              *auth.AuthzService
	orgs               *organizations.Store
	projects           *projects.Store
	store              storage.ObjectStore
	installationAdmins *installation.Store
}

func NewService(
	pool *db.Pool, authz *auth.AuthzService, orgs *organizations.Store, projectsStore *projects.Store,
	store storage.ObjectStore, installationAdmins *installation.Store,
) *Service {
	return &Service{pool: pool, authz: authz, orgs: orgs, projects: projectsStore, store: store, installationAdmins: installationAdmins}
}

// resolveProjectOrgID returns projectID's owning organization, or nil if it
// lives in a personal namespace. Callers must derive the organization this
// way rather than accept it as adapter-supplied input — an org ID coming
// from the client is not authority-bearing, and trusting it let a caller
// drop the real owning organization from an override/effective-resolution
// check (docs/adr/0012 gap fixed alongside migrations/000013).
func (s *Service) resolveProjectOrgID(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) (*uuid.UUID, error) {
	var orgID *uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT n.organization_id FROM projects p JOIN namespaces n ON n.id = p.namespace_id WHERE p.id = $1`,
		projectID,
	).Scan(&orgID)
	if err != nil {
		return nil, fmt.Errorf("resolve project organization: %w", err)
	}
	return orgID, nil
}

// checkSkillVersionBelongsTo verifies skillVersionID is actually a version
// of skillID before a binding is allowed to pair them — Install/Override
// otherwise accept the two UUIDs independently, letting a binding claim
// skill A's identity while pinning skill B's content. The database also
// enforces this (migrations/000014); this check exists to return
// ErrSkillVersionMismatch instead of an opaque constraint-violation error.
func (s *Service) checkSkillVersionBelongsTo(ctx context.Context, tx pgx.Tx, skillID, skillVersionID uuid.UUID) error {
	var exists bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM skill_versions WHERE id = $1 AND skill_id = $2)`,
		skillVersionID, skillID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("verify skill_version_id belongs to skill_id: %w", err)
	}
	if !exists {
		return ErrSkillVersionMismatch
	}
	return nil
}

func (s *Service) checkCapability(ctx context.Context, principalID, projectID uuid.UUID, cap auth.Capability) error {
	allowed, err := s.authz.Can(ctx, principalID, projectID, cap)
	if err != nil {
		return fmt.Errorf("skills: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// checkOverrideTarget verifies scope/scopeRef names something the caller
// can actually bind into: for ScopeOrganization, the caller (identified by
// userID — organization_members is keyed by user, docs/adr/0006) must hold
// CapOrgManageMembers on that organization. ScopeProject must be
// independently checked against scopeRef itself — projectID (the anchor
// used for the earlier capability check) and scopeRef are caller-supplied
// and not required to be equal, so the anchor check alone does not prove
// the caller may install into scopeRef. ScopeInstallation requires userID
// to hold installation.RoleAdmin (docs/adr/0017) — scopeRef is always nil
// for it (migrations/000007's CHECK), so there is nothing else to verify.
func (s *Service) checkScopeTarget(ctx context.Context, tx pgx.Tx, principalID, userID uuid.UUID, scope Scope, scopeRef *uuid.UUID) error {
	switch scope {
	case ScopeInstallation:
		isAdmin, err := s.installationAdmins.IsAdmin(ctx, tx, userID)
		if err != nil {
			return fmt.Errorf("skills: check installation admin: %w", err)
		}
		if !isAdmin {
			return ErrForbidden
		}
		return nil
	case ScopeOrganization:
		if scopeRef == nil {
			return ErrScopeRefRequired
		}
		m, err := s.orgs.GetMembership(ctx, tx, *scopeRef, userID)
		if err != nil {
			return ErrForbidden
		}
		if !organizations.RoleCan(m.Role, organizations.CapOrgManageMembers) {
			return ErrForbidden
		}
		return nil
	case ScopeProject:
		if scopeRef == nil {
			return ErrScopeRefRequired
		}
		// Uses projects.Store directly against the already-open tx, not
		// s.checkCapability (which opens its own transaction via
		// AuthzService.Can) — nesting a second pool.Begin inside this
		// transaction's callback risks pool exhaustion/deadlock under a
		// small connection pool.
		m, err := s.projects.GetMembership(ctx, tx, *scopeRef, principalID)
		if err != nil {
			return ErrForbidden
		}
		if !auth.RoleCan(m.Role, auth.CapSkillInstall) {
			return ErrForbidden
		}
		return nil
	default:
		return fmt.Errorf("skills: unknown scope %q", scope)
	}
}

// Publish inserts a new Skill (if skillID is nil) or a new SkillVersion
// under an existing one — always a pure INSERT, never an UPDATE to any
// existing SkillVersion row (docs/adr/0012). content is uploaded to
// ObjectStore unencrypted — see docs/gotchas.md.
func (s *Service) Publish(
	ctx context.Context, principalID, projectID uuid.UUID,
	skillID *uuid.UUID, name, author, origin string,
	content []byte, declaredCapabilities []string,
) (SkillVersion, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapSkillPublish); err != nil {
		return SkillVersion{}, err
	}
	// Checked here rather than only in the adapters: the CLI publishes
	// straight from a file with no HTTP body limit in the way, so the cap
	// belongs at the domain chokepoint that every adapter shares. See
	// MaxContentSize for why a cap exists at all.
	if len(content) > MaxContentSize {
		return SkillVersion{}, ErrTooLarge
	}

	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])
	versionID := uuid.New()

	declared := declaredCapabilities
	if declared == nil {
		declared = []string{}
	}

	// Phase 1: resolve/create the Skill and reserve the next version number,
	// committed on its own — before any upload, so a brand-new Skill with no
	// SkillVersion yet is never mistaken for an installable one.
	var skill Skill
	var nextVersion int
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if skillID == nil {
			// Born in the project it was published from — the narrowest
			// scope, so publishing is never accidentally a public act
			// (docs/adr/0018). Widening it is Promote's job and a separate
			// decision.
			skill = Skill{
				ID: uuid.New(), Name: name, Author: author, Origin: origin,
				Scope: ScopeProject, ScopeRef: &projectID,
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO skills (id, name, author, origin, scope, scope_ref) VALUES ($1, $2, $3, $4, $5, $6)`,
				skill.ID, skill.Name, skill.Author, skill.Origin, skill.Scope, skill.ScopeRef,
			); err != nil {
				return fmt.Errorf("insert skill: %w", err)
			}
		} else {
			skill.ID = *skillID
		}

		return tx.QueryRow(ctx,
			`SELECT COALESCE(MAX(version), 0) + 1 FROM skill_versions WHERE skill_id = $1`, skill.ID,
		).Scan(&nextVersion)
	})
	if err != nil {
		return SkillVersion{}, fmt.Errorf("skills: publish: resolve next version: %w", err)
	}

	storageKey := fmt.Sprintf("skills/%s/versions/%s", skill.ID, versionID)

	// Phase 2: upload content under an immutable, UUID-based key — outside
	// any transaction, since object stores don't participate in Postgres
	// transactions (mirrors internal/artifacts.Service.Write,
	// docs/adr/0013). If this fails, no skill_versions row was ever
	// written, so there is nothing installable pointing at missing content.
	if _, err := s.store.Put(ctx, storageKey, bytes.NewReader(content), int64(len(content))); err != nil {
		return SkillVersion{}, fmt.Errorf("skills: upload content: %w", err)
	}

	// Phase 3: commit the immutable SkillVersion row now that its content
	// definitely exists in storage. If this fails, the object uploaded
	// above becomes an orphan candidate (unreferenced, unused storage) —
	// the safe failure direction, unlike a DB row referencing nothing.
	result := SkillVersion{
		ID: versionID, SkillID: skill.ID, Version: nextVersion,
		Checksum: checksum, StorageKey: storageKey,
		Files: []FileManifestEntry{}, DeclaredCapabilities: declared,
		TrustStatus: "unverified",
	}
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO skill_versions (id, skill_id, version, checksum, storage_key, files, declared_capabilities, trust_status,
			                             published_in_project, published_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, 'unverified', $8, $9)`,
			versionID, skill.ID, nextVersion, checksum, storageKey, []byte("[]"), declared, projectID, principalID,
		)
		return err
	})
	if err != nil {
		return SkillVersion{}, fmt.Errorf("skills: publish: insert skill_versions: %w", err)
	}
	return result, nil
}

// Install creates a SkillBinding — "project X uses skill Y". scope names
// where the binding applies, scopeRef its organization/project id (nil for
// ScopeInstallation, which Install always rejects — see
// ErrUnsupportedScope). projectID anchors the initial capability check;
// checkScopeTarget independently authorizes the caller against scopeRef
// itself before the binding is written, since scopeRef need not equal
// projectID.
func (s *Service) Install(
	ctx context.Context, principalID, userID, projectID uuid.UUID,
	scope Scope, scopeRef *uuid.UUID,
	skillID, skillVersionID uuid.UUID,
	managed, overridable bool,
) (SkillBinding, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapSkillInstall); err != nil {
		return SkillBinding{}, err
	}

	b := SkillBinding{
		ID: uuid.New(), Scope: scope, ScopeRef: scopeRef,
		SkillID: skillID, SkillVersionID: skillVersionID,
		Pinned: true, Managed: managed, Overridable: overridable,
		InstalledBy: &principalID,
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := s.checkScopeTarget(ctx, tx, principalID, userID, scope, scopeRef); err != nil {
			return err
		}
		if err := s.checkSkillVersionBelongsTo(ctx, tx, skillID, skillVersionID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO skill_bindings (id, scope, scope_ref, skill_id, skill_version_id, pinned, managed, overridable, installed_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			b.ID, b.Scope, b.ScopeRef, b.SkillID, b.SkillVersionID, b.Pinned, b.Managed, b.Overridable, b.InstalledBy,
		)
		return err
	})
	if err != nil {
		return SkillBinding{}, fmt.Errorf("skills: install: %w", err)
	}
	return b, nil
}

// Override creates a project-scope binding for skillID that shadows
// whatever organization/installation binding of the same skill would
// otherwise apply — rejecting the attempt with ErrNotOverridable if any
// visible non-project binding of skillID has Overridable = false
// (docs/adr/0012).
func (s *Service) Override(
	ctx context.Context, principalID, projectID uuid.UUID,
	skillID, skillVersionID uuid.UUID, managed, overridableForChild bool,
) (SkillBinding, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapSkillOverride); err != nil {
		return SkillBinding{}, err
	}

	b := SkillBinding{
		ID: uuid.New(), Scope: ScopeProject, ScopeRef: &projectID,
		SkillID: skillID, SkillVersionID: skillVersionID,
		Pinned: true, Managed: managed, Overridable: overridableForChild,
		InstalledBy: &principalID,
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := s.checkSkillVersionBelongsTo(ctx, tx, skillID, skillVersionID); err != nil {
			return err
		}

		orgID, err := s.resolveProjectOrgID(ctx, tx, projectID)
		if err != nil {
			return err
		}

		// Scoped to installation-wide bindings plus this project's actual
		// owning organization only — not every organization's binding of
		// skillID system-wide, and not silently missing the owning
		// organization's binding because it wasn't independently visible
		// under RLS (migrations/000013 makes it visible to any project
		// member of an org project, regardless of org membership).
		rows, err := tx.Query(ctx,
			`SELECT overridable FROM skill_bindings
			 WHERE skill_id = $1 AND (scope = 'installation' OR (scope = 'organization' AND scope_ref = $2))`,
			skillID, orgID,
		)
		if err != nil {
			return fmt.Errorf("query existing bindings: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var overridable bool
			if err := rows.Scan(&overridable); err != nil {
				return err
			}
			if !overridable {
				return ErrNotOverridable
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO skill_bindings (id, scope, scope_ref, skill_id, skill_version_id, pinned, managed, overridable, installed_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			b.ID, b.Scope, b.ScopeRef, b.SkillID, b.SkillVersionID, b.Pinned, b.Managed, b.Overridable, b.InstalledBy,
		)
		return err
	})
	if err != nil {
		return SkillBinding{}, fmt.Errorf("skills: override: %w", err)
	}
	return b, nil
}

// Pin/Unpin toggle a project-scope binding's Pinned flag. In v1 this has no
// resolution-time effect (ListEffective always resolves through
// SkillVersionID regardless of Pinned) — the column and these methods exist
// for forward compatibility with a future version-constraint resolver, per
// ADR 0012's field list.
func (s *Service) setPinned(ctx context.Context, principalID, projectID, bindingID uuid.UUID, pinned bool) error {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapSkillInstall); err != nil {
		return err
	}
	return db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE skill_bindings SET pinned = $1 WHERE id = $2 AND scope = 'project' AND scope_ref = $3`,
			pinned, bindingID, projectID,
		)
		if err != nil {
			return fmt.Errorf("skills: set pinned: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Service) Pin(ctx context.Context, principalID, projectID, bindingID uuid.UUID) error {
	return s.setPinned(ctx, principalID, projectID, bindingID, true)
}

func (s *Service) Unpin(ctx context.Context, principalID, projectID, bindingID uuid.UUID) error {
	return s.setPinned(ctx, principalID, projectID, bindingID, false)
}

// ListEffective composes installation + organization (if orgID given) +
// project scope bindings into the effective set for projectID — a project
// binding of a given SkillID overrides any organization/installation
// binding of that same SkillID (docs/architecture-cookbook.md §12.1:
// scopes compose, they do not replace one another, but within a single
// SkillID the narrowest explicit binding wins).
func (s *Service) ListEffective(ctx context.Context, principalID, projectID uuid.UUID) ([]SkillBinding, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapSkillRead); err != nil {
		return nil, err
	}

	var bindings []SkillBinding
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		orgID, err := s.resolveProjectOrgID(ctx, tx, projectID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx,
			`SELECT id, scope, scope_ref, skill_id, skill_version_id, pinned, managed, overridable, installed_by, installed_at
			 FROM skill_bindings
			 WHERE scope = 'installation'
			    OR (scope = 'organization' AND scope_ref = $1)
			    OR (scope = 'project' AND scope_ref = $2)`,
			orgID, projectID,
		)
		if err != nil {
			return fmt.Errorf("query bindings: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var b SkillBinding
			if err := rows.Scan(&b.ID, &b.Scope, &b.ScopeRef, &b.SkillID, &b.SkillVersionID, &b.Pinned, &b.Managed, &b.Overridable, &b.InstalledBy, &b.InstalledAt); err != nil {
				return fmt.Errorf("scan binding: %w", err)
			}
			bindings = append(bindings, b)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("skills: list effective: %w", err)
	}

	// Compose: project wins over organization wins over installation, per
	// SkillID.
	rank := func(scope Scope) int {
		switch scope {
		case ScopeProject:
			return 3
		case ScopeOrganization:
			return 2
		default:
			return 1
		}
	}
	bySkill := make(map[uuid.UUID]SkillBinding, len(bindings))
	for _, b := range bindings {
		existing, ok := bySkill[b.SkillID]
		if !ok || rank(b.Scope) > rank(existing.Scope) {
			bySkill[b.SkillID] = b
		}
	}
	effective := make([]SkillBinding, 0, len(bySkill))
	for _, b := range bySkill {
		effective = append(effective, b)
	}
	return effective, nil
}
