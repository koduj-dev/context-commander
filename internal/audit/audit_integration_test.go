package audit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type fixture struct {
	svc            *audit.Service
	crypto         *crypto.Provider
	ownerPool      *db.Pool
	appPool        *db.Pool
	projectID      uuid.UUID
	ownerPrincipal uuid.UUID
}

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

	ownerPrincipal := uuid.New()
	projectID := uuid.New()
	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture Owner"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			ownerPrincipal, userID); err != nil {
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
			projectID, ownerPrincipal)
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

	return &fixture{
		svc:            audit.NewService(appPool, auth.NewAuthzService(appPool, projects.NewStore())),
		crypto:         cryptoProvider,
		ownerPool:      ownerPool,
		appPool:        appPool,
		projectID:      projectID,
		ownerPrincipal: ownerPrincipal,
	}
}

// addMember seeds an additional project member with the given role, via
// the RLS-exempt owner pool (fixture setup only).
func (f *fixture) addMember(t *testing.T, ctx context.Context, role projects.Role) uuid.UUID {
	t.Helper()
	principalID := uuid.New()
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture Member"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, principalID, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`,
			f.projectID, principalID, role)
		return err
	})
	if err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return principalID
}

func (f *fixture) write(t *testing.T, ctx context.Context, action, resourceType string) {
	t.Helper()
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &f.projectID, PrincipalID: &f.ownerPrincipal,
			Action: action, ResourceType: resourceType, Result: audit.ResultAllow,
		})
	})
	if err != nil {
		t.Fatalf("audit.Write: %v", err)
	}
}

func TestWriteAndQueryRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.write(t, ctx, "artifact.write", "artifact_version")
	f.write(t, ctx, "artifact.delete", "artifact_version")
	f.write(t, ctx, "handoff.create", "handoff")

	var all paging.Page[audit.Record]
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		var err error
		all, err = audit.Query(ctx, tx, f.projectID, audit.QueryOptions{})
		return err
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(all.Items) != 3 {
		t.Fatalf("Query returned %d rows, want 3", len(all.Items))
	}
	// most-recent-first
	if all.Items[0].Action != "handoff.create" {
		t.Errorf("first row action = %q, want handoff.create (most recent first)", all.Items[0].Action)
	}

	var filtered paging.Page[audit.Record]
	err = db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		var err error
		filtered, err = audit.Query(ctx, tx, f.projectID, audit.QueryOptions{ResourceType: "artifact_version"})
		return err
	})
	if err != nil {
		t.Fatalf("Query filtered: %v", err)
	}
	if len(filtered.Items) != 2 {
		t.Fatalf("Query(resource_type=artifact_version) returned %d rows, want 2", len(filtered.Items))
	}

	var byAction paging.Page[audit.Record]
	err = db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		var err error
		byAction, err = audit.Query(ctx, tx, f.projectID, audit.QueryOptions{Action: "artifact.delete"})
		return err
	})
	if err != nil {
		t.Fatalf("Query by action: %v", err)
	}
	if len(byAction.Items) != 1 {
		t.Fatalf("Query(action=artifact.delete) returned %d rows, want 1", len(byAction.Items))
	}
}

func TestQueryTimeRangeFilter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.write(t, ctx, "artifact.write", "artifact_version")

	future := time.Now().Add(time.Hour)
	var empty paging.Page[audit.Record]
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		var err error
		empty, err = audit.Query(ctx, tx, f.projectID, audit.QueryOptions{Since: &future})
		return err
	})
	if err != nil {
		t.Fatalf("Query since future: %v", err)
	}
	if len(empty.Items) != 0 {
		t.Fatalf("Query(since=future) returned %d rows, want 0", len(empty.Items))
	}
}

func TestWriteWithPayloadRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	resourceID := uuid.New()
	plaintext := []byte(`{"diff":"before -> after"}`)

	var auditLogID int64
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		if err := audit.WriteWithPayload(ctx, tx, audit.Entry{
			ProjectID: &f.projectID, PrincipalID: &f.ownerPrincipal,
			Action: "handoff.create", ResourceType: "handoff", ResourceID: &resourceID,
			Result: audit.ResultAllow,
		}, plaintext, f.crypto); err != nil {
			return err
		}
		rows, err := audit.Query(ctx, tx, f.projectID, audit.QueryOptions{Action: "handoff.create"})
		if err != nil {
			return err
		}
		if len(rows.Items) != 1 {
			t.Fatalf("expected 1 row after WriteWithPayload, got %d", len(rows.Items))
		}
		auditLogID = rows.Items[0].ID
		return nil
	})
	if err != nil {
		t.Fatalf("WriteWithPayload: %v", err)
	}

	var got []byte
	err = db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		var err error
		got, err = audit.ReadPayload(ctx, tx, f.crypto, f.projectID, auditLogID, resourceID)
		return err
	})
	if err != nil {
		t.Fatalf("ReadPayload: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("ReadPayload = %q, want %q", got, plaintext)
	}
}

func TestQueryIsolatedAcrossProjects(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.write(t, ctx, "artifact.write", "artifact_version")

	otherProjectID := uuid.New()
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		rows, err := audit.Query(ctx, tx, otherProjectID, audit.QueryOptions{})
		if err != nil {
			return err
		}
		if len(rows.Items) != 0 {
			t.Errorf("Query for an unrelated project returned %d rows, want 0", len(rows.Items))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Query other project: %v", err)
	}
}

// TestQueryCursorPagesBackward is the cursor the web UI's Audit tab "Load
// more" button relies on: walking with limit=1 and the previous page's
// NextCursor must visit every row exactly once, in the same order a single
// unpaginated call returns them.
//
// The last page carries the load-bearing assertion. NextCursor must be
// empty there — not a cursor that merely happens to return nothing — because
// the UI shows or hides "Load more" on that emptiness, so a service that
// always hands one back leaves the button on screen forever.
func TestQueryCursorPagesBackward(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.write(t, ctx, "artifact.write", "artifact_version")
	f.write(t, ctx, "artifact.delete", "artifact_version")
	f.write(t, ctx, "handoff.create", "handoff")

	var want paging.Page[audit.Record]
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		var err error
		want, err = audit.Query(ctx, tx, f.projectID, audit.QueryOptions{})
		return err
	})
	if err != nil {
		t.Fatalf("Query (single page): %v", err)
	}
	if len(want.Items) != 3 {
		t.Fatalf("Query (single page) returned %d rows, want 3", len(want.Items))
	}
	if want.NextCursor != "" {
		t.Errorf("a page holding every row still offered NextCursor %q", want.NextCursor)
	}

	var got []audit.Record
	err = db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		cursor := ""
		for i := range want.Items {
			page, err := audit.Query(ctx, tx, f.projectID, audit.QueryOptions{
				Paging: paging.Options{Limit: 1, Cursor: cursor},
			})
			if err != nil {
				return err
			}
			if len(page.Items) != 1 {
				t.Fatalf("page %d: got %d rows, want 1", i, len(page.Items))
			}
			got = append(got, page.Items[0])

			onLastRow := i == len(want.Items)-1
			if onLastRow && page.NextCursor != "" {
				t.Errorf("final page offered NextCursor %q, want none", page.NextCursor)
			}
			if !onLastRow && page.NextCursor == "" {
				t.Fatalf("page %d offered no NextCursor but %d rows remain", i, len(want.Items)-1-i)
			}
			cursor = page.NextCursor
		}
		return nil
	})
	if err != nil {
		t.Fatalf("paginate: %v", err)
	}

	if len(got) != len(want.Items) {
		t.Fatalf("paginated through %d rows, want %d", len(got), len(want.Items))
	}
	for i := range want.Items {
		if got[i].ID != want.Items[i].ID {
			t.Errorf("page %d id = %d, want %d (order must match the single-page Query)", i, got[i].ID, want.Items[i].ID)
		}
	}
}

func TestServiceQueryDeniedForContributor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.write(t, ctx, "artifact.write", "artifact_version")

	contributorID := f.addMember(t, ctx, projects.RoleContributor)

	_, err := f.svc.Query(ctx, contributorID, f.projectID, audit.QueryOptions{})
	if !errors.Is(err, audit.ErrForbidden) {
		t.Fatalf("Query by CONTRIBUTOR: got err=%v, want ErrForbidden (audit.read is ADMIN/OWNER-only)", err)
	}
}

func TestServiceQueryAllowedForOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.write(t, ctx, "artifact.write", "artifact_version")

	rows, err := f.svc.Query(ctx, f.ownerPrincipal, f.projectID, audit.QueryOptions{})
	if err != nil {
		t.Fatalf("Query by OWNER: %v", err)
	}
	if len(rows.Items) != 1 {
		t.Fatalf("Query returned %d rows, want 1", len(rows.Items))
	}
}

// seedOutsiderPrincipal creates a principal that is a member of nothing.
func (f *fixture) seedOutsiderPrincipal(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	principalID := uuid.New()
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Outsider"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, principalID, userID)
		return err
	})
	if err != nil {
		t.Fatalf("seed outsider: %v", err)
	}
	return principalID
}

// TestRLSIsolationBetweenProjects pins the database half for audit_log, the
// table whose whole purpose is to record who did what — and therefore the
// one where a policy regression would be worst.
//
// Every other test in this package goes through Service.Query, which filters
// by project in SQL and so gives the right answer with RLS switched off. This
// one counts the table through cc_app with no filter of its own, which is
// the only shape that can observe the policy (docs/adr/0002).
func TestRLSIsolationBetweenProjects(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &f.projectID, PrincipalID: &f.ownerPrincipal,
			Action: "test.action", ResourceType: "test", Result: audit.ResultAllow,
		})
	})
	if err != nil {
		t.Fatalf("write audit row: %v", err)
	}

	outsider := f.seedOutsiderPrincipal(t)
	err = db.WithPrincipal(ctx, f.appPool, db.Principal{ID: outsider}, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatalf("audit_log: a non-member's session sees %d row(s) — RLS is not isolating the record of who did what", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("count audit_log through cc_app: %v", err)
	}

	// The project's own member still sees it, so the policy isolates rather
	// than hiding everything from everyone.
	err = db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			t.Fatal("the project's own member sees no audit rows")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("count as member: %v", err)
	}
}
