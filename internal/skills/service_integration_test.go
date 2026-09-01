package skills_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/skills"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

type fixture struct {
	t           *testing.T
	ctx         context.Context
	ownerPool   *db.Pool
	appPool     *db.Pool
	svc         *skills.Service
	orgs        *organizations.Store
	orgSvc      *organizations.Service
	projectID   uuid.UUID
	principalID uuid.UUID
	userID      uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsns := dbtest.New(t)
	ctx := context.Background()

	ownerPool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}
	appPool, err := db.NewPool(ctx, dsns.App)
	if err != nil {
		t.Fatalf("connect as cc_app: %v", err)
	}

	principalID := uuid.New()
	userID := uuid.New()
	projectID := uuid.New()
	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture User"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			principalID, userID); err != nil {
			return err
		}
		namespaceID := uuid.New()
		if _, err := tx.Exec(ctx,
			`INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			namespaceID, "fixture-"+namespaceID.String()[:8], userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'fixture', 'Fixture')`,
			projectID, namespaceID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'ADMIN')`,
			projectID, principalID)
		return err
	})
	if err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	store, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatalf("filesystem.New: %v", err)
	}

	orgStore := organizations.NewStore()
	projStore := projects.NewStore()
	authz := auth.NewAuthzService(appPool, projStore)

	return &fixture{
		t: t, ctx: ctx, ownerPool: ownerPool, appPool: appPool,
		svc:         skills.NewService(appPool, authz, orgStore, projStore, store),
		orgs:        orgStore,
		orgSvc:      organizations.NewService(appPool, orgStore),
		projectID:   projectID,
		principalID: principalID,
		userID:      userID,
	}
}

func TestPublishNeverMutatesExistingVersion(t *testing.T) {
	f := newFixture(t)

	v1, err := f.svc.Publish(f.ctx, f.principalID, f.projectID, nil, "code-review", "acme", "local", []byte("v1 content"), nil)
	if err != nil {
		t.Fatalf("Publish v1: %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("first Publish should produce version 1, got %d", v1.Version)
	}

	v2, err := f.svc.Publish(f.ctx, f.principalID, f.projectID, &v1.SkillID, "code-review", "acme", "local", []byte("v2 content"), nil)
	if err != nil {
		t.Fatalf("Publish v2: %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("second Publish should produce version 2, got %d", v2.Version)
	}
	if v2.SkillID != v1.SkillID {
		t.Fatalf("v2.SkillID = %s, want %s", v2.SkillID, v1.SkillID)
	}

	// The original version's row must be untouched — Publish only inserts.
	err = db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: f.principalID}, func(tx pgx.Tx) error {
		var checksum string
		if err := tx.QueryRow(f.ctx, `SELECT checksum FROM skill_versions WHERE id = $1`, v1.ID).Scan(&checksum); err != nil {
			return err
		}
		if checksum != v1.Checksum {
			t.Fatalf("v1 checksum changed after publishing v2: got %s, want %s", checksum, v1.Checksum)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify v1 unchanged: %v", err)
	}
}

func TestOverrideRejectedWhenNotOverridable(t *testing.T) {
	f := newFixture(t)

	v1, err := f.svc.Publish(f.ctx, f.principalID, f.projectID, nil, "security-review", "acme", "local", []byte("content"), nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Seed a non-overridable installation-scope binding directly (Install
	// rejects ScopeInstallation — this models an operator-seeded mandated
	// skill, see skills.ErrUnsupportedScope).
	bindingID := uuid.New()
	err = db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`INSERT INTO skill_bindings (id, scope, scope_ref, skill_id, skill_version_id, overridable) VALUES ($1, 'installation', NULL, $2, $3, false)`,
			bindingID, v1.SkillID, v1.ID)
		return err
	})
	if err != nil {
		t.Fatalf("seed installation binding: %v", err)
	}

	_, err = f.svc.Override(f.ctx, f.principalID, f.projectID, v1.SkillID, v1.ID, false, true)
	if !errors.Is(err, skills.ErrNotOverridable) {
		t.Fatalf("Override of non-overridable installation binding: got err=%v, want ErrNotOverridable", err)
	}
}

func TestListEffectiveComposesScopesAndProjectWins(t *testing.T) {
	f := newFixture(t)

	installV, err := f.svc.Publish(f.ctx, f.principalID, f.projectID, nil, "coding-conventions", "acme", "local", []byte("install content"), nil)
	if err != nil {
		t.Fatalf("Publish installation skill: %v", err)
	}
	err = db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`INSERT INTO skill_bindings (id, scope, scope_ref, skill_id, skill_version_id, overridable) VALUES ($1, 'installation', NULL, $2, $3, true)`,
			uuid.New(), installV.SkillID, installV.ID)
		return err
	})
	if err != nil {
		t.Fatalf("seed installation binding: %v", err)
	}

	// A different skill, only bound at project scope.
	projectV, err := f.svc.Publish(f.ctx, f.principalID, f.projectID, nil, "xyz-api", "acme", "local", []byte("project content"), nil)
	if err != nil {
		t.Fatalf("Publish project skill: %v", err)
	}
	if _, err := f.svc.Install(f.ctx, f.principalID, f.userID, f.projectID, skills.ScopeProject, &f.projectID, projectV.SkillID, projectV.ID, false, true); err != nil {
		t.Fatalf("Install project skill: %v", err)
	}

	// Override the installation skill at project scope with a newer version.
	overrideV, err := f.svc.Publish(f.ctx, f.principalID, f.projectID, &installV.SkillID, "coding-conventions", "acme", "local", []byte("overridden content"), nil)
	if err != nil {
		t.Fatalf("Publish override version: %v", err)
	}
	if _, err := f.svc.Override(f.ctx, f.principalID, f.projectID, overrideV.SkillID, overrideV.ID, false, true); err != nil {
		t.Fatalf("Override: %v", err)
	}

	effective, err := f.svc.ListEffective(f.ctx, f.principalID, f.projectID, nil)
	if err != nil {
		t.Fatalf("ListEffective: %v", err)
	}
	if len(effective) != 2 {
		t.Fatalf("ListEffective returned %d bindings, want 2 (coding-conventions override + xyz-api)", len(effective))
	}
	for _, b := range effective {
		switch b.SkillID {
		case installV.SkillID:
			if b.Scope != skills.ScopeProject || b.SkillVersionID != overrideV.ID {
				t.Errorf("coding-conventions: got scope=%s version=%s, want project-scope override version %s", b.Scope, b.SkillVersionID, overrideV.ID)
			}
		case projectV.SkillID:
			if b.Scope != skills.ScopeProject {
				t.Errorf("xyz-api: got scope=%s, want project", b.Scope)
			}
		default:
			t.Errorf("unexpected skill_id %s in effective set", b.SkillID)
		}
	}
}

func TestPublishDeniedWithoutCapability(t *testing.T) {
	f := newFixture(t)
	outsider := uuid.New()

	_, err := f.svc.Publish(f.ctx, outsider, f.projectID, nil, "x", "a", "o", []byte("c"), nil)
	if !errors.Is(err, skills.ErrForbidden) {
		t.Fatalf("Publish by non-member: got err=%v, want ErrForbidden", err)
	}
}

// TestInstallDeniedForNonMemberOfScopeRefProject regression-guards against
// installing a binding into a project other than the anchor projectID used
// for the initial capability check: f.principalID is ADMIN on f.projectID
// (the anchor, giving CapSkillInstall) but only READER on a second project
// — Install must deny writing a binding scoped to that second project
// rather than authorizing it off the unrelated anchor.
func TestInstallDeniedForNonMemberOfScopeRefProject(t *testing.T) {
	f := newFixture(t)
	otherProjectID := uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		otherOwnerUserID := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			otherOwnerUserID, otherOwnerUserID.String()+"@example.test", "Other Project Owner"); err != nil {
			return err
		}
		namespaceID := uuid.New()
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			namespaceID, "other-"+namespaceID.String()[:8], otherOwnerUserID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'other', 'Other')`,
			otherProjectID, namespaceID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'READER')`,
			otherProjectID, f.principalID)
		return err
	})
	if err != nil {
		t.Fatalf("seed other project: %v", err)
	}

	v, err := f.svc.Publish(f.ctx, f.principalID, f.projectID, nil, "cross-project-skill", "acme", "local", []byte("content"), nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	_, err = f.svc.Install(f.ctx, f.principalID, f.userID, f.projectID, skills.ScopeProject, &otherProjectID, v.SkillID, v.ID, false, true)
	if !errors.Is(err, skills.ErrForbidden) {
		t.Fatalf("Install into a project the caller is only READER on: got err=%v, want ErrForbidden", err)
	}
}

func TestInstallInstallationScopeRejected(t *testing.T) {
	f := newFixture(t)

	v, err := f.svc.Publish(f.ctx, f.principalID, f.projectID, nil, "git-workflow", "acme", "local", []byte("content"), nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	_, err = f.svc.Install(f.ctx, f.principalID, f.userID, f.projectID, skills.ScopeInstallation, nil, v.SkillID, v.ID, false, true)
	if !errors.Is(err, skills.ErrUnsupportedScope) {
		t.Fatalf("Install at installation scope: got err=%v, want ErrUnsupportedScope", err)
	}
}
