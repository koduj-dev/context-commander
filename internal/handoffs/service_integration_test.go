package handoffs_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type fixture struct {
	svc     *handoffs.Service
	appPool *db.Pool
}

func (f *fixture) seedProject(t *testing.T, ownerPool *db.Pool, role projects.Role) (principalID, projectID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	principalID, projectID = uuid.New(), uuid.New()

	err := db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
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

func newFixture(t *testing.T) (*fixture, *db.Pool) {
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
	authz := auth.NewAuthzService(appPool, projects.NewStore())

	return &fixture{
		svc:     handoffs.NewService(appPool, cryptoProvider, authz),
		appPool: appPool,
	}, ownerPool
}

func TestCreateGetRoundTrip(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	body := handoffs.Body{
		Summary:             "did the thing",
		CompletedWork:       "wrote the code",
		NextActions:         "review and merge",
		ReferencedArtifacts: []uuid.UUID{uuid.New()},
	}
	h, err := f.svc.Create(ctx, principalID, nil, projectID, nil, body)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if h.Status != "open" {
		t.Fatalf("Status = %q, want open", h.Status)
	}

	got, gotBody, err := f.svc.Get(ctx, principalID, projectID, h.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != h.ID {
		t.Fatalf("Get returned wrong handoff: %+v", got)
	}
	if gotBody.Summary != body.Summary || gotBody.CompletedWork != body.CompletedWork || gotBody.NextActions != body.NextActions {
		t.Fatalf("decrypted body = %+v, want %+v", gotBody, body)
	}
	if len(gotBody.ReferencedArtifacts) != 1 || gotBody.ReferencedArtifacts[0] != body.ReferencedArtifacts[0] {
		t.Fatalf("decrypted ReferencedArtifacts = %+v, want %+v", gotBody.ReferencedArtifacts, body.ReferencedArtifacts)
	}
}

func TestListFiltersByStatus(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	if _, err := f.svc.Create(ctx, principalID, nil, projectID, nil, handoffs.Body{Summary: "one"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.svc.Create(ctx, principalID, nil, projectID, nil, handoffs.Body{Summary: "two"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	open := "open"
	listPage, err := f.svc.List(ctx, principalID, projectID, &open, paging.Options{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	list := listPage.Items
	if len(list) != 2 {
		t.Fatalf("List returned %d handoffs, want 2", len(list))
	}

	closed := "closed"
	nonePage, err := f.svc.List(ctx, principalID, projectID, &closed, paging.Options{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	none := nonePage.Items
	if len(none) != 0 {
		t.Fatalf("List(closed) returned %d handoffs, want 0", len(none))
	}
}

func TestRBACReaderCannotCreate(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	readerID, projectID := f.seedProject(t, ownerPool, projects.RoleReader)

	if _, err := f.svc.Create(ctx, readerID, nil, projectID, nil, handoffs.Body{Summary: "x"}); !errors.Is(err, handoffs.ErrForbidden) {
		t.Fatalf("READER Create: got %v, want ErrForbidden", err)
	}
}

func TestRLSIsolationBetweenProjects(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalA, projectA := f.seedProject(t, ownerPool, projects.RoleContributor)
	principalB, projectB := f.seedProject(t, ownerPool, projects.RoleContributor)

	h, err := f.svc.Create(ctx, principalA, nil, projectA, nil, handoffs.Body{Summary: "secret to A"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, _, err := f.svc.Get(ctx, principalB, projectA, h.ID); !errors.Is(err, handoffs.ErrForbidden) {
		t.Fatalf("cross-project Get: got %v, want ErrForbidden", err)
	}

	listBPage, err := f.svc.List(ctx, principalB, projectB, nil, paging.Options{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	listB := listBPage.Items
	if len(listB) != 0 {
		t.Fatalf("projectB should see 0 handoffs, got %d", len(listB))
	}

	// Everything above exercises internal/handoffs.Service — its capability
	// check refuses the cross-project Get, and its List carries an explicit
	// "WHERE project_id = $1". Neither depends on Row-Level Security at all:
	// this test passed unchanged with the handoffs policy replaced by
	// "USING (true)", despite its name.
	//
	// The assertion below is the one that actually exercises RLS, and it is
	// the whole reason RLS exists (docs/adr/0002: the database mirrors the
	// same decision as defense in depth). It goes through cc_app — the role
	// the production server connects as — with principalB's identity set,
	// and deliberately omits any project filter of its own. If the policy
	// stops working, the row shows up here and nowhere else.
	assertNoRowsVisible(t, ctx, f.appPool, principalB, "handoffs")
}

// assertNoRowsVisible counts an entire table through cc_app as principalID,
// with no WHERE clause to do the filtering for it. Any row that comes back
// was let through by Row-Level Security itself.
func assertNoRowsVisible(t *testing.T, ctx context.Context, appPool *db.Pool, principalID uuid.UUID, table string) {
	t.Helper()
	err := db.WithPrincipal(ctx, appPool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var n int
		// The table name is a constant from the caller, never test input.
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatalf("%s: cc_app sees %d row(s) belonging to another project — RLS is not isolating this table", table, n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("count %s through cc_app: %v", table, err)
	}
}
