package ideas_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/ideas"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/plans"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type member struct {
	principalID uuid.UUID
	clientID    uuid.UUID // a PAT, as an agent holds
	sessionID   uuid.UUID // a browser session, as a person signed in holds
}

type fixture struct {
	t         *testing.T
	ctx       context.Context
	ownerPool *db.Pool
	appPool   *db.Pool
	svc       *ideas.Service
	plans     *plans.Service
	projectID uuid.UUID
	clock     time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	dsns := dbtest.New(t)
	ownerPool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatal(err)
	}
	appPool, err := db.NewPool(ctx, dsns.App)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_ROOT_KEY", key)
	root, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
	if err != nil {
		t.Fatal(err)
	}
	cp := crypto.NewProvider(root)
	authz := auth.NewAuthzService(appPool, projects.NewStore())
	plansSvc := plans.NewService(appPool, cp, authz)
	f := &fixture{
		t: t, ctx: ctx, ownerPool: ownerPool, appPool: appPool,
		svc: ideas.NewService(appPool, cp, authz, plansSvc), plans: plansSvc,
		clock: time.Now().UTC().Truncate(time.Second),
	}
	f.svc.SetClock(func() time.Time { return f.clock })
	f.projectID = f.newProject("work")
	return f
}

func (f *fixture) newProject(slug string) uuid.UUID {
	f.t.Helper()
	id, ns := uuid.New(), uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		owner := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, 'Owner')`, owner, owner.String()+"@example.test"); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`, ns, "ns-"+ns.String()[:8], owner); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, $3, $3)`, id, ns, slug)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed project: %v", err)
	}
	return id
}

// join adds a user to a project with a role, a PAT and a browser session.
func (f *fixture) join(projectID uuid.UUID, name string, role projects.Role) member {
	f.t.Helper()
	m := member{principalID: uuid.New(), clientID: uuid.New(), sessionID: uuid.New()}
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		user := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`, user, user.String()+"@example.test", name); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, m.principalID, user); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO client_credentials (id, principal_id, client_type, token_hash) VALUES ($1, $2, 'claude-code', 'unused'), ($3, $2, 'session', 'unused')`,
			m.clientID, m.principalID, m.sessionID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`, projectID, m.principalID, role)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed member: %v", err)
	}
	return m
}

// anotherClient gives m a second PAT: a second agent run by the same person.
func (f *fixture) anotherClient(m member) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `INSERT INTO client_credentials (id, principal_id, client_type, token_hash) VALUES ($1, $2, 'codex', 'unused')`, id, m.principalID)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed client: %v", err)
	}
	return id
}

func (f *fixture) add(m member, title, priority string) ideas.Idea {
	f.t.Helper()
	i, err := f.svc.Create(f.ctx, m.principalID, &m.clientID, f.projectID, ideas.NewIdea{Title: title, Priority: priority})
	if err != nil {
		f.t.Fatalf("Create %q: %v", title, err)
	}
	return i
}

func (f *fixture) claimNext(m member, clientID uuid.UUID) (ideas.Idea, bool) {
	f.t.Helper()
	i, found, _, err := f.svc.ClaimNext(f.ctx, m.principalID, &clientID, f.projectID)
	if err != nil {
		f.t.Fatalf("ClaimNext: %v", err)
	}
	return i, found
}

func TestAnIdeaIsStoredEncryptedAndReadBackWhole(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)

	created, err := f.svc.Create(f.ctx, alice.principalID, &alice.clientID, f.projectID, ideas.NewIdea{
		Title: "Cache the pricing table", Body: "Every request reads it from disk",
		TicketKey: "PRICE-7", TicketURL: "https://tracker.example.test/PRICE-7",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := f.svc.Get(f.ctx, alice.principalID, f.projectID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Cache the pricing table" || got.Body != "Every request reads it from disk" ||
		got.Status != ideas.StatusOpen || got.Priority != ideas.PriorityNormal || got.TicketKey != "PRICE-7" {
		t.Fatalf("read back %+v", got)
	}

	var ct []byte
	err = db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `SELECT ciphertext FROM ideas WHERE id = $1`, created.ID).Scan(&ct)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"pricing", "from disk"} {
		if bytes.Contains(ct, []byte(plain)) {
			t.Errorf("%q is stored in plaintext", plain)
		}
	}

	for _, bad := range []string{"javascript:alert(1)", "ftp://example.test/x", "/relative"} {
		if _, err := f.svc.Create(f.ctx, alice.principalID, &alice.clientID, f.projectID, ideas.NewIdea{Title: "x", TicketURL: bad}); !errors.Is(err, ideas.ErrInvalid) {
			t.Errorf("ticket_url %q accepted: %v", bad, err)
		}
	}
}

// The property the stack exists for: two planning agents asking at once
// never take the same idea. Twice as many claimers as ideas, all at once;
// every idea must be claimed exactly once and the surplus must find nothing.
func TestConcurrentClaimNextNeverHandsOutAnIdeaTwice(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	const n = 8
	for k := range n {
		f.add(alice, fmt.Sprintf("idea %d", k), ideas.PriorityNormal)
	}
	clients := make([]uuid.UUID, 2*n)
	for k := range clients {
		clients[k] = f.anotherClient(alice)
	}

	var wg sync.WaitGroup
	got := make([]ideas.Idea, len(clients))
	found := make([]bool, len(clients))
	errs := make([]error, len(clients))
	start := make(chan struct{})
	for k := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got[k], found[k], _, errs[k] = f.svc.ClaimNext(f.ctx, alice.principalID, &clients[k], f.projectID)
		}()
	}
	close(start)
	wg.Wait()

	seen := map[uuid.UUID]uuid.UUID{}
	claimed := 0
	for k := range clients {
		if errs[k] != nil {
			t.Fatalf("ClaimNext: %v", errs[k])
		}
		if !found[k] {
			continue
		}
		claimed++
		if other, dup := seen[got[k].ID]; dup {
			t.Errorf("idea %s handed to client %s and to client %s", got[k].ID, other, clients[k])
		}
		seen[got[k].ID] = clients[k]
		if got[k].ClaimedByClient == nil || *got[k].ClaimedByClient != clients[k] {
			t.Errorf("idea %s returned to %s records claimant %v", got[k].ID, clients[k], got[k].ClaimedByClient)
		}
	}
	if claimed != n {
		t.Errorf("%d ideas claimed, want %d", claimed, n)
	}

	// And the database agrees: every idea has exactly one claimant, the one
	// that was told it won.
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		rows, err := tx.Query(f.ctx, `SELECT id, claimed_by_client FROM ideas WHERE project_id = $1`, f.projectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var by *uuid.UUID
			if err := rows.Scan(&id, &by); err != nil {
				return err
			}
			if by == nil || seen[id] != *by {
				t.Errorf("idea %s is held by %v in the database, but %s was told it won", id, by, seen[id])
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestALapsedClaimGoesBackToTheQueue(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	crashed, rescuer := alice.clientID, f.anotherClient(alice)
	idea := f.add(alice, "Investigate the slow export", "")

	first, ok := f.claimNext(alice, crashed)
	if !ok || first.ID != idea.ID {
		t.Fatalf("first claim got %v, %v", first.ID, ok)
	}
	if want := f.clock.Add(ideas.ClaimLease); first.ClaimExpiresAt == nil || !first.ClaimExpiresAt.Equal(want) {
		t.Errorf("lease ends %v, want %v", first.ClaimExpiresAt, want)
	}
	if _, ok := f.claimNext(alice, rescuer); ok {
		t.Fatal("a live claim was handed out again")
	}
	if _, err := f.svc.Claim(f.ctx, alice.principalID, &rescuer, f.projectID, idea.ID); !errors.Is(err, ideas.ErrAlreadyClaimed) {
		t.Errorf("claiming a live claim: %v, want ErrAlreadyClaimed", err)
	}

	// The first agent crashes. One minute before the lease ends, the idea is
	// still its; one minute after, it is anyone's.
	f.clock = f.clock.Add(ideas.ClaimLease - time.Minute)
	if _, ok := f.claimNext(alice, rescuer); ok {
		t.Fatal("a claim was handed out before its lease ended")
	}
	f.clock = f.clock.Add(2 * time.Minute)
	c, err := f.svc.Counts(f.ctx, alice.principalID, f.projectID)
	if err != nil || c.Claimed != 1 || c.Lapsed != 1 {
		t.Errorf("Counts = %+v, %v; want one claimed, lapsed", c, err)
	}
	second, ok := f.claimNext(alice, rescuer)
	if !ok || second.ID != idea.ID || *second.ClaimedByClient != rescuer {
		t.Fatalf("after the lease the rescuer got %v (%v), claimant %v", second.ID, ok, second.ClaimedByClient)
	}

	// The crashed agent comes back: its claim is gone, and it cannot
	// propose over the rescuer's.
	if _, err := f.svc.Propose(f.ctx, alice.principalID, &crashed, f.projectID, idea.ID, "late", nil); !errors.Is(err, ideas.ErrNotClaimant) {
		t.Errorf("a lapsed claimant proposed: %v, want ErrNotClaimant", err)
	}
	if _, err := f.svc.Renew(f.ctx, alice.principalID, &crashed, f.projectID, idea.ID); !errors.Is(err, ideas.ErrNotClaimant) {
		t.Errorf("a lapsed claimant renewed: %v, want ErrNotClaimant", err)
	}

	// Renewing extends the lease from now.
	f.clock = f.clock.Add(time.Hour)
	renewed, err := f.svc.Renew(f.ctx, alice.principalID, &rescuer, f.projectID, idea.ID)
	if err != nil || !renewed.ClaimExpiresAt.Equal(f.clock.Add(ideas.ClaimLease)) {
		t.Errorf("Renew = %v, %v", renewed.ClaimExpiresAt, err)
	}
}

func TestClaimNextGivesBackTheIdeaYouAlreadyHold(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	f.add(alice, "one", "")
	f.add(alice, "two", "")

	first, _ := f.claimNext(alice, alice.clientID)
	again, found, resumed, err := f.svc.ClaimNext(f.ctx, alice.principalID, &alice.clientID, f.projectID)
	if err != nil || !found || !resumed || again.ID != first.ID {
		t.Fatalf("second ClaimNext = %v found=%v resumed=%v err=%v; want the first idea, resumed", again.ID, found, resumed, err)
	}
	// Claiming what you hold is a renewal, not a refusal.
	if _, err := f.svc.Claim(f.ctx, alice.principalID, &alice.clientID, f.projectID, first.ID); err != nil {
		t.Errorf("re-claiming your own idea: %v", err)
	}
	// Releasing puts it back first in line.
	if _, err := f.svc.Release(f.ctx, alice.principalID, &alice.clientID, f.projectID, first.ID); err != nil {
		t.Fatalf("Release: %v", err)
	}
	other := f.anotherClient(alice)
	if next, _ := f.claimNext(alice, other); next.ID != first.ID {
		t.Errorf("after release the queue starts with %q, want %q", next.Title, first.Title)
	}
}

func TestTheQueueIsPriorityThenManualOrderThenAge(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	a := f.add(alice, "a", ideas.PriorityLow)
	b := f.add(alice, "b", "")
	c := f.add(alice, "c", "")
	d := f.add(alice, "d", ideas.PriorityHigh)
	e := f.add(alice, "e", "")

	// e to the front of the normal line, then a to high (the end of it).
	one := 1
	if _, err := f.svc.Update(f.ctx, alice.principalID, f.projectID, e.ID, ideas.Update{Position: &one}); err != nil {
		t.Fatalf("move: %v", err)
	}
	high := ideas.PriorityHigh
	if _, err := f.svc.Update(f.ctx, alice.principalID, f.projectID, a.ID, ideas.Update{Priority: &high}); err != nil {
		t.Fatalf("reprioritise: %v", err)
	}
	want := []string{d.Title, a.Title, e.Title, b.Title, c.Title}

	// The list pages in that order, two at a time, without repeats or gaps.
	var listed []string
	opts := paging.Options{Limit: 2}
	for {
		page, err := f.svc.List(f.ctx, alice.principalID, f.projectID, []string{ideas.StatusOpen}, opts)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, i := range page.Items {
			listed = append(listed, i.Title)
			if i.Body != "" || i.Proposal != "" {
				t.Errorf("the list carried content beyond the title: %+v", i)
			}
		}
		if page.NextCursor == "" {
			break
		}
		opts.Cursor = page.NextCursor
	}
	if fmt.Sprint(listed) != fmt.Sprint(want) {
		t.Errorf("listed %v, want %v", listed, want)
	}

	// And claim_next takes them in exactly that order.
	var claimed []string
	for range want {
		i, ok := f.claimNext(alice, f.anotherClient(alice))
		if !ok {
			t.Fatal("the queue ran out early")
		}
		claimed = append(claimed, i.Title)
	}
	if fmt.Sprint(claimed) != fmt.Sprint(want) {
		t.Errorf("claimed %v, want %v", claimed, want)
	}
	if _, err := f.svc.Update(f.ctx, alice.principalID, f.projectID, a.ID, ideas.Update{Position: &one}); !errors.Is(err, ideas.ErrInvalidTransition) {
		t.Errorf("moving a claimed idea: %v, want ErrInvalidTransition", err)
	}
}

func TestAProposalBringsADraftPlanThatOnlyAPersonCanAccept(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	idea := f.add(alice, "Split the monolith's billing module", "")
	claimed, _ := f.claimNext(alice, alice.clientID)

	proposed, err := f.svc.Propose(f.ctx, alice.principalID, &alice.clientID, f.projectID, claimed.ID,
		"## Proposal\nExtract behind an interface first.", &ideas.DraftPlan{
			Items: []ideas.PlanItem{{Text: "Introduce the interface"}, {Text: "Tests still pass", Kind: plans.KindCheck}},
		})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if proposed.Status != ideas.StatusProposed || proposed.ResultingPlanID == nil || proposed.ClaimExpiresAt != nil {
		t.Fatalf("proposed idea %+v", proposed)
	}
	plan, err := f.plans.GetPlan(f.ctx, alice.principalID, f.projectID, *proposed.ResultingPlanID)
	if err != nil || plan.Status != plans.PlanDraft || plan.Title != idea.Title || len(plan.Items) != 2 {
		t.Fatalf("draft plan = %+v, %v", plan, err)
	}

	// The agent, on its PAT, may not decide on its own proposal.
	if _, err := f.svc.Accept(f.ctx, alice.principalID, &alice.clientID, f.projectID, idea.ID); !errors.Is(err, ideas.ErrNeedsPerson) {
		t.Errorf("accept on a PAT: %v, want ErrNeedsPerson", err)
	}
	if _, err := f.svc.Reject(f.ctx, alice.principalID, &alice.clientID, f.projectID, idea.ID, "no", false); !errors.Is(err, ideas.ErrNeedsPerson) {
		t.Errorf("reject on a PAT: %v, want ErrNeedsPerson", err)
	}
	// Nor may a session that is someone else's.
	bob := f.join(f.projectID, "Bob", projects.RoleContributor)
	if _, err := f.svc.Accept(f.ctx, alice.principalID, &bob.sessionID, f.projectID, idea.ID); !errors.Is(err, ideas.ErrNeedsPerson) {
		t.Errorf("accept on another user's session: %v, want ErrNeedsPerson", err)
	}

	accepted, err := f.svc.Accept(f.ctx, bob.principalID, &bob.sessionID, f.projectID, idea.ID)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if accepted.Status != ideas.StatusAccepted || accepted.DecidedBy == nil || *accepted.DecidedBy != bob.principalID {
		t.Errorf("accepted idea %+v", accepted)
	}
	if *accepted.ClaimedBy != alice.principalID {
		t.Error("accepting forgot who proposed")
	}
	if plan, _ := f.plans.GetPlan(f.ctx, bob.principalID, f.projectID, *proposed.ResultingPlanID); plan.Status != plans.PlanActive {
		t.Errorf("after accept the plan is %s, want active", plan.Status)
	}
	if _, err := f.svc.Accept(f.ctx, bob.principalID, &bob.sessionID, f.projectID, idea.ID); !errors.Is(err, ideas.ErrInvalidTransition) {
		t.Errorf("accepting twice: %v", err)
	}
}

func TestARejectedProposalCanGoBackToTheQueueWithItsReason(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	f.add(alice, "first in line", "")
	f.add(alice, "second", "")
	claimed, _ := f.claimNext(alice, alice.clientID)
	proposed, err := f.svc.Propose(f.ctx, alice.principalID, &alice.clientID, f.projectID, claimed.ID, "rewrite it in Rust", &ideas.DraftPlan{Title: "Rust"})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}

	if _, err := f.svc.Reject(f.ctx, alice.principalID, &alice.sessionID, f.projectID, claimed.ID, "  ", true); !errors.Is(err, ideas.ErrInvalid) {
		t.Errorf("a reasonless rejection: %v, want ErrInvalid", err)
	}
	reopened, err := f.svc.Reject(f.ctx, alice.principalID, &alice.sessionID, f.projectID, claimed.ID, "Too large; keep it in Go", true)
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if reopened.Status != ideas.StatusOpen || reopened.ClaimedBy != nil || reopened.Rejection != "Too large; keep it in Go" || reopened.Proposal != "rewrite it in Rust" {
		t.Errorf("reopened idea %+v", reopened)
	}
	if plan, _ := f.plans.GetPlan(f.ctx, alice.principalID, f.projectID, *proposed.ResultingPlanID); plan.Status != plans.PlanAbandoned {
		t.Errorf("the rejected draft plan is %s, want abandoned", plan.Status)
	}
	// It kept its place: the next agent gets it first, reason included.
	next, _ := f.claimNext(alice, f.anotherClient(alice))
	if next.ID != claimed.ID || next.Rejection == "" {
		t.Errorf("next claim got %q with rejection %q", next.Title, next.Rejection)
	}
	// The new proposal replaces the old one and the reason it answered.
	other := *next.ClaimedByClient
	again, err := f.svc.Propose(f.ctx, alice.principalID, &other, f.projectID, next.ID, "extract one package", nil)
	if err != nil || again.Rejection != "" || again.ResultingPlanID != nil {
		t.Errorf("second proposal = %+v, %v", again, err)
	}
	final, err := f.svc.Reject(f.ctx, alice.principalID, &alice.sessionID, f.projectID, next.ID, "not now", false)
	if err != nil || final.Status != ideas.StatusRejected {
		t.Errorf("final rejection = %v, %v", final.Status, err)
	}
}

func TestWithdrawingAProposedIdeaAbandonsItsDraftPlan(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	f.add(alice, "maybe", "")
	claimed, _ := f.claimNext(alice, alice.clientID)
	proposed, err := f.svc.Propose(f.ctx, alice.principalID, &alice.clientID, f.projectID, claimed.ID, "do it", &ideas.DraftPlan{})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	w, err := f.svc.Withdraw(f.ctx, alice.principalID, &alice.clientID, f.projectID, claimed.ID)
	if err != nil || w.Status != ideas.StatusWithdrawn {
		t.Fatalf("Withdraw = %v, %v", w.Status, err)
	}
	if plan, _ := f.plans.GetPlan(f.ctx, alice.principalID, f.projectID, *proposed.ResultingPlanID); plan.Status != plans.PlanAbandoned {
		t.Errorf("the withdrawn idea's plan is %s, want abandoned", plan.Status)
	}
	if _, err := f.svc.Withdraw(f.ctx, alice.principalID, &alice.clientID, f.projectID, claimed.ID); !errors.Is(err, ideas.ErrInvalidTransition) {
		t.Errorf("withdrawing twice: %v", err)
	}
}

func TestAPersonCanFreeAnIdeaAnAgentHolds(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	bob := f.join(f.projectID, "Bob", projects.RoleContributor)
	idea := f.add(alice, "stuck", "")
	f.claimNext(alice, alice.clientID)

	if _, err := f.svc.Release(f.ctx, bob.principalID, &bob.clientID, f.projectID, idea.ID); !errors.Is(err, ideas.ErrNotClaimant) {
		t.Errorf("another agent released the claim: %v, want ErrNotClaimant", err)
	}
	if r, err := f.svc.Release(f.ctx, bob.principalID, &bob.sessionID, f.projectID, idea.ID); err != nil || r.Status != ideas.StatusOpen {
		t.Errorf("a person could not free the idea: %v, %v", r.Status, err)
	}
}

func TestReadersReadAndOutsidersSeeNothing(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	reader := f.join(f.projectID, "Rita", projects.RoleReader)
	otherProject := f.newProject("elsewhere")
	outsider := f.join(otherProject, "Otto", projects.RoleOwner)
	idea := f.add(alice, "secret roadmap item", "")

	if _, err := f.svc.Get(f.ctx, reader.principalID, f.projectID, idea.ID); err != nil {
		t.Errorf("a reader could not read: %v", err)
	}
	if _, err := f.svc.Create(f.ctx, reader.principalID, &reader.clientID, f.projectID, ideas.NewIdea{Title: "x"}); !errors.Is(err, ideas.ErrForbidden) {
		t.Errorf("a reader added an idea: %v", err)
	}
	if _, _, _, err := f.svc.ClaimNext(f.ctx, reader.principalID, &reader.clientID, f.projectID); !errors.Is(err, ideas.ErrForbidden) {
		t.Errorf("a reader claimed: %v", err)
	}
	if _, err := f.svc.Get(f.ctx, outsider.principalID, f.projectID, idea.ID); !errors.Is(err, ideas.ErrForbidden) {
		t.Errorf("an outsider read the idea: %v", err)
	}
	// Through their own project, the idea does not exist.
	if _, err := f.svc.Claim(f.ctx, outsider.principalID, &outsider.clientID, otherProject, idea.ID); !errors.Is(err, ideas.ErrNotFound) {
		t.Errorf("an idea was reached through another project: %v", err)
	}
	if _, found, _, _ := f.svc.ClaimNext(f.ctx, outsider.principalID, &outsider.clientID, otherProject); found {
		t.Error("claim_next in another project took this project's idea")
	}

	// And RLS itself, as cc_app with the outsider's identity and no service
	// in the way: the row is invisible and cannot be updated.
	err := db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: outsider.principalID}, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(f.ctx, `SELECT count(*) FROM ideas`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("cc_app as an outsider sees %d ideas", n)
		}
		tag, err := tx.Exec(f.ctx, `UPDATE ideas SET status = 'withdrawn', claim_expires_at = NULL WHERE id = $1`, idea.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 0 {
			t.Error("cc_app as an outsider updated the idea")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// cc_app may not erase an idea at all.
	err = db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: alice.principalID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `DELETE FROM ideas WHERE id = $1`, idea.ID)
		return err
	})
	if err == nil {
		t.Error("cc_app deleted an idea; DELETE should not be granted")
	}
}
