package artifacts_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/storage"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

type fixture struct {
	svc         *artifacts.Service
	ownerPool   *db.Pool
	store       storage.ObjectStore
	projectID   uuid.UUID
	principalID uuid.UUID
	clientID    *uuid.UUID
}

// newFixture connects the Service to cc_app, the same role the production
// server uses — Write/Read/List/etc. below genuinely exercise RLS, not just
// application logic (docs/adr/0002's testing requirement).
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
	projectID := uuid.New()
	clientID := uuid.New()
	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture User"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			principalID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO client_credentials (id, principal_id, client_type, token_hash) VALUES ($1, $2, 'test', 'unused')`,
			clientID, principalID); err != nil {
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
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'OWNER')`,
			projectID, principalID)
		return err
	})
	if err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	rootKeyB64, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatalf("GenerateRootKeyBase64: %v", err)
	}
	t.Setenv("CC_ROOT_KEY", rootKeyB64)
	root, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
	if err != nil {
		t.Fatalf("NewEnvRootKeyProvider: %v", err)
	}
	cryptoProvider := crypto.NewProvider(root)

	store, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatalf("filesystem.New: %v", err)
	}

	return &fixture{
		svc:         artifacts.NewService(appPool, cryptoProvider, store, auth.NewAuthzService(appPool, projects.NewStore()), gates.NewEngine(appPool)),
		ownerPool:   ownerPool,
		store:       store,
		projectID:   projectID,
		principalID: principalID,
		clientID:    &clientID,
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	v, err := f.svc.Write(ctx, f.principalID, f.clientID, f.projectID, "notes/architecture.md", "text/markdown", []byte("# Architecture\n"), 0)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if v.Version != 1 {
		t.Errorf("first Write should produce version 1, got %d", v.Version)
	}

	got, content, err := f.svc.Read(ctx, f.principalID, f.projectID, "notes/architecture.md", nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(content) != "# Architecture\n" {
		t.Fatalf("Read content = %q, want %q", content, "# Architecture\n")
	}
	if got.ID != v.ID {
		t.Error("Read returned a different version than Write produced")
	}

	v2, err := f.svc.Write(ctx, f.principalID, f.clientID, f.projectID, "notes/architecture.md", "text/markdown", []byte("# Architecture v2\n"), 1)
	if err != nil {
		t.Fatalf("second Write: %v", err)
	}
	if v2.Version != 2 {
		t.Errorf("second Write should produce version 2, got %d", v2.Version)
	}
	if v2.ParentVersion == nil || *v2.ParentVersion != 1 {
		t.Errorf("v2.ParentVersion = %v, want pointer to 1", v2.ParentVersion)
	}

	latest, latestContent, err := f.svc.Read(ctx, f.principalID, f.projectID, "notes/architecture.md", nil)
	if err != nil {
		t.Fatalf("Read latest: %v", err)
	}
	if latest.Version != 2 || string(latestContent) != "# Architecture v2\n" {
		t.Fatalf("Read latest returned version=%d content=%q, want version=2 content=%q", latest.Version, latestContent, "# Architecture v2\n")
	}

	v1, v1Content, err := f.svc.Read(ctx, f.principalID, f.projectID, "notes/architecture.md", intPtr(1))
	if err != nil {
		t.Fatalf("Read v1: %v", err)
	}
	if string(v1Content) != "# Architecture\n" {
		t.Fatalf("Read v1 content = %q, want the original content", v1Content)
	}
	_ = v1
}

// TestConcurrentWriteConflict is the plan's own acceptance test: two
// writers both believing expected_version=0 (or one racing a value the
// other just advanced past) — exactly one succeeds, the other gets
// ErrConflict, via the atomic UPDATE ... WHERE current_version = expected
// (not an app-level read-then-write).
func TestConcurrentWriteConflict(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.svc.Write(ctx, f.principalID, f.clientID, f.projectID, "shared.md", "text/markdown", []byte("v1"), 0); err != nil {
		t.Fatalf("initial Write: %v", err)
	}

	type result struct {
		v   artifacts.Version
		err error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		content := []byte{byte('a' + i)}
		go func() {
			v, err := f.svc.Write(ctx, f.principalID, f.clientID, f.projectID, "shared.md", "text/markdown", content, 1)
			results <- result{v, err}
		}()
	}

	var successes, conflicts int
	for i := 0; i < 2; i++ {
		r := <-results
		switch {
		case r.err == nil:
			successes++
		case errors.Is(r.err, artifacts.ErrConflict):
			conflicts++
		default:
			t.Errorf("unexpected error: %v", r.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("got %d successes and %d conflicts, want exactly 1 and 1", successes, conflicts)
	}

	history, err := f.svc.History(ctx, f.principalID, f.projectID, "shared.md")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("History has %d versions after one lost conflict, want exactly 2 (v1 + the one winner)", len(history))
	}
}

// addPrincipal creates a new user+principal and adds it to f.projectID with
// role, for testing AuthzService-driven ErrForbidden paths.
func (f *fixture) addPrincipal(t *testing.T, role projects.Role) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	principalID := uuid.New()
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Extra Fixture User"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			principalID, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`,
			f.projectID, principalID, role)
		return err
	})
	if err != nil {
		t.Fatalf("addPrincipal: %v", err)
	}
	return principalID
}

// nonMemberPrincipal returns a valid principal that is NOT a member of
// f.projectID at all — for cross-project-style ErrForbidden tests.
func (f *fixture) nonMemberPrincipal(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	principalID := uuid.New()
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Outsider"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			principalID, userID)
		return err
	})
	if err != nil {
		t.Fatalf("nonMemberPrincipal: %v", err)
	}
	return principalID
}

func TestWriteDeniedForReaderRole(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	reader := f.addPrincipal(t, projects.RoleReader)

	_, err := f.svc.Write(ctx, reader, f.clientID, f.projectID, "notes.md", "text/markdown", []byte("hi"), 0)
	if !errors.Is(err, artifacts.ErrForbidden) {
		t.Fatalf("READER Write: got err=%v, want ErrForbidden", err)
	}
}

func TestReadDeniedForNonMember(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.svc.Write(ctx, f.principalID, f.clientID, f.projectID, "secret.md", "text/markdown", []byte("secret"), 0); err != nil {
		t.Fatalf("Write: %v", err)
	}

	outsider := f.nonMemberPrincipal(t)
	if _, _, err := f.svc.Read(ctx, outsider, f.projectID, "secret.md", nil); !errors.Is(err, artifacts.ErrForbidden) {
		t.Fatalf("non-member Read: got err=%v, want ErrForbidden", err)
	}
}

func TestWriteRejectsOversizedContent(t *testing.T) {
	f := newFixture(t)
	oversized := make([]byte, artifacts.MaxSize+1)

	_, err := f.svc.Write(context.Background(), f.principalID, f.clientID, f.projectID, "big.bin", "application/octet-stream", oversized, 0)
	if !errors.Is(err, artifacts.ErrTooLarge) {
		t.Fatalf("Write over MaxSize: got err=%v, want ErrTooLarge", err)
	}
}

func TestListStatAndDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.svc.Write(ctx, f.principalID, f.clientID, f.projectID, "a.md", "text/markdown", []byte("a"), 0); err != nil {
		t.Fatalf("Write a: %v", err)
	}
	if _, err := f.svc.Write(ctx, f.principalID, f.clientID, f.projectID, "b.md", "text/markdown", []byte("b"), 0); err != nil {
		t.Fatalf("Write b: %v", err)
	}

	list, err := f.svc.List(ctx, f.principalID, f.projectID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List returned %d artifacts, want 2", len(list))
	}

	stat, err := f.svc.Stat(ctx, f.principalID, f.projectID, "a.md")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat.CurrentVersion != 1 {
		t.Errorf("Stat CurrentVersion = %d, want 1", stat.CurrentVersion)
	}

	if err := f.svc.Delete(ctx, f.principalID, f.clientID, f.projectID, "a.md"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	listAfterDelete, err := f.svc.List(ctx, f.principalID, f.projectID)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(listAfterDelete) != 1 {
		t.Fatalf("List after deleting one of two artifacts returned %d, want 1", len(listAfterDelete))
	}

	if _, _, err := f.svc.Read(ctx, f.principalID, f.projectID, "a.md", nil); !errors.Is(err, artifacts.ErrDeleted) {
		t.Fatalf("Read a deleted artifact: got err=%v, want ErrDeleted", err)
	}

	// Delete is a tombstone, not physical removal — the version history
	// (for audit) must still be reachable through the DB directly even
	// though the Service's own Read/List hide it. We verify via Stat,
	// which still returns the row with DeletedAt set.
	deletedStat, err := f.svc.Stat(ctx, f.principalID, f.projectID, "a.md")
	if err != nil {
		t.Fatalf("Stat of deleted artifact: %v", err)
	}
	if deletedStat.DeletedAt == nil {
		t.Error("Stat of a deleted artifact should still show DeletedAt set, not disappear entirely")
	}
}

func TestPurgeArtifactRemovesDataAndObjects(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	v, err := f.svc.Write(ctx, f.principalID, f.clientID, f.projectID, "temp.md", "text/markdown", []byte("temp"), 0)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	var storageKeys []string
	err = db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		var err error
		storageKeys, err = artifacts.PurgeArtifact(ctx, tx, v.ArtifactID)
		return err
	})
	if err != nil {
		t.Fatalf("PurgeArtifact: %v", err)
	}
	if len(storageKeys) != 1 || storageKeys[0] != v.StorageKey {
		t.Fatalf("PurgeArtifact returned storage keys %v, want [%s]", storageKeys, v.StorageKey)
	}

	if err := artifacts.DeleteStorageKeys(ctx, f.store, storageKeys); err != nil {
		t.Fatalf("DeleteStorageKeys: %v", err)
	}

	if _, err := f.store.Get(ctx, v.StorageKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("object should be gone from storage after purge, got err=%v", err)
	}

	stat, err := f.svc.Stat(ctx, f.principalID, f.projectID, "temp.md")
	if err == nil {
		t.Fatalf("Stat after purge should fail (artifact row is gone), got %+v", stat)
	}
}

func TestSweepOrphanObjectsRespectsGracePeriod(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	orphanKey := "projects/" + f.projectID.String() + "/artifacts/orphan/versions/" + uuid.New().String()
	if _, err := f.store.Put(ctx, orphanKey, bytes.NewReader([]byte("uploaded but never committed")), int64(len("uploaded but never committed"))); err != nil {
		t.Fatalf("Put orphan: %v", err)
	}

	// Fresh upload, still within the grace period: must NOT be swept.
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		deleted, err := artifacts.SweepOrphanObjects(ctx, tx, f.store, f.projectID, time.Hour)
		if err != nil {
			return err
		}
		if len(deleted) != 0 {
			t.Errorf("sweep with a 1h grace period deleted %v, want nothing (orphan is brand new)", deleted)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("sweep (grace period): %v", err)
	}

	// Same orphan, now "older than" a zero grace period: must be swept.
	err = db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		deleted, err := artifacts.SweepOrphanObjects(ctx, tx, f.store, f.projectID, 0)
		if err != nil {
			return err
		}
		if len(deleted) != 1 || deleted[0] != orphanKey {
			t.Errorf("sweep with no grace period deleted %v, want [%s]", deleted, orphanKey)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("sweep (no grace period): %v", err)
	}

	if _, err := f.store.Get(ctx, orphanKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("orphan should be deleted from storage, got err=%v", err)
	}
}

func TestSweepOrphanObjectsNeverDeletesKnownVersions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	v, err := f.svc.Write(ctx, f.principalID, f.clientID, f.projectID, "keep.md", "text/markdown", []byte("keep me"), 0)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	err = db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		deleted, err := artifacts.SweepOrphanObjects(ctx, tx, f.store, f.projectID, 0)
		if err != nil {
			return err
		}
		if len(deleted) != 0 {
			t.Errorf("sweep deleted %v, want nothing — %s is referenced by artifact_versions", deleted, v.StorageKey)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if _, _, err := f.svc.Read(ctx, f.principalID, f.projectID, "keep.md", nil); err != nil {
		t.Fatalf("Read after sweep: %v", err)
	}
}

func TestWriteBlockedBySecretScannerGate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO gate_bindings (id, scope, scope_ref, gate_name, hook) VALUES ($1, 'installation', NULL, 'secret-scanner', 'artifact.before_write')`,
			uuid.New())
		return err
	})
	if err != nil {
		t.Fatalf("seed gate binding: %v", err)
	}

	_, err = f.svc.Write(ctx, f.principalID, f.clientID, f.projectID, "config/.env", "text/plain", []byte("AWS_SECRET_ACCESS_KEY=AKIAABCDEFGHIJKLMNOP"), 0)
	if !errors.Is(err, artifacts.ErrBlockedByGate) {
		t.Fatalf("Write with secret-like content: got err=%v, want ErrBlockedByGate", err)
	}

	// Writing ordinary content on the same hook must still succeed.
	if _, err := f.svc.Write(ctx, f.principalID, f.clientID, f.projectID, "notes/readme.md", "text/markdown", []byte("# hello"), 0); err != nil {
		t.Fatalf("Write with clean content: %v", err)
	}
}

func intPtr(v int) *int { return &v }
