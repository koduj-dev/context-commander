package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
)

// TestSetLocalDoesNotLeakAcrossPooledConnections is the acceptance test
// docs/adr/0003-set-local-transaction-context.md calls for: a principal set
// via WithPrincipal must never be visible to a later transaction on a
// *reused* physical connection that didn't set one itself. MaxConns=1 forces
// every transaction below onto the same physical connection, so this
// actually exercises connection reuse rather than hoping for it.
//
// This also regression-tests a real bug found while building Phase 1:
// current_setting('app.current_principal_id', true) returns NULL only the
// *first* time that custom GUC is touched on a backend. After it's been set
// once, an unset transaction on the same connection gets ” back, not NULL —
// which would hard-error a naive `USING (... ::uuid)` RLS policy instead of
// safely denying. cc_has_project_access() guards against that internally.
func TestSetLocalDoesNotLeakAcrossPooledConnections(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()

	ownerPool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}

	appCfg, err := pgxpool.ParseConfig(dsns.App)
	if err != nil {
		t.Fatalf("parse app dsn: %v", err)
	}
	appCfg.MaxConns = 1
	appPool, err := db.NewPoolFromConfig(ctx, appCfg)
	if err != nil {
		t.Fatalf("connect as cc_app: %v", err)
	}

	memberPrincipalID := uuid.New()
	otherPrincipalID := uuid.New()
	projectID := uuid.New()

	seedFixture(t, ctx, ownerPool, memberPrincipalID, otherPrincipalID, projectID)

	countVisible := func(tx pgx.Tx) int {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_members WHERE project_id = $1`, projectID).Scan(&n); err != nil {
			t.Fatalf("count visible rows: %v", err)
		}
		return n
	}

	// tx1: memberPrincipalID is a member -> sees the row.
	var tx1Count int
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: memberPrincipalID}, func(tx pgx.Tx) error {
		tx1Count = countVisible(tx)
		return nil
	})
	if err != nil {
		t.Fatalf("tx1: %v", err)
	}
	if tx1Count != 1 {
		t.Errorf("tx1 (member principal): got %d visible rows, want 1", tx1Count)
	}

	// tx2: no principal set at all on this (reused) connection. Must be
	// zero rows, and — the actual regression — must not error.
	var tx2Count int
	err = db.WithoutPrincipal(ctx, appPool, func(tx pgx.Tx) error {
		tx2Count = countVisible(tx)
		return nil
	})
	if err != nil {
		t.Fatalf("tx2 (no principal, reused connection): unexpected error: %v", err)
	}
	if tx2Count != 0 {
		t.Errorf("tx2 (no principal): got %d visible rows, want 0 — previous principal leaked across the pooled connection", tx2Count)
	}

	// tx3: a different, non-member principal on the same reused connection
	// -> still zero rows, proving isolation in both directions.
	var tx3Count int
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: otherPrincipalID}, func(tx pgx.Tx) error {
		tx3Count = countVisible(tx)
		return nil
	})
	if err != nil {
		t.Fatalf("tx3: %v", err)
	}
	if tx3Count != 0 {
		t.Errorf("tx3 (non-member principal): got %d visible rows, want 0", tx3Count)
	}
}

func seedFixture(t *testing.T, ctx context.Context, ownerPool *db.Pool, memberPrincipalID, otherPrincipalID, projectID uuid.UUID) {
	t.Helper()

	err := db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture User"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			memberPrincipalID, userID); err != nil {
			return err
		}

		otherUserID := uuid.New()
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			otherUserID, otherUserID.String()+"@example.test", "Other Fixture User"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			otherPrincipalID, otherUserID); err != nil {
			return err
		}

		namespaceID := uuid.New()
		if _, err := tx.Exec(ctx,
			`INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			namespaceID, "fixture-"+namespaceID.String()[:8], userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'fixture', 'Fixture Project')`,
			projectID, namespaceID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'OWNER')`,
			projectID, memberPrincipalID); err != nil {
			return err
		}
		// otherPrincipalID is deliberately NOT added as a member.
		return nil
	})
	if err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
}
