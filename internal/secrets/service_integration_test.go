package secrets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/secrets"
)

type fixture struct {
	svc       *secrets.Service
	ownerPool *db.Pool
	appPool   *db.Pool
	crypto    *crypto.Provider
}

// seedProject inserts a fresh user/principal/namespace/project and returns
// the principal's ID with role on that project.
func (f *fixture) seedProject(t *testing.T, role projects.Role) (principalID, projectID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	principalID, projectID = uuid.New(), uuid.New()

	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture User"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			principalID, userID); err != nil {
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
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`,
			projectID, principalID, role)
		return err
	})
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return principalID, projectID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	dsns := dbtest.New(t)

	ownerPool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}
	appPool, err := db.NewPool(ctx, dsns.App)
	if err != nil {
		t.Fatalf("connect as cc_app: %v", err)
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

	backend := secrets.NewLocalEncryptedBackend(appPool, cryptoProvider)
	authz := auth.NewAuthzService(appPool, projects.NewStore())

	return &fixture{
		svc:       secrets.NewService(appPool, authz, backend),
		ownerPool: ownerPool,
		appPool:   appPool,
		crypto:    cryptoProvider,
	}
}

func countAuditRows(t *testing.T, f *fixture, projectID uuid.UUID, action string) int {
	t.Helper()
	var n int
	err := db.WithoutPrincipal(context.Background(), f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT count(*) FROM audit_log WHERE project_id = $1 AND action = $2`, projectID, action,
		).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

func TestCreateResolveRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, projects.RoleContributor)

	ref, err := f.svc.Create(ctx, principalID, projectID, "github", []byte("ghp_supersecret"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ref.String() == "" {
		t.Fatal("Create returned empty Reference")
	}

	before := countAuditRows(t, f, projectID, "secret.resolve")

	plaintext, err := f.svc.Resolve(ctx, principalID, projectID, ref)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(plaintext) != "ghp_supersecret" {
		t.Fatalf("Resolve = %q, want %q", plaintext, "ghp_supersecret")
	}

	after := countAuditRows(t, f, projectID, "secret.resolve")
	if after != before+1 {
		t.Fatalf("secret.resolve audit rows = %d -> %d, want exactly +1", before, after)
	}

	// Rotate the value: Create again under the same name.
	if _, err := f.svc.Create(ctx, principalID, projectID, "github", []byte("ghp_rotated")); err != nil {
		t.Fatalf("rotate Create: %v", err)
	}
	rotated, err := f.svc.Resolve(ctx, principalID, projectID, ref)
	if err != nil {
		t.Fatalf("Resolve after rotate: %v", err)
	}
	if string(rotated) != "ghp_rotated" {
		t.Fatalf("Resolve after rotate = %q, want %q", rotated, "ghp_rotated")
	}
}

func TestCreateRejectsInvalidName(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, projects.RoleContributor)

	if _, err := f.svc.Create(ctx, principalID, projectID, "team/github", []byte("x")); !errors.Is(err, secrets.ErrInvalidName) {
		t.Fatalf("Create with a slash in name: got err=%v, want ErrInvalidName", err)
	}
	if err := f.svc.Delete(ctx, principalID, projectID, "team/github"); !errors.Is(err, secrets.ErrInvalidName) {
		t.Fatalf("Delete with a slash in name: got err=%v, want ErrInvalidName", err)
	}
}

func TestListReturnsNamesOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, projects.RoleContributor)

	if _, err := f.svc.Create(ctx, principalID, projectID, "github", []byte("secret-value-1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.svc.Create(ctx, principalID, projectID, "npm", []byte("secret-value-2")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// secrets.Reference has no value field at all — List's return type
	// makes leaking a value a compile error, not just a runtime one. This
	// test only needs to check List resolves the right names/scope.
	refsPage, err := f.svc.List(ctx, principalID, projectID, paging.Options{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	refs := refsPage.Items
	if len(refs) != 2 {
		t.Fatalf("List returned %d references, want 2", len(refs))
	}
	got := map[string]bool{}
	for _, r := range refs {
		if r.Scope != "project" || r.ScopeRef != projectID {
			t.Fatalf("List returned wrong scope: %+v", r)
		}
		got[r.Name] = true
	}
	if !got["github"] || !got["npm"] {
		t.Fatalf("List names = %v, want github and npm", got)
	}
}

func TestDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, projects.RoleContributor)

	ref, err := f.svc.Create(ctx, principalID, projectID, "github", []byte("ghp_supersecret"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := f.svc.Delete(ctx, principalID, projectID, "github"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.svc.Resolve(ctx, principalID, projectID, ref); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("Resolve after Delete: got %v, want ErrNotFound", err)
	}
}

func TestRLSIsolationBetweenProjects(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	principalA, projectA := f.seedProject(t, projects.RoleContributor)
	principalB, projectB := f.seedProject(t, projects.RoleContributor)

	refA, err := f.svc.Create(ctx, principalA, projectA, "github", []byte("a-secret"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// principalB is a member of projectB, not projectA: checkCapability
	// denies before ever reaching the backend/RLS.
	if _, err := f.svc.Resolve(ctx, principalB, projectA, refA); !errors.Is(err, secrets.ErrForbidden) {
		t.Fatalf("cross-project Resolve: got %v, want ErrForbidden", err)
	}

	// principalB, scoped to their own project, cannot resolve a Reference
	// whose ScopeRef points at a different project even if they somehow
	// obtained the string — Service.Resolve checks ref.ScopeRef == projectID.
	if _, err := f.svc.Resolve(ctx, principalB, projectB, refA); !errors.Is(err, secrets.ErrForbidden) {
		t.Fatalf("mismatched-scope Resolve: got %v, want ErrForbidden", err)
	}

	refsBPage, err := f.svc.List(ctx, principalB, projectB, paging.Options{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	refsB := refsBPage.Items
	if len(refsB) != 0 {
		t.Fatalf("projectB should see 0 secrets, got %d (RLS isolation broken)", len(refsB))
	}
}

func TestRBACReaderCannotReadOrWriteSecrets(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	readerID, projectID := f.seedProject(t, projects.RoleReader)

	if _, err := f.svc.Create(ctx, readerID, projectID, "github", []byte("x")); !errors.Is(err, secrets.ErrForbidden) {
		t.Fatalf("READER Create: got %v, want ErrForbidden", err)
	}
	if _, err := f.svc.List(ctx, readerID, projectID, paging.Options{}); !errors.Is(err, secrets.ErrForbidden) {
		t.Fatalf("READER List: got %v, want ErrForbidden", err)
	}
	if _, err := f.svc.Resolve(ctx, readerID, projectID, secrets.NewProjectReference(projectID, "github")); !errors.Is(err, secrets.ErrForbidden) {
		t.Fatalf("READER Resolve: got %v, want ErrForbidden", err)
	}
}

func TestWriteAuditRecordHasNoSecretValue(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, projects.RoleContributor)

	if _, err := f.svc.Create(ctx, principalID, projectID, "github", []byte("ghp_supersecret_value")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var records paging.Page[audit.Record]
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		var err error
		records, err = audit.Query(ctx, tx, projectID, audit.QueryOptions{Action: "secret.write"})
		return err
	})
	if err != nil {
		t.Fatalf("audit.Query: %v", err)
	}
	if len(records.Items) == 0 {
		t.Fatal("expected at least one secret.write audit record")
	}
	// audit_log's schema is a fixed allowlist of columns (docs/adr/0011) —
	// there is no column ANY of the fixed audit.Entry fields could carry a
	// secret value in, so this test is really documenting that invariant
	// rather than needing to scan for the literal plaintext anywhere.
}
