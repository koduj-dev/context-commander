package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/identity"
)

// A second identity claiming a taken email or name is a conflict the caller
// can act on, not a server fault. Before ErrAlreadyExists both surfaced as a
// bare constraint violation, which the HTTP adapter had no case for and so
// answered 500 — telling an operator who had simply created the same user
// twice that their installation was broken.
func TestCreateReportsDuplicatesAsConflict(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()

	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}
	defer pool.Close()

	store := identity.NewStore()

	t.Run("user email", func(t *testing.T) {
		// Separate transactions: the first insert must be committed for the
		// second to collide with it the way two requests would.
		if err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			_, err := store.CreateUser(ctx, tx, "duplicate@example.com", "First")
			return err
		}); err != nil {
			t.Fatalf("first create: %v", err)
		}

		err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			_, err := store.CreateUser(ctx, tx, "duplicate@example.com", "Second")
			return err
		})
		if !errors.Is(err, identity.ErrAlreadyExists) {
			t.Fatalf("duplicate email: got %v, want ErrAlreadyExists", err)
		}
	})

	// service_principals.name carries no UNIQUE constraint (migrations/000001),
	// unlike users.email. Two agents may therefore share a name today, and
	// this records that as the current behaviour rather than leaving the next
	// reader to discover the asymmetry from a surprise. CreateServicePrincipal
	// maps a unique violation the same way regardless, so adding the
	// constraint later needs a migration and this test flipped — no change to
	// the store.
	t.Run("service principal names are not unique today", func(t *testing.T) {
		for _, description := range []string{"first", "second"} {
			if err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
				_, err := store.CreateServicePrincipal(ctx, tx, "duplicate-agent", description, nil)
				return err
			}); err != nil {
				t.Fatalf("create %q: %v", description, err)
			}
		}
	})
}
