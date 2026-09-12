package handoffs_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// create is the three-line setup every lifecycle test below opens with.
func create(t *testing.T, f *fixture, principalID, projectID uuid.UUID, summary string) handoffs.Handoff {
	t.Helper()
	h, err := f.svc.Create(context.Background(), principalID, nil, projectID, nil, handoffs.Body{Summary: summary})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return h
}

func TestClaimThenClose(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	// clientID stays nil here: created_by_client/claimed_by_client are real
	// foreign keys into client_credentials, and this fixture seeds a
	// principal without issuing one. Attribution to a concrete client is
	// covered end to end in adapters/http, where the requests carry actual
	// tokens.
	h := create(t, f, principalID, projectID, "migrate the index mappings")
	if h.Status != handoffs.StatusOpen {
		t.Fatalf("Create status = %q, want %q", h.Status, handoffs.StatusOpen)
	}

	claimed, err := f.svc.Claim(ctx, principalID, nil, projectID, h.ID)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed.Status != handoffs.StatusClaimed {
		t.Fatalf("Claim status = %q, want %q", claimed.Status, handoffs.StatusClaimed)
	}
	// Attribution is the point of the transition, not a side effect: a
	// claim nobody can be traced to would leave the next agent unable to
	// tell whether the work is actually being continued.
	if claimed.ClaimedAt == nil || claimed.ClaimedByPrincipal == nil || *claimed.ClaimedByPrincipal != principalID {
		t.Fatalf("Claim did not record attribution: claimed_at=%v by=%v", claimed.ClaimedAt, claimed.ClaimedByPrincipal)
	}

	closed, err := f.svc.Close(ctx, principalID, nil, projectID, h.ID)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed.Status != handoffs.StatusClosed {
		t.Fatalf("Close status = %q, want %q", closed.Status, handoffs.StatusClosed)
	}
	if closed.ClosedAt == nil {
		t.Fatal("Close did not record closed_at")
	}
	// The claim survives the close — a closed handoff must still say which
	// client actually did the work.
	if closed.ClaimedAt == nil {
		t.Fatal("closing cleared claimed_at; the claim attribution must survive")
	}
}

func TestCloseDirectlyFromOpen(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	h := create(t, f, principalID, projectID, "obsolete plan")
	closed, err := f.svc.Close(ctx, principalID, nil, projectID, h.ID)
	if err != nil {
		t.Fatalf("Close from open: %v", err)
	}
	if closed.Status != handoffs.StatusClosed {
		t.Fatalf("status = %q, want %q", closed.Status, handoffs.StatusClosed)
	}
	// Nobody claimed it, so nothing may claim otherwise.
	if closed.ClaimedAt != nil {
		t.Fatalf("closing an unclaimed handoff invented a claim: %v", closed.ClaimedAt)
	}
}

func TestClaimTwiceIsAlreadyClaimed(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	h := create(t, f, principalID, projectID, "one job")
	if _, err := f.svc.Claim(ctx, principalID, nil, projectID, h.ID); err != nil {
		t.Fatalf("first Claim: %v", err)
	}
	_, err := f.svc.Claim(ctx, principalID, nil, projectID, h.ID)
	if !errors.Is(err, handoffs.ErrAlreadyClaimed) {
		t.Fatalf("second Claim error = %v, want ErrAlreadyClaimed", err)
	}
}

func TestClaimClosedIsInvalidTransition(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	h := create(t, f, principalID, projectID, "done already")
	if _, err := f.svc.Close(ctx, principalID, nil, projectID, h.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := f.svc.Claim(ctx, principalID, nil, projectID, h.ID)
	if !errors.Is(err, handoffs.ErrInvalidTransition) {
		t.Fatalf("Claim after close error = %v, want ErrInvalidTransition", err)
	}
	// Specifically NOT ErrAlreadyClaimed: "someone else is on this" and
	// "this is finished" call for different responses from an agent.
	if errors.Is(err, handoffs.ErrAlreadyClaimed) {
		t.Fatal("claiming a closed handoff reported ErrAlreadyClaimed, which would tell the caller to wait for work that is already done")
	}

	_, err = f.svc.Close(ctx, principalID, nil, projectID, h.ID)
	if !errors.Is(err, handoffs.ErrInvalidTransition) {
		t.Fatalf("double Close error = %v, want ErrInvalidTransition", err)
	}
}

// TestConcurrentClaimsElectExactlyOneWinner is the reason the status
// precondition lives in the UPDATE's WHERE clause instead of a
// read-then-write pair. Two clients racing to continue the same work is the
// expected collision in a cross-client handoff, and both "winning" would
// mean two agents silently doing the same job.
func TestConcurrentClaimsElectExactlyOneWinner(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	h := create(t, f, principalID, projectID, "contended")

	const racers = 8
	var wg sync.WaitGroup
	results := make([]error, racers)
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, results[i] = f.svc.Claim(ctx, principalID, nil, projectID, h.ID)
		}()
	}
	close(start)
	wg.Wait()

	winners := 0
	for i, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, handoffs.ErrAlreadyClaimed):
			// The expected outcome for every loser.
		default:
			t.Fatalf("racer %d: unexpected error %v", i, err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d concurrent claims produced %d winners, want exactly 1", racers, winners)
	}
}

func TestCurrentReturnsNewestUnclosed(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	// No handoffs at all: "nothing to take over" is an answer, not a fault.
	if _, err := f.svc.Current(ctx, principalID, projectID); !errors.Is(err, handoffs.ErrNotFound) {
		t.Fatalf("Current with no handoffs = %v, want ErrNotFound", err)
	}

	first := create(t, f, principalID, projectID, "first")
	second := create(t, f, principalID, projectID, "second")

	got, err := f.svc.Current(ctx, principalID, projectID)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if got.ID != second.ID {
		t.Fatalf("Current = %s, want the newest handoff %s", got.ID, second.ID)
	}

	// A claimed handoff is still current — it is being worked on, not done.
	if _, err := f.svc.Claim(ctx, principalID, nil, projectID, second.ID); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	got, err = f.svc.Current(ctx, principalID, projectID)
	if err != nil {
		t.Fatalf("Current after claim: %v", err)
	}
	if got.ID != second.ID || got.Status != handoffs.StatusClaimed {
		t.Fatalf("Current after claim = %s/%s, want %s/claimed", got.ID, got.Status, second.ID)
	}

	// Closing the newest falls back to the older one that is still open.
	if _, err := f.svc.Close(ctx, principalID, nil, projectID, second.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got, err = f.svc.Current(ctx, principalID, projectID)
	if err != nil {
		t.Fatalf("Current after close: %v", err)
	}
	if got.ID != first.ID {
		t.Fatalf("Current after closing the newest = %s, want the older open handoff %s", got.ID, first.ID)
	}

	if _, err := f.svc.Close(ctx, principalID, nil, projectID, first.ID); err != nil {
		t.Fatalf("Close first: %v", err)
	}
	if _, err := f.svc.Current(ctx, principalID, projectID); !errors.Is(err, handoffs.ErrNotFound) {
		t.Fatalf("Current with everything closed = %v, want ErrNotFound", err)
	}
}

func TestReaderCannotClaim(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	writerID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	h := create(t, f, writerID, projectID, "not yours to take")

	// A second principal on the same project, holding READER only.
	readerID := uuid.New()
	err := db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Reader"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, readerID, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'READER')`,
			projectID, readerID)
		return err
	})
	if err != nil {
		t.Fatalf("seed reader: %v", err)
	}

	// Claiming changes what other agents will decide to do, so it is a
	// write capability — a READER may see the handoff but not take it.
	if _, err := f.svc.Current(ctx, readerID, projectID); err != nil {
		t.Fatalf("reader Current: %v", err)
	}
	if _, err := f.svc.Claim(ctx, readerID, nil, projectID, h.ID); !errors.Is(err, handoffs.ErrForbidden) {
		t.Fatalf("reader Claim = %v, want ErrForbidden", err)
	}
}

// TestTransitionsAreAudited holds the line NEXT.md's backlog calls out for
// membership changes: a privilege- or state-changing operation that leaves
// no audit row is a provenance gap, and "trace the origin of a change
// without SQL" is a stated v0.1 metric.
func TestTransitionsAreAudited(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	h := create(t, f, principalID, projectID, "audited")
	if _, err := f.svc.Claim(ctx, principalID, nil, projectID, h.ID); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, err := f.svc.Close(ctx, principalID, nil, projectID, h.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		for _, action := range []string{"handoff.claim", "handoff.close"} {
			var n int
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM audit_log WHERE resource_id = $1 AND action = $2`, h.ID, action,
			).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				t.Fatalf("audit rows for %s = %d, want 1", action, n)
			}
		}
		// And the matching outbox events, published in the same transaction.
		for _, eventType := range []string{"handoff.claimed", "handoff.closed"} {
			var n int
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM domain_events WHERE resource_id = $1 AND type = $2`, h.ID, eventType,
			).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				t.Fatalf("domain_events rows for %s = %d, want 1", eventType, n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify audit: %v", err)
	}
}
