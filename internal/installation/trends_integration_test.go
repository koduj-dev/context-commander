package installation_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/installation"
)

// authenticate issues a token for principalID and presents it twice, the way
// a client making two requests on one day would.
func (f *fixture) authenticate(principalID uuid.UUID) {
	f.t.Helper()
	var token string
	err := db.WithoutPrincipal(f.ctx, f.appPool, func(tx pgx.Tx) error {
		var err error
		token, _, err = f.identity.IssueCredential(f.ctx, tx, principalID, "test", nil)
		return err
	})
	if err != nil {
		f.t.Fatalf("issue credential: %v", err)
	}
	for range 2 {
		if _, err := identity.Authenticate(f.ctx, f.appPool, token); err != nil {
			f.t.Fatalf("authenticate: %v", err)
		}
	}
}

// seedHandoffs gives a project the admin cannot see three handoffs with
// known timings:
//
//	taken over: created 2h ago by client A, claimed 1h ago by client B,
//	            closed 30 min ago — the cross-client loop
//	same client: created 3h ago and claimed 15 min later, both by client A
//	stale: open, created 8 days ago
//
// and backdates the project a day, so "time to first takeover" is 23 hours.
func (f *fixture) seedHandoffs(owner principal, projectID uuid.UUID) {
	f.t.Helper()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx,
			`UPDATE projects SET created_at = now() - interval '1 day' WHERE id = $1`, projectID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO project_keys (project_id, key_version, wrapped_key, root_key_ref)
			 VALUES ($1, 1, '\x00', 'test')`, projectID); err != nil {
			return err
		}
		clientA, clientB := uuid.New(), uuid.New()
		for _, id := range []uuid.UUID{clientA, clientB} {
			if _, err := tx.Exec(f.ctx,
				`INSERT INTO client_credentials (id, principal_id, client_type, token_hash)
				 VALUES ($1, $2, 'test', 'unused')`, id, owner.principalID); err != nil {
				return err
			}
		}
		_, err := tx.Exec(f.ctx, `
			INSERT INTO handoffs (project_id, created_by_principal, created_by_client, status,
			                      ciphertext, nonce, wrapped_dek, project_key_version, created_at,
			                      claimed_at, claimed_by_principal, claimed_by_client,
			                      closed_at, closed_by_principal, closed_by_client)
			VALUES
			  ($1, $2, $3, 'closed', '\x00', '\x00', '\x00', 1, now() - interval '2 hours',
			   now() - interval '1 hour', $2, $4, now() - interval '30 minutes', $2, $4),
			  ($1, $2, $3, 'claimed', '\x00', '\x00', '\x00', 1, now() - interval '3 hours',
			   now() - interval '165 minutes', $2, $3, NULL, NULL, NULL),
			  ($1, $2, $3, 'open', '\x00', '\x00', '\x00', 1, now() - interval '8 days',
			   NULL, NULL, NULL, NULL, NULL, NULL)`,
			projectID, owner.principalID, clientA, clientB)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed handoffs: %v", err)
	}
}

func TestTrendsMeasureTheHandoffLoopInProjectsTheAdminCannotSee(t *testing.T) {
	f := newFixture(t)
	admin := f.newUser("trends-admin@example.test")
	f.grant(admin.userID)
	stranger := f.newUser("trends-stranger@example.test")
	f.seedHandoffs(stranger, f.seedForeignProject(stranger))

	// Three weeks always contains everything seeded, whatever weekday this
	// runs on: the oldest row is eight days old.
	weeks, flow, err := f.svc.Trends(f.ctx, admin.principalID, admin.userID, 3)
	if err != nil {
		t.Fatalf("Trends: %v", err)
	}

	if len(weeks) != 3 {
		t.Fatalf("got %d weeks, want 3 — empty weeks must be present, not skipped", len(weeks))
	}
	var created, claimed, other, closed int64
	for i, w := range weeks {
		if w.Start.Weekday() != time.Monday {
			t.Errorf("week %d starts on %s, want Monday", i, w.Start.Weekday())
		}
		if i > 0 && !w.Start.After(weeks[i-1].Start) {
			t.Errorf("weeks are not oldest first: %v then %v", weeks[i-1].Start, w.Start)
		}
		created += w.HandoffsCreated
		claimed += w.HandoffsClaimed
		other += w.HandoffsClaimedByOtherClient
		closed += w.HandoffsClosed
	}
	if created != 3 || claimed != 2 || other != 1 || closed != 1 {
		t.Errorf("weekly sums: created=%d claimed=%d other=%d closed=%d, want 3/2/1/1",
			created, claimed, other, closed)
	}

	near := func(name string, got *time.Duration, want time.Duration) {
		t.Helper()
		if got == nil {
			t.Errorf("%s = nil, want %v", name, want)
			return
		}
		if d := *got - want; d < -time.Minute || d > time.Minute {
			t.Errorf("%s = %v, want about %v", name, *got, want)
		}
	}
	if flow.Handoffs != 3 || flow.Claimed != 2 || flow.ClaimedByOtherClient != 1 || flow.StaleOpen != 1 {
		t.Errorf("flow counts = %+v", flow)
	}
	// Claims took 60 and 15 minutes: the median is between them, and p90
	// interpolates towards the slower one.
	near("MedianToClaim", flow.MedianToClaim, 37*time.Minute+30*time.Second)
	near("P90ToClaim", flow.P90ToClaim, 55*time.Minute+30*time.Second)
	near("MedianClaimToClose", flow.MedianClaimToClose, 30*time.Minute)
	if flow.Projects != 1 || flow.ProjectsWithTakeover != 1 {
		t.Errorf("projects = %d with takeover %d, want 1 and 1", flow.Projects, flow.ProjectsWithTakeover)
	}
	near("MedianToFirstTakeover", flow.MedianToFirstTakeover, 23*time.Hour)
}

// Active principals are counted per week from authenticated requests, users
// and agents apart, each once however many requests it made.
func TestTrendsCountActivePrincipalsOncePerWeek(t *testing.T) {
	f := newFixture(t)
	admin := f.newUser("active-admin@example.test")
	f.grant(admin.userID)
	f.newUser("idle@example.test") // exists, never authenticates

	var agent uuid.UUID
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		sp, err := f.identity.CreateServicePrincipal(f.ctx, tx, "trend-agent", "", nil)
		if err != nil {
			return err
		}
		p, err := f.identity.CreatePrincipalForService(f.ctx, tx, sp.ID)
		agent = p.ID
		return err
	})
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	f.authenticate(admin.principalID)
	f.authenticate(agent)

	weeks, _, err := f.svc.Trends(f.ctx, admin.principalID, admin.userID, 1)
	if err != nil {
		t.Fatalf("Trends: %v", err)
	}
	if len(weeks) != 1 {
		t.Fatalf("got %d weeks, want 1", len(weeks))
	}
	if weeks[0].ActiveUsers != 1 || weeks[0].ActiveAgents != 1 {
		t.Errorf("this week: %d users, %d agents active; want 1 and 1",
			weeks[0].ActiveUsers, weeks[0].ActiveAgents)
	}
	if weeks[0].MedianToClaim != nil {
		t.Errorf("a week with no claims reports a median of %v, want none", *weeks[0].MedianToClaim)
	}
}

// The application writes who was active when and can never read it back:
// the only way out is a count. A SELECT grant added later "for a report"
// would turn the table into a per-person activity log for anyone who finds
// a query to run through cc_app.
func TestActivityDaysAreUnreadableToTheApplication(t *testing.T) {
	f := newFixture(t)
	u := f.newUser("unreadable@example.test")
	f.authenticate(u.principalID)

	err := db.WithoutPrincipal(f.ctx, f.appPool, func(tx pgx.Tx) error {
		var n int64
		return tx.QueryRow(f.ctx, `SELECT count(*) FROM principal_activity_days`).Scan(&n)
	})
	if err == nil {
		t.Fatal("cc_app read principal_activity_days; it must only be able to insert")
	}
}

func TestTrendsRefuseANonAdmin(t *testing.T) {
	f := newFixture(t)
	plain := f.newUser("trends-plain@example.test")

	_, _, err := f.svc.Trends(f.ctx, plain.principalID, plain.userID, 12)
	if !errors.Is(err, installation.ErrForbidden) {
		t.Fatalf("Trends as non-admin: got %v, want ErrForbidden", err)
	}
}
