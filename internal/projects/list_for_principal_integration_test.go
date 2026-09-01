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

func seedPrincipal(t *testing.T, ctx context.Context, ownerPool *db.Pool, email string) (principalID, userID uuid.UUID) {
	t.Helper()
	principalID, userID = uuid.New(), uuid.New()
	err := db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, email, email); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, principalID, userID)
		return err
	})
	if err != nil {
		t.Fatalf("seed principal %s: %v", email, err)
	}
	return principalID, userID
}

// TestListForPrincipalOnlyReturnsCallerMemberships is the negative test the
// web UI's dashboard project picker depends on: a principal must see
// exactly the projects they're a member of, RLS-enforced through cc_app,
// never a project belonging only to some other principal.
func TestListForPrincipalOnlyReturnsCallerMemberships(t *testing.T) {
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

	store := projects.NewStore()

	aliceID, aliceUserID := seedPrincipal(t, ctx, ownerPool, "alice-list@example.test")
	bobID, bobUserID := seedPrincipal(t, ctx, ownerPool, "bob-list@example.test")

	var aliceProject, bobProject projects.Project
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: aliceID}, func(tx pgx.Tx) error {
		var err error
		aliceProject, err = store.CreateForOwner(ctx, tx, aliceUserID, aliceID, "listforprincipal-alice/proj", "Alice Proj")
		return err
	})
	if err != nil {
		t.Fatalf("create alice's project: %v", err)
	}
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: bobID}, func(tx pgx.Tx) error {
		var err error
		bobProject, err = store.CreateForOwner(ctx, tx, bobUserID, bobID, "listforprincipal-bob/proj", "Bob Proj")
		return err
	})
	if err != nil {
		t.Fatalf("create bob's project: %v", err)
	}

	var aliceList []projects.MembershipWithProject
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: aliceID}, func(tx pgx.Tx) error {
		var err error
		aliceList, err = store.ListForPrincipal(ctx, tx, aliceID)
		return err
	})
	if err != nil {
		t.Fatalf("ListForPrincipal(alice): %v", err)
	}
	if len(aliceList) != 1 || aliceList[0].Project.ID != aliceProject.ID {
		t.Fatalf("ListForPrincipal(alice) = %+v, want exactly [%s]", aliceList, aliceProject.ID)
	}
	if aliceList[0].Role != projects.RoleOwner {
		t.Errorf("alice's role = %s, want OWNER", aliceList[0].Role)
	}
	if aliceList[0].NamespaceSlug != "listforprincipal-alice" {
		t.Errorf("namespace slug = %q, want %q", aliceList[0].NamespaceSlug, "listforprincipal-alice")
	}

	var bobList []projects.MembershipWithProject
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: bobID}, func(tx pgx.Tx) error {
		var err error
		bobList, err = store.ListForPrincipal(ctx, tx, bobID)
		return err
	})
	if err != nil {
		t.Fatalf("ListForPrincipal(bob): %v", err)
	}
	if len(bobList) != 1 || bobList[0].Project.ID != bobProject.ID {
		t.Fatalf("ListForPrincipal(bob) = %+v, want exactly [%s]", bobList, bobProject.ID)
	}
}
