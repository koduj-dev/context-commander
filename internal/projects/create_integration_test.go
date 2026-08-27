package projects_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// TestCreateProjectAndSelfOwnerThroughAppRole is the regression test
// docs/adr/0015-project-creation-rls-bootstrap.md calls for: creating a
// project and adding oneself as its first OWNER, run entirely as cc_app —
// not seeded through the RLS-exempt cc_owner pool the way every earlier
// test did, which is exactly why this gap went unnoticed until the CLI's
// real write path exercised it.
func TestCreateProjectAndSelfOwnerThroughAppRole(t *testing.T) {
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

	// Only the User/Principal exists beforehand (identity isn't
	// project-scoped, so it's fine to seed via the owner pool) — everything
	// project-related below runs as cc_app.
	principalID := uuid.New()
	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture User"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, principalID, userID)
		return err
	})
	if err != nil {
		t.Fatalf("seed principal: %v", err)
	}

	store := projects.NewStore()

	var project projects.Project
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		// A User's owner_user_id needs the actual user row; fetch it via
		// the principal we just created (identity isn't this package's
		// concern, so a raw lookup is fine here).
		var userID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT user_id FROM principals WHERE id = $1`, principalID).Scan(&userID); err != nil {
			return err
		}

		ns, err := store.CreatePersonalNamespace(ctx, tx, userID, "app-role-fixture")
		if err != nil {
			return err
		}
		project, err = store.Create(ctx, tx, ns.ID, "proj", "Proj")
		if err != nil {
			return err
		}
		_, err = store.AddMember(ctx, tx, project.ID, principalID, projects.RoleOwner)
		return err
	})
	if err != nil {
		t.Fatalf("create project + self-OWNER through cc_app: %v", err)
	}

	// The creator can now read their own project and membership back,
	// still as cc_app.
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		got, err := store.Get(ctx, tx, project.ID)
		if err != nil {
			return err
		}
		if got.ID != project.ID {
			t.Errorf("Get returned a different project")
		}
		membership, err := store.GetMembership(ctx, tx, project.ID, principalID)
		if err != nil {
			return err
		}
		if membership.Role != projects.RoleOwner {
			t.Errorf("membership role = %s, want OWNER", membership.Role)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read back through cc_app: %v", err)
	}

	// A second, unrelated principal must NOT be able to see the project at
	// all (projects now has RLS — docs/adr/0015).
	outsiderID := uuid.New()
	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Outsider"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, outsiderID, userID)
		return err
	})
	if err != nil {
		t.Fatalf("seed outsider: %v", err)
	}

	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: outsiderID}, func(tx pgx.Tx) error {
		_, err := store.Get(ctx, tx, project.ID)
		return err
	})
	if err == nil {
		t.Fatal("outsider (non-member) should not be able to Get a project they're not a member of, got no error")
	}
}

// TestProjectMembersBootstrapCannotBeReusedAfterFirstMember proves the
// self-bootstrap INSERT policy really can only ever fire once per project:
// a second principal trying the same "insert myself, no members exist yet"
// path against an already-owned project must be denied by RLS, not
// silently let them self-grant membership.
func TestProjectMembersBootstrapCannotBeReusedAfterFirstMember(t *testing.T) {
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

	ownerPrincipalID := uuid.New()
	attackerPrincipalID := uuid.New()
	var projectID uuid.UUID

	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		ownerUserID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			ownerUserID, ownerUserID.String()+"@example.test", "Owner"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			ownerPrincipalID, ownerUserID); err != nil {
			return err
		}
		attackerUserID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			attackerUserID, attackerUserID.String()+"@example.test", "Attacker"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			attackerPrincipalID, attackerUserID); err != nil {
			return err
		}
		namespaceID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			namespaceID, "victim-"+namespaceID.String()[:8], ownerUserID); err != nil {
			return err
		}
		projectID = uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'victim', 'Victim')`,
			projectID, namespaceID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'OWNER')`,
			projectID, ownerPrincipalID)
		return err
	})
	if err != nil {
		t.Fatalf("seed owned project: %v", err)
	}

	// The project already has an OWNER — the attacker's own self-bootstrap
	// insert must be rejected by RLS (both permissive INSERT policies fail:
	// bootstrap because members already exist, by-member because the
	// attacker isn't one).
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: attackerPrincipalID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'OWNER')`,
			projectID, attackerPrincipalID)
		return err
	})
	if err == nil {
		t.Fatal("attacker self-inserting membership into an already-owned project should be denied by RLS, got no error")
	}
}
