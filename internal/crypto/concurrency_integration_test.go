package crypto_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
)

// TestConcurrentFirstEncryptProvisionsOneProjectKey is the regression test
// for the read-then-insert race in ensureProjectKey. Every encrypting domain
// — artifacts, handoffs, history, secrets — provisions a project's key
// lazily on its first write, so N writes arriving together at a brand-new
// project all saw ErrNoProjectKey and all inserted key_version 1. All but one
// failed on project_keys' unique constraint, surfacing as an opaque SQLSTATE
// 23505 rather than anything a caller could act on.
//
// Serialized, every writer must succeed and they must all share key_version 1
// — the second writer adopts the key the first committed instead of minting
// a rival.
//
// The barrier is inside the transaction, not around it: releasing the
// goroutines before they have connections only proves that pgxpool opens
// connections one at a time. The first writer would finish its whole
// transaction while the rest were still connecting, and the race would never
// be reached. Every writer therefore holds an open transaction before any of
// them proceeds, which needs MaxConns >= writers.
func TestConcurrentFirstEncryptProvisionsOneProjectKey(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()

	const writers = 16
	cfg, err := pgxpool.ParseConfig(dsns.Owner)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.MaxConns = writers + 2
	pool, err := db.NewPoolFromConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	projectID := seedProject(t, ctx, pool)
	provider := newTestProvider(t)

	var connected sync.WaitGroup
	connected.Add(writers)
	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	errs := make([]error, writers)
	versions := make([]int, writers)

	for i := range writers {
		done.Add(1)
		go func() {
			defer done.Done()
			ec := crypto.EncryptionContext{
				ProjectID: projectID, ResourceType: "artifact_version",
				ResourceID: uuid.New(), ResourceVersion: 1,
			}
			errs[i] = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
				connected.Done()
				start.Wait()
				_, _, _, version, err := provider.Encrypt(ctx, tx, ec, fmt.Appendf(nil, "payload-%d", i))
				versions[i] = version
				return err
			})
		}()
	}
	connected.Wait()
	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent first encrypt %d: %v", i, err)
		}
	}
	for i, v := range versions {
		if v != 1 {
			t.Errorf("writer %d got project_key_version %d, want 1", i, v)
		}
	}

	var keyRows int
	if err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_keys WHERE project_id = $1`, projectID).Scan(&keyRows)
	}); err != nil {
		t.Fatalf("count project keys: %v", err)
	}
	if keyRows != 1 {
		t.Errorf("project_keys rows = %d, want exactly 1", keyRows)
	}
}

// TestConcurrentRotationsEachGetTheirOwnVersion pins the same serialization
// on the explicit rotation path: two rotations that both read version N and
// both insert N+1 would collide on the unique constraint the same way.
func TestConcurrentRotationsEachGetTheirOwnVersion(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	projectID := seedProject(t, ctx, pool)
	provider := newTestProvider(t)

	// Provision version 1 first, so the rotations race on N -> N+1 rather
	// than on creation.
	if err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		return provider.RotateProjectKey(ctx, tx, projectID)
	}); err != nil {
		t.Fatalf("seed project key: %v", err)
	}

	const rotations = 8
	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	errs := make([]error, rotations)
	for i := range rotations {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			errs[i] = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
				return provider.RotateProjectKey(ctx, tx, projectID)
			})
		}()
	}
	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent rotation %d: %v", i, err)
		}
	}

	var keyRows int
	if err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_keys WHERE project_id = $1`, projectID).Scan(&keyRows)
	}); err != nil {
		t.Fatalf("count project keys: %v", err)
	}
	if want := 1 + rotations; keyRows != want {
		t.Errorf("project_keys rows = %d, want %d — a rotation was lost or collided", keyRows, want)
	}
}
