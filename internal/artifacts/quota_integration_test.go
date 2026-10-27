package artifacts_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/quota"
)

// used reads the fixture namespace's count as the owner: cc_app has no grant
// on namespace_storage.
func (f *fixture) used(t *testing.T) int64 {
	t.Helper()
	var n int64
	err := db.WithoutPrincipal(context.Background(), f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT COALESCE((SELECT s.used_bytes FROM namespace_storage s JOIN projects p ON p.namespace_id = s.namespace_id
			                  WHERE p.id = $1), 0)`, f.projectID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("read used bytes: %v", err)
	}
	return n
}

// setLimit is the admin's exception, set the way the server sets it.
func (f *fixture) setLimit(t *testing.T, limit *int64, unlimited bool) {
	t.Helper()
	err := db.WithoutPrincipal(context.Background(), f.ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(),
			`SELECT cc_set_namespace_quota((SELECT namespace_id FROM projects WHERE id = $1), $2, $3, NULL)`,
			f.projectID, limit, unlimited)
		return err
	})
	if err != nil {
		t.Fatalf("set limit: %v", err)
	}
}

func (f *fixture) write(path string, size int, expected int) error {
	_, err := f.svc.Write(context.Background(), f.principalID, f.clientID, f.projectID, path, "text/plain",
		[]byte(strings.Repeat("x", size)), expected)
	return err
}

func TestTheServerDefaultRefusesAWriteThatDoesNotFit(t *testing.T) {
	f := newFixture(t)
	f.appPool.SetStorageQuotaDefault(100)

	if err := f.write("a.txt", 60, 0); err != nil {
		t.Fatalf("a write under the limit: %v", err)
	}
	if got := f.used(t); got != 60 {
		t.Fatalf("used = %d after a 60-byte write, want 60", got)
	}
	if err := f.write("b.txt", 50, 0); !errors.Is(err, quota.ErrExceeded) {
		t.Fatalf("a write over the limit returned %v, want quota.ErrExceeded", err)
	}
	if got := f.used(t); got != 60 {
		t.Errorf("used = %d after a refused write, want 60 — a refusal must not be charged", got)
	}
	// Exactly to the limit is allowed: the limit is what the namespace may
	// hold, not one byte less.
	if err := f.write("c.txt", 40, 0); err != nil {
		t.Errorf("a write that fills the namespace exactly: %v", err)
	}
}

// The check before the upload is advisory; this is the test that the
// trigger decides. Both writers pass the check (the namespace is empty when
// they look), and only one of them may fit.
func TestConcurrentWritesCannotBothSqueezeUnderTheLimit(t *testing.T) {
	f := newFixture(t)
	f.appPool.SetStorageQuotaDefault(100)

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = f.write(fmt.Sprintf("w%d.txt", i), 60, 0)
		}()
	}
	wg.Wait()

	var ok, refused int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, quota.ErrExceeded):
			refused++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || refused != 3 {
		t.Errorf("%d writes succeeded and %d were refused, want 1 and 3", ok, refused)
	}
	if got := f.used(t); got != 60 {
		t.Errorf("used = %d, want 60", got)
	}
}

func TestDeletingAnArtifactGivesItsBytesBack(t *testing.T) {
	f := newFixture(t)
	f.appPool.SetStorageQuotaDefault(100)

	if err := f.write("a.txt", 30, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.write("a.txt", 50, 1); err != nil {
		t.Fatal(err)
	}
	if got := f.used(t); got != 80 {
		t.Fatalf("used = %d after two versions, want 80 — every version is stored", got)
	}

	// An admin lowers the limit below what is held. Writes stop; the delete
	// that gets the namespace back under must not.
	low := int64(10)
	f.setLimit(t, &low, false)
	if err := f.write("b.txt", 1, 0); !errors.Is(err, quota.ErrExceeded) {
		t.Fatalf("a write over a lowered limit returned %v", err)
	}
	if err := f.svc.Delete(context.Background(), f.principalID, f.clientID, f.projectID, "a.txt"); err != nil {
		t.Fatalf("delete in a namespace over its limit: %v", err)
	}
	if got := f.used(t); got != 0 {
		t.Errorf("used = %d after deleting the only artifact, want 0", got)
	}
	if err := f.write("b.txt", 10, 0); err != nil {
		t.Errorf("a write that fits after the delete: %v", err)
	}
}

func TestAnAdminExceptionOverridesTheDefault(t *testing.T) {
	f := newFixture(t)
	f.appPool.SetStorageQuotaDefault(10)

	f.setLimit(t, nil, true)
	if err := f.write("big.txt", 500, 0); err != nil {
		t.Fatalf("a write in an unlimited namespace: %v", err)
	}

	larger := int64(600)
	f.setLimit(t, &larger, false)
	if err := f.write("more.txt", 100, 0); err != nil {
		t.Fatalf("a write under the exception, over the default: %v", err)
	}
	if err := f.write("too-much.txt", 1, 0); !errors.Is(err, quota.ErrExceeded) {
		t.Fatalf("a write over the exception returned %v", err)
	}

	// Back on the default, which this namespace is far over.
	f.setLimit(t, nil, false)
	if err := f.write("x.txt", 1, 0); !errors.Is(err, quota.ErrExceeded) {
		t.Errorf("back on the default, a write returned %v", err)
	}
}

func TestWithoutADefaultNothingIsLimitedButEverythingIsCounted(t *testing.T) {
	f := newFixture(t)
	if err := f.write("a.txt", 1000, 0); err != nil {
		t.Fatal(err)
	}
	if got := f.used(t); got != 1000 {
		t.Errorf("used = %d, want 1000 — the count runs whether or not anything is limited", got)
	}
}

func TestTheApplicationCannotTouchTheCountDirectly(t *testing.T) {
	f := newFixture(t)
	err := db.WithPrincipal(context.Background(), f.appPool, db.Principal{ID: f.principalID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE namespace_storage SET used_bytes = 0`)
		return err
	})
	if err == nil {
		t.Error("cc_app updated namespace_storage")
	}
	err = db.WithPrincipal(context.Background(), f.appPool, db.Principal{ID: f.principalID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `SELECT cc_charge_storage(gen_random_uuid(), -1)`)
		return err
	})
	if err == nil {
		t.Error("cc_app called cc_charge_storage")
	}
}
