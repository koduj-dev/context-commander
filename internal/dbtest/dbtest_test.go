package dbtest_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
)

// These tests are about the harness, not about any domain. Cloning a
// template database instead of starting a container per test is only a
// legitimate trade if the clone keeps every property the tests below it
// silently depend on — so those properties are asserted here rather than
// assumed, since a clone that quietly lost one would not fail loudly. It
// would make every RLS test in the repo pass for the wrong reason.

// TestEachTestGetsItsOwnDatabase is the isolation the per-test container
// used to provide. Two calls must not see each other's rows.
func TestEachTestGetsItsOwnDatabase(t *testing.T) {
	ctx := context.Background()
	first := dbtest.New(t)
	second := dbtest.New(t)

	if first.Owner == second.Owner {
		t.Fatal("two calls returned the same database")
	}

	firstPool, err := db.NewPool(ctx, first.Owner)
	if err != nil {
		t.Fatalf("connect to first: %v", err)
	}
	defer firstPool.Close()
	secondPool, err := db.NewPool(ctx, second.Owner)
	if err != nil {
		t.Fatalf("connect to second: %v", err)
	}
	defer secondPool.Close()

	userID := uuid.New()
	err = db.WithoutPrincipal(ctx, firstPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Only in the first database")
		return err
	})
	if err != nil {
		t.Fatalf("insert into first: %v", err)
	}

	var visible int
	err = db.WithoutPrincipal(ctx, secondPool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, userID).Scan(&visible)
	})
	if err != nil {
		t.Fatalf("count in second: %v", err)
	}
	if visible != 0 {
		t.Fatal("a row written by one test is visible to another — the databases are not isolated")
	}
}

// TestCloneKeepsTheRoleModel pins what the clone must carry over from the
// template: the schema is owned by cc_owner and cc_app is neither owner nor
// RLS-exempt. Every RLS test in this repo proves nothing if cc_app can see
// rows because it happens to own the table (owners bypass their own
// policies unless FORCE ROW LEVEL SECURITY is set — docs/adr/0002).
func TestCloneKeepsTheRoleModel(t *testing.T) {
	ctx := context.Background()
	dsns := dbtest.New(t)

	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}
	defer pool.Close()

	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var owner string
		if err := tx.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE tablename = 'project_members'`).Scan(&owner); err != nil {
			return err
		}
		if owner != "cc_owner" {
			t.Fatalf("project_members is owned by %q, want cc_owner — RLS tests would prove nothing", owner)
		}

		var bypassRLS, superuser bool
		if err := tx.QueryRow(ctx,
			`SELECT rolbypassrls, rolsuper FROM pg_roles WHERE rolname = 'cc_app'`).Scan(&bypassRLS, &superuser); err != nil {
			return err
		}
		if bypassRLS || superuser {
			t.Fatalf("cc_app has bypassrls=%v superuser=%v, want both false", bypassRLS, superuser)
		}

		var rlsEnabled bool
		if err := tx.QueryRow(ctx,
			`SELECT relrowsecurity FROM pg_class WHERE relname = 'project_members'`).Scan(&rlsEnabled); err != nil {
			return err
		}
		if !rlsEnabled {
			t.Fatal("row level security is not enabled on project_members in the cloned database")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect clone: %v", err)
	}
}

// TestCloneIsFullyMigrated: a clone that came from a half-migrated template
// would fail in whichever domain test happened to touch the missing table,
// which is a miserable way to learn the harness is broken.
func TestCloneIsFullyMigrated(t *testing.T) {
	ctx := context.Background()
	dsns := dbtest.New(t)

	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}
	defer pool.Close()

	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var version int
		var dirty bool
		if err := tx.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
			return err
		}
		if dirty {
			t.Fatalf("cloned database is at a dirty migration version %d", version)
		}
		// The newest migration at the time of writing. A bare "> 0" would
		// pass against a template that stopped applying migrations
		// halfway.
		if version < 18 {
			t.Fatalf("cloned database is at migration %d, want at least 18", version)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect schema_migrations: %v", err)
	}
}
