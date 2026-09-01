package identity_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/identity"
)

// TestGetUserAndGetUsersForPrincipals exercises the two read paths the web
// UI needs — GetMe (a single user by id) and the audit log's batch actor
// resolution (a set of principal ids to their users) — neither of which
// existed before this package had any way to go from an id back to a human.
func TestGetUserAndGetUsersForPrincipals(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()

	ownerPool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}

	store := identity.NewStore()

	var aliceUserID, alicePrincipalID, bobUserID, bobPrincipalID = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			aliceUserID, "alice-store@example.test", "Alice"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			alicePrincipalID, aliceUserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			bobUserID, "bob-store@example.test", "Bob"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			bobPrincipalID, bobUserID)
		return err
	})
	if err != nil {
		t.Fatalf("seed users/principals: %v", err)
	}

	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		u, err := store.GetUser(ctx, tx, aliceUserID)
		if err != nil {
			return err
		}
		if u.Email != "alice-store@example.test" {
			t.Errorf("GetUser email = %q, want alice-store@example.test", u.Email)
		}

		users, err := store.GetUsersForPrincipals(ctx, tx, []uuid.UUID{alicePrincipalID, bobPrincipalID})
		if err != nil {
			return err
		}
		if len(users) != 2 {
			t.Fatalf("GetUsersForPrincipals returned %d users, want 2", len(users))
		}
		if got := users[alicePrincipalID].Email; got != "alice-store@example.test" {
			t.Errorf("GetUsersForPrincipals[alice] email = %q, want alice-store@example.test", got)
		}
		if got := users[bobPrincipalID].Email; got != "bob-store@example.test" {
			t.Errorf("GetUsersForPrincipals[bob] email = %q, want bob-store@example.test", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
}
