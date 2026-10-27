package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
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

	// Agent names are unique per registrar, case-insensitively
	// (migrations/000027) — not installation-wide, and not at all for an
	// agent nobody is recorded as having registered.
	t.Run("service principal names", func(t *testing.T) {
		var first, second uuid.UUID
		if err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			a, err := store.CreateUser(ctx, tx, "registrar-a@example.com", "A")
			if err != nil {
				return err
			}
			b, err := store.CreateUser(ctx, tx, "registrar-b@example.com", "B")
			first, second = a.ID, b.ID
			return err
		}); err != nil {
			t.Fatalf("create registrars: %v", err)
		}

		create := func(name string, by *uuid.UUID) error {
			return db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
				_, err := store.CreateServicePrincipal(ctx, tx, name, "", by)
				return err
			})
		}

		if err := create("codex", &first); err != nil {
			t.Fatalf("first agent: %v", err)
		}
		if err := create("Codex", &first); !errors.Is(err, identity.ErrAlreadyExists) {
			t.Fatalf("same registrar, same name in another case: got %v, want ErrAlreadyExists", err)
		}
		if err := create("codex", &second); err != nil {
			t.Fatalf("another registrar may use the same name: %v", err)
		}
		for i := range 2 {
			if err := create("codex", nil); err != nil {
				t.Fatalf("unattributed agent %d: %v", i, err)
			}
		}
	})
}
