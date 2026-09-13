package events_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type fixture struct {
	svc         *events.Service
	ownerPool   *db.Pool
	appPool     *db.Pool
	projectID   uuid.UUID
	principalID uuid.UUID
}

// newFixture seeds one project with one OWNER principal, through the
// RLS-exempt owner pool, then wires Service to cc_app — the same role the
// production server connects as, so List genuinely exercises RLS
// (docs/adr/0002's testing requirement).
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

	return &fixture{
		svc:         events.NewService(appPool, auth.NewAuthzService(appPool, projects.NewStore())),
		ownerPool:   ownerPool,
		appPool:     appPool,
		projectID:   projectID,
		principalID: principalID,
	}
}

// publish inserts n events directly through the outbox primitive, as a
// domain mutation would, inside its own transaction against the app pool
// (not the owner pool) so RLS applies exactly as it would in production.
func (f *fixture) publish(t *testing.T, ctx context.Context, n int) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.principalID}, func(tx pgx.Tx) error {
		for i := 0; i < n; i++ {
			id, err := events.Publish(ctx, tx, f.projectID, "fixture.event", "fixture", nil, events.PayloadKindMetadataOnly)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return ids
}

func TestPublishAndListSinceOrdering(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	ids := f.publish(t, ctx, 3)

	var got []events.Event
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.principalID}, func(tx pgx.Tx) error {
		var err error
		got, err = events.ListSince(ctx, tx, f.projectID, 0, 10)
		return err
	})
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListSince returned %d events, want 3", len(got))
	}
	for i, e := range got {
		if e.EventID != ids[i] {
			t.Errorf("event %d = %s, want %s (ListSince must return seq-ascending order)", i, e.EventID, ids[i])
		}
		if e.Seq <= 0 {
			t.Errorf("event %d has non-positive seq %d", i, e.Seq)
		}
	}

	// afterSeq pagination: re-fetching after the first event's seq must
	// yield only the remaining two.
	page2, err := listSince(ctx, f, got[0].Seq, 10)
	if err != nil {
		t.Fatalf("ListSince page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("ListSince(afterSeq=%d) returned %d events, want 2", got[0].Seq, len(page2))
	}

	// limit is honored.
	limited, err := listSince(ctx, f, 0, 1)
	if err != nil {
		t.Fatalf("ListSince limited: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("ListSince(limit=1) returned %d events, want 1", len(limited))
	}
}

func listSince(ctx context.Context, f *fixture, afterSeq int64, limit int) ([]events.Event, error) {
	var out []events.Event
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.principalID}, func(tx pgx.Tx) error {
		var err error
		out, err = events.ListSince(ctx, tx, f.projectID, afterSeq, limit)
		return err
	})
	return out, err
}

func TestListSinceIsolatedAcrossProjects(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.publish(t, ctx, 2)

	otherProjectID := uuid.New()
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.principalID}, func(tx pgx.Tx) error {
		got, err := events.ListSince(ctx, tx, otherProjectID, 0, 10)
		if err != nil {
			return err
		}
		if len(got) != 0 {
			t.Errorf("ListSince for an unrelated project returned %d events, want 0", len(got))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ListSince other project: %v", err)
	}
}

func TestServiceListDeniedForNonMember(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.publish(t, ctx, 1)

	outsiderID := uuid.New()
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Outsider"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, outsiderID, userID)
		return err
	})
	if err != nil {
		t.Fatalf("seed outsider: %v", err)
	}

	_, err = f.svc.List(ctx, outsiderID, f.projectID, paging.Options{Limit: 10})
	if !errors.Is(err, events.ErrForbidden) {
		t.Fatalf("List by non-member: got err=%v, want ErrForbidden", err)
	}
}

func TestServiceListAllowedForMember(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.publish(t, ctx, 2)

	gotPage, err := f.svc.List(ctx, f.principalID, f.projectID, paging.Options{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := gotPage.Items
	if len(got) != 2 {
		t.Fatalf("List returned %d events, want 2", len(got))
	}
}

// TestRLSIsolationBetweenProjects pins the database half for domain_events.
// Every other test here goes through the Service, whose capability check and
// explicit project filter give the right answer with RLS switched off
// entirely — so none of them actually exercises the policy (docs/adr/0002
// calls it load-bearing defense in depth). This one queries through cc_app
// with no project filter of its own, which is the only shape that can
// observe it.
func TestRLSIsolationBetweenProjects(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.publish(t, ctx, 1)

	outsiderID := uuid.New()
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Outsider"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, outsiderID, userID)
		return err
	})
	if err != nil {
		t.Fatalf("seed outsider: %v", err)
	}

	err = db.WithPrincipal(ctx, f.appPool, db.Principal{ID: outsiderID}, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM domain_events`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatalf("domain_events: a non-member's session sees %d row(s) — RLS is not isolating this table", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("count domain_events through cc_app: %v", err)
	}

	// And the member still sees their own, so the policy isolates rather
	// than hiding everything from everyone.
	err = db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.principalID}, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM domain_events`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			t.Fatal("the project's own member sees no events")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("count as member: %v", err)
	}
}
