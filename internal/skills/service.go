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
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/storage"
)

// Service is skills' authorization chokepoint (docs/adr/0014). Every
// mutation needs an anchor project (projectID) to check a capability
// against, even for organization/installation-scope bindings — this
// codebase has no separate "organization admin" or "installation admin"
// capability model outside of internal/organizations' own OWNER/ADMIN
// roles, and Publish (cookbook §12.2) is framed as something an agent does
// while working on a specific project, so anchoring every mutation to a
// project the caller actually holds a role on is the honest v1 model, not
// a shortcut. See ErrUnsupportedScope for the one case (installation-scope
// bindings) this does not cover at all.
type Service struct {
	pool     *db.Pool
	authz    *auth.AuthzService
	orgs     *organizations.Store
	projects *projects.Store
	store    storage.ObjectStore
}

func NewService(pool *db.Pool, authz *auth.AuthzService, orgs *organizations.Store, projectsStore *projects.Store, store storage.ObjectStore) *Service {
	return &Service{pool: pool, authz: authz, orgs: orgs, projects: projectsStore, store: store}
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
// the caller may install into scopeRef. ScopeStore installation is
// rejected outright — see ErrUnsupportedScope.
func (s *Service) checkScopeTarget(ctx context.Context, tx pgx.Tx, principalID, userID uuid.UUID, scope Scope, scopeRef *uuid.UUID) error {
	switch scope {
	case ScopeInstallation:
		return ErrUnsupportedScope
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

	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])
	versionID := uuid.New()

	var result SkillVersion
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var skill Skill
		if skillID == nil {
			skill = Skill{ID: uuid.New(), Name: name, Author: author, Origin: origin}
			if _, err := tx.Exec(ctx,
				`INSERT INTO skills (id, name, author, origin) VALUES ($1, $2, $3, $4)`,
				skill.ID, skill.Name, skill.Author, skill.Origin,
			); err != nil {
				return fmt.Errorf("insert skill: %w", err)
			}
		} else {
			skill.ID = *skillID
		}

		var nextVersion int
		err := tx.QueryRow(ctx,
			`SELECT COALESCE(MAX(version), 0) + 1 FROM skill_versions WHERE skill_id = $1`, skill.ID,
		).Scan(&nextVersion)
		if err != nil {
			return fmt.Errorf("resolve next version: %w", err)
		}

		storageKey := fmt.Sprintf("skills/%s/versions/%d", skill.ID, nextVersion)

		declared := declaredCapabilities
		if declared == nil {
			declared = []string{}
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO skill_versions (id, skill_id, version, checksum, storage_key, files, declared_capabilities, trust_status)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, 'unverified')`,
			versionID, skill.ID, nextVersion, checksum, storageKey, []byte("[]"), declared,
		)
		if err != nil {
			return fmt.Errorf("insert skill_versions: %w", err)
		}

		result = SkillVersion{
			ID: versionID, SkillID: skill.ID, Version: nextVersion,
			Checksum: checksum, StorageKey: storageKey,
			Files: []FileManifestEntry{}, DeclaredCapabilities: declared,
			TrustStatus: "unverified",
		}
		return nil
	})
	if err != nil {
		return SkillVersion{}, fmt.Errorf("skills: publish: %w", err)
	}

	if _, err := s.store.Put(ctx, result.StorageKey, bytes.NewReader(content), int64(len(content))); err != nil {
		return SkillVersion{}, fmt.Errorf("skills: upload content: %w", err)
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
		rows, err := tx.Query(ctx,
			`SELECT overridable FROM skill_bindings WHERE skill_id = $1 AND scope IN ('installation', 'organization')`,
			skillID,
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
func (s *Service) ListEffective(ctx context.Context, principalID, projectID uuid.UUID, orgID *uuid.UUID) ([]SkillBinding, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapSkillRead); err != nil {
		return nil, err
	}

	var bindings []SkillBinding
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
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
