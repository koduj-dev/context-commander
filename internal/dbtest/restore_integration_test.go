package dbtest_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/memories"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/secrets"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

// services is everything that reads encrypted content, wired to one
// database, one object store and one root key — as `ccd serve` wires them.
type services struct {
	artifacts *artifacts.Service
	memories  *memories.Service
	secrets   *secrets.Service
}

func newServices(t *testing.T, appDSN, storageDir, rootKeyB64 string) services {
	t.Helper()
	pool, err := db.NewPool(context.Background(), appDSN)
	if err != nil {
		t.Fatalf("connect as cc_app: %v", err)
	}
	t.Cleanup(pool.Close)
	store, err := filesystem.New(storageDir)
	if err != nil {
		t.Fatalf("filesystem store: %v", err)
	}
	t.Setenv("CC_ROOT_KEY", rootKeyB64)
	root, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
	if err != nil {
		t.Fatalf("root key: %v", err)
	}
	cp := crypto.NewProvider(root)
	authz := auth.NewAuthzService(pool, projects.NewStore())
	return services{
		artifacts: artifacts.NewService(pool, cp, store, authz, gates.NewEngine(pool)),
		memories:  memories.NewService(pool, cp, authz),
		secrets:   secrets.NewService(pool, authz, secrets.NewLocalEncryptedBackend(pool, cp)),
	}
}

// TestDumpAndRestoreRoundTrip is the procedure in docs/backup-restore.md,
// against the real schema: encrypted content written through the services,
// the database dumped and restored into an empty one, the object store
// carried over, and everything read back with the same root key. It also
// pins what the procedure relies on: owners survive the restore, migrating
// the restored database again is a no-op, and a different root key reads
// nothing.
func TestDumpAndRestoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	original := dbtest.New(t)
	storageDir := t.TempDir()
	rootKey, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatalf("root key: %v", err)
	}

	principalID, projectID := seedProject(t, original.Owner)

	before := newServices(t, original.App, storageDir, rootKey)
	artifactBody := []byte("artifact body that must survive a restore")
	if _, err := before.artifacts.Write(ctx, principalID, nil, projectID, "notes/backup.md", "text/markdown", artifactBody, 0); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	mem, err := before.memories.Create(ctx, principalID, nil, projectID, memories.NewMemory{
		Kind: "decision", Title: "Backups are tested", Body: "memory body that must survive a restore",
	})
	if err != nil {
		t.Fatalf("create memory: %v", err)
	}
	ref, err := before.secrets.Create(ctx, principalID, projectID, "db-password", []byte("secret value that must survive a restore"))
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}

	restored := dbtest.DumpAndRestore(t, original)

	// The schema must still belong to cc_owner: it is the RLS-exempt owner
	// the policies rely on (docs/adr/0002), and SECURITY DEFINER functions
	// run as whoever owns them. A restore with --no-owner fails here.
	owner, err := pgx.Connect(ctx, restored.Owner)
	if err != nil {
		t.Fatalf("connect to restored database as cc_owner: %v", err)
	}
	defer func() { _ = owner.Close(ctx) }()
	var foreignTables, foreignDefiners int
	if err := owner.QueryRow(ctx,
		`SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tableowner <> 'cc_owner'`,
	).Scan(&foreignTables); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if err := owner.QueryRow(ctx,
		`SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND p.prosecdef AND pg_get_userbyid(p.proowner) <> 'cc_owner'`,
	).Scan(&foreignDefiners); err != nil {
		t.Fatalf("count functions: %v", err)
	}
	if foreignTables != 0 || foreignDefiners != 0 {
		t.Fatalf("after restore, %d tables and %d SECURITY DEFINER functions are not owned by cc_owner", foreignTables, foreignDefiners)
	}

	// `ccd migrate` runs on every start of the compose deployment; on a
	// restored database it must find nothing to do and change nothing.
	if err := db.Migrate(restored.Owner); err != nil {
		t.Fatalf("migrate restored database: %v", err)
	}

	after := newServices(t, restored.App, storageDir, rootKey)
	_, gotArtifact, err := after.artifacts.Read(ctx, principalID, projectID, "notes/backup.md", nil)
	if err != nil {
		t.Fatalf("read artifact after restore: %v", err)
	}
	if !bytes.Equal(gotArtifact, artifactBody) {
		t.Errorf("artifact after restore = %q, want %q", gotArtifact, artifactBody)
	}
	gotMem, err := after.memories.Get(ctx, principalID, projectID, mem.ID)
	if err != nil {
		t.Fatalf("read memory after restore: %v", err)
	}
	if gotMem.Body != mem.Body || gotMem.Title != mem.Title {
		t.Errorf("memory after restore = %q/%q, want %q/%q", gotMem.Title, gotMem.Body, mem.Title, mem.Body)
	}
	gotSecret, err := after.secrets.Resolve(ctx, principalID, projectID, ref)
	if err != nil {
		t.Fatalf("resolve secret after restore: %v", err)
	}
	if string(gotSecret) != "secret value that must survive a restore" {
		t.Errorf("secret after restore = %q", gotSecret)
	}

	// RLS is still in force on the restored copy: someone who is not a
	// member reads nothing.
	stranger, _ := seedProject(t, restored.Owner)
	if _, err := after.memories.Get(ctx, stranger, projectID, mem.ID); err == nil {
		t.Error("a non-member read a memory in the restored database")
	}

	// The same backup with another root key: nothing decrypts, and the error
	// says why rather than looking like corrupt data.
	otherKey, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatalf("root key: %v", err)
	}
	wrong := newServices(t, restored.App, storageDir, otherKey)
	_, _, err = wrong.artifacts.Read(ctx, principalID, projectID, "notes/backup.md", nil)
	if err == nil || !strings.Contains(err.Error(), "wrong CC_ROOT_KEY") {
		t.Errorf("read with a different root key: err = %v, want one naming the wrong CC_ROOT_KEY", err)
	}
	if _, err := wrong.memories.Get(ctx, principalID, projectID, mem.ID); err == nil {
		t.Error("read a memory with a different root key")
	}
	if _, err := wrong.secrets.Resolve(ctx, principalID, projectID, ref); err == nil {
		t.Error("resolved a secret with a different root key")
	}
}

// seedProject inserts a user, its principal, a personal namespace and a
// project the principal owns, as cc_owner (fixture setup bypasses RLS on
// purpose; the behaviour under test then runs as cc_app).
func seedProject(t *testing.T, ownerDSN string) (principalID, projectID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pool, err := db.NewPool(ctx, ownerDSN)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}
	defer pool.Close()

	principalID, projectID = uuid.New(), uuid.New()
	userID, namespaceID := uuid.New(), uuid.New()
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		stmts := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO users (id, email, display_name) VALUES ($1, $2, 'Fixture User')`, []any{userID, userID.String() + "@example.test"}},
			{`INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, []any{principalID, userID}},
			{`INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`, []any{namespaceID, "fixture-" + namespaceID.String()[:8], userID}},
			{`INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'fixture', 'Fixture')`, []any{projectID, namespaceID}},
			{`INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`, []any{projectID, principalID, projects.RoleOwner}},
		}
		for _, s := range stmts {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return principalID, projectID
}
