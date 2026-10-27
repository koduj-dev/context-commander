package plans_test

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
	"github.com/koduj-dev/context-commander/internal/plans"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type member struct {
	principalID uuid.UUID
	clientID    uuid.UUID
}

type fixture struct {
	t         *testing.T
	ctx       context.Context
	ownerPool *db.Pool
	svc       *plans.Service
	projectID uuid.UUID
	nsSlug    string
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
	f := &fixture{
		t: t, ctx: ctx, ownerPool: ownerPool,
		svc:   plans.NewService(appPool, crypto.NewProvider(root), auth.NewAuthzService(appPool, projects.NewStore())),
		clock: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC),
	}
	f.svc.SetClock(func() time.Time { return f.clock })
	f.projectID, f.nsSlug = f.newProject("work")
	return f
}

func (f *fixture) newProject(slug string) (uuid.UUID, string) {
	f.t.Helper()
	id, ns := uuid.New(), uuid.New()
	nsSlug := "ns-" + ns.String()[:8]
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		owner := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, 'Owner')`, owner, owner.String()+"@example.test"); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`, ns, nsSlug, owner); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, $3, $3)`, id, ns, slug)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed project: %v", err)
	}
	return id, nsSlug
}

// join adds a named user to a project with a role and gives them a client.
func (f *fixture) join(projectID uuid.UUID, name string, role projects.Role) member {
	f.t.Helper()
	m := member{principalID: uuid.New(), clientID: uuid.New()}
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		user := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`, user, user.String()+"@example.test", name); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, m.principalID, user); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO client_credentials (id, principal_id, client_type, token_hash) VALUES ($1, $2, 'claude-code', 'unused')`,
			m.clientID, m.principalID); err != nil {
			return err
		}
		return f.addMember(tx, projectID, m, role)
	})
	if err != nil {
		f.t.Fatalf("seed member: %v", err)
	}
	return m
}

func (f *fixture) addMember(tx pgx.Tx, projectID uuid.UUID, m member, role projects.Role) error {
	_, err := tx.Exec(f.ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`, projectID, m.principalID, role)
	return err
}

func (f *fixture) plan(m member, title string, items ...string) plans.Plan {
	f.t.Helper()
	var news []plans.NewItem
	for _, it := range items {
		news = append(news, plans.NewItem{Text: it})
	}
	p, err := f.svc.CreatePlan(f.ctx, m.principalID, &m.clientID, f.projectID, nil, title, "", news)
	if err != nil {
		f.t.Fatalf("CreatePlan: %v", err)
	}
	return p
}

func (f *fixture) set(m member, itemID uuid.UUID, status string) plans.Item {
	f.t.Helper()
	it, err := f.svc.UpdateItem(f.ctx, m.principalID, &m.clientID, f.projectID, itemID, plans.ItemUpdate{Status: &status})
	if err != nil {
		f.t.Fatalf("UpdateItem %s: %v", status, err)
	}
	return it
}

func TestAPlanIsStoredEncryptedAndReadBackWhole(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)

	created, err := f.svc.CreatePlan(f.ctx, alice.principalID, &alice.clientID, f.projectID, nil,
		"Migrate the billing database", "Cut over on Friday",
		[]plans.NewItem{{Text: "Dump production"}, {Text: "Restore checksums match", Kind: plans.KindCheck}})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	got, err := f.svc.GetPlan(f.ctx, alice.principalID, f.projectID, created.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.Title != "Migrate the billing database" || got.Body != "Cut over on Friday" || len(got.Items) != 2 {
		t.Fatalf("read back %+v", got)
	}
	if got.Items[0].Text != "Dump production" || got.Items[1].Kind != plans.KindCheck || got.Items[1].Position != 2 {
		t.Errorf("items %+v", got.Items)
	}

	var ct []byte
	err = db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `SELECT ciphertext FROM plans WHERE id = $1`, created.ID).Scan(&ct)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"billing", "Dump production", "checksums"} {
		if bytes.Contains(ct, []byte(plain)) {
			t.Errorf("%q is stored in plaintext", plain)
		}
	}
}

func TestConcurrentAppendsBothLand(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	p := f.plan(alice, "Shared plan", "first")

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.svc.AddItems(f.ctx, alice.principalID, f.projectID, p.ID, []plans.NewItem{{Text: fmt.Sprintf("added %d", i)}})
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("AddItems: %v", err)
		}
	}
	got, err := f.svc.GetPlan(f.ctx, alice.principalID, f.projectID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 6 {
		t.Fatalf("%d items, want 6 — an append was lost", len(got.Items))
	}
	seen := map[int]bool{}
	for _, it := range got.Items {
		if it.Text == "" {
			t.Errorf("item %d has no text: its blob entry was overwritten", it.Position)
		}
		if seen[it.Position] {
			t.Errorf("two items at position %d", it.Position)
		}
		seen[it.Position] = true
	}
}

func TestStatusHistoryIsBoundedAndRecordsOnlyRealChanges(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	p := f.plan(alice, "Ticks", "the item")
	item := p.Items[0].ID

	started := f.set(alice, item, plans.ItemInProgress)
	if started.Assignee == nil || *started.Assignee != alice.principalID {
		t.Errorf("starting an unassigned item did not assign it to whoever started it: %v", started.Assignee)
	}
	f.set(alice, item, plans.ItemInProgress) // no change, nothing recorded

	h, err := f.svc.Transitions(f.ctx, alice.principalID, f.projectID, item)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 1 || h[0].From != plans.ItemOpen || h[0].To != plans.ItemInProgress {
		t.Fatalf("history after one real change = %+v", h)
	}

	for i := range 30 {
		if i%2 == 0 {
			f.set(alice, item, plans.ItemDone)
		} else {
			f.set(alice, item, plans.ItemInProgress)
		}
	}
	if h, _ = f.svc.Transitions(f.ctx, alice.principalID, f.projectID, item); len(h) != plans.HistoryPerItem {
		t.Fatalf("history holds %d changes, want the cap of %d", len(h), plans.HistoryPerItem)
	}
	if h[0].To != plans.ItemInProgress {
		t.Errorf("newest change first: got %s", h[0].To)
	}

	done := f.set(alice, item, plans.ItemDone)
	if done.CompletedAt == nil {
		t.Error("a done item has no completion time")
	}
	if reopened := f.set(alice, item, plans.ItemOpen); reopened.CompletedAt != nil {
		t.Error("a reopened item kept its completion time")
	}
}

func TestRewritingAPlansTextNeedsTheCurrentVersion(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	p := f.plan(alice, "Draft title", "one")

	// An item edit bumps the version: the plan's blob was rewritten.
	note := "watch the replica lag"
	if _, err := f.svc.UpdateItem(f.ctx, alice.principalID, &alice.clientID, f.projectID, p.Items[0].ID, plans.ItemUpdate{Note: &note}); err != nil {
		t.Fatal(err)
	}
	title := "Final title"
	if _, err := f.svc.UpdatePlan(f.ctx, alice.principalID, f.projectID, p.ID, plans.PlanUpdate{Title: &title, ExpectedVersion: p.Version}); !errors.Is(err, plans.ErrConflict) {
		t.Fatalf("a stale rewrite returned %v, want ErrConflict", err)
	}
	cur, _ := f.svc.GetPlan(f.ctx, alice.principalID, f.projectID, p.ID)
	if cur.Items[0].Note != note {
		t.Errorf("the note did not survive: %q", cur.Items[0].Note)
	}
	updated, err := f.svc.UpdatePlan(f.ctx, alice.principalID, f.projectID, p.ID, plans.PlanUpdate{Title: &title, ExpectedVersion: cur.Version})
	if err != nil {
		t.Fatalf("a current rewrite: %v", err)
	}
	if updated.Title != title || updated.Items[0].Note != note {
		t.Errorf("after the rewrite: %+v", updated)
	}
}

func TestReadersReadAndOutsidersSeeNothing(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	reader := f.join(f.projectID, "Rita", projects.RoleReader)
	otherProject, _ := f.newProject("elsewhere")
	outsider := f.join(otherProject, "Otto", projects.RoleOwner)
	p := f.plan(alice, "Visible", "step")

	if _, err := f.svc.GetPlan(f.ctx, reader.principalID, f.projectID, p.ID); err != nil {
		t.Errorf("a reader could not read: %v", err)
	}
	done := plans.ItemDone
	if _, err := f.svc.UpdateItem(f.ctx, reader.principalID, &reader.clientID, f.projectID, p.Items[0].ID, plans.ItemUpdate{Status: &done}); !errors.Is(err, plans.ErrForbidden) {
		t.Errorf("a reader ticked an item: %v", err)
	}
	if _, err := f.svc.Board(f.ctx, outsider.principalID, f.projectID); !errors.Is(err, plans.ErrForbidden) {
		t.Errorf("an outsider read the board: %v", err)
	}
	// And the outsider's own project cannot be used to reach this one's item.
	if _, err := f.svc.UpdateItem(f.ctx, outsider.principalID, &outsider.clientID, otherProject, p.Items[0].ID, plans.ItemUpdate{Status: &done}); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("an item was reached through another project: %v", err)
	}
}

func TestHeartbeatsCountTimeOnlyWhileTheyKeepComing(t *testing.T) {
	f := newFixture(t)
	agent := f.join(f.projectID, "Codex", projects.RoleContributor)
	p := f.plan(agent, "Steps", "step A", "step B")
	a, b := p.Items[0].ID, p.Items[1].ID

	report := func(item uuid.UUID, tokensIn int64) {
		t.Helper()
		if _, err := f.svc.Report(f.ctx, agent.principalID, &agent.clientID, f.projectID,
			plans.Report{ItemID: &item, Activity: "working", Usage: plans.Effort{TokensIn: tokensIn}}); err != nil {
			t.Fatalf("Report: %v", err)
		}
	}
	effort := func(item uuid.UUID) plans.Effort {
		t.Helper()
		got, err := f.svc.GetPlan(f.ctx, agent.principalID, f.projectID, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range got.Items {
			if it.ID == item {
				return it.Effort
			}
		}
		t.Fatalf("item %s missing", item)
		return plans.Effort{}
	}

	report(a, 0)
	f.clock = f.clock.Add(5 * time.Minute)
	report(a, 1000) // five minutes on A, and the tokens that took
	f.clock = f.clock.Add(40 * time.Minute)
	report(a, 0) // forty silent minutes: not provably work
	f.clock = f.clock.Add(10 * time.Minute)
	report(b, 500) // ten more on A, then moved to B; the 500 were spent on A

	if e := effort(a); e.Seconds != 15*60 || e.TokensIn != 1500 {
		t.Errorf("A: %+v, want 900 s and 1500 tokens in", e)
	}
	if e := effort(b); e.Seconds != 0 || e.TokensIn != 0 {
		t.Errorf("B: %+v, want nothing yet", e)
	}

	f.clock = f.clock.Add(3 * time.Minute)
	if err := f.svc.ClearPresence(f.ctx, agent.principalID, &agent.clientID, f.projectID, plans.Effort{TokensOut: 42}); err != nil {
		t.Fatal(err)
	}
	if e := effort(b); e.Seconds != 180 || e.TokensOut != 42 {
		t.Errorf("B after signing off: %+v, want 180 s and 42 tokens out", e)
	}
	board, err := f.svc.Board(f.ctx, agent.principalID, f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Presence) != 0 {
		t.Errorf("a cleared client is still present: %+v", board.Presence)
	}
	if _, err := f.svc.Report(f.ctx, agent.principalID, nil, f.projectID, plans.Report{}); !errors.Is(err, plans.ErrNoClient) {
		t.Errorf("a report without a client returned %v", err)
	}
}

func TestTheBoardSaysWhereEveryoneIs(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	bob := f.join(f.projectID, "Bob", projects.RoleContributor)

	phase, err := f.svc.CreatePhase(f.ctx, alice.principalID, f.projectID, "Release 1.2", "ship it")
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.svc.CreatePlan(f.ctx, alice.principalID, &alice.clientID, f.projectID, &phase.ID, "Release checklist", "",
		[]plans.NewItem{{Text: "Tag"}, {Text: "Smoke test", Kind: plans.KindCheck}, {Text: "Announce"}})
	if err != nil {
		t.Fatal(err)
	}
	f.set(alice, p.Items[0].ID, plans.ItemDone)
	f.set(bob, p.Items[1].ID, plans.ItemInProgress)
	f.set(bob, p.Items[2].ID, plans.ItemBlocked)
	if _, err := f.svc.Report(f.ctx, bob.principalID, &bob.clientID, f.projectID,
		plans.Report{ItemID: &p.Items[1].ID, Activity: "running the smoke suite"}); err != nil {
		t.Fatal(err)
	}
	f.plan(alice, "Old idea", "never")
	abandoned := plans.PlanAbandoned
	old := f.plan(alice, "Abandoned", "x")
	if _, err := f.svc.UpdatePlan(f.ctx, alice.principalID, f.projectID, old.ID, plans.PlanUpdate{Status: &abandoned}); err != nil {
		t.Fatal(err)
	}

	b, err := f.svc.Board(f.ctx, alice.principalID, f.projectID)
	if err != nil {
		t.Fatalf("Board: %v", err)
	}
	if len(b.Phases) != 1 || b.Phases[0].Title != "Release 1.2" {
		t.Fatalf("phases %+v", b.Phases)
	}
	if c := b.Phases[0].Counts; c.Done != 1 || c.InProgress != 1 || c.Blocked != 1 || c.Total() != 3 {
		t.Errorf("phase counts %+v", c)
	}
	if len(b.Plans) != 2 {
		t.Errorf("%d plans listed, want 2 — the abandoned one is left out", len(b.Plans))
	}
	if len(b.InProgress) != 1 || b.InProgress[0].Item.Text != "Smoke test" || *b.InProgress[0].Item.Assignee != bob.principalID {
		t.Errorf("in progress %+v", b.InProgress)
	}
	if len(b.Blocked) != 1 || b.Blocked[0].Item.Text != "Announce" {
		t.Errorf("blocked %+v", b.Blocked)
	}
	if len(b.Presence) != 1 {
		t.Fatalf("presence %+v", b.Presence)
	}
	pr := b.Presence[0]
	if pr.Activity != "running the smoke suite" || pr.ItemText != "Smoke test" || pr.PlanTitle != "Release checklist" || pr.ClientType != "claude-code" {
		t.Errorf("presence %+v", pr)
	}
	if b.People[bob.principalID].Name != "Bob" {
		t.Errorf("Bob is named %q", b.People[bob.principalID].Name)
	}

	mine, people, err := f.svc.MyPresence(f.ctx, alice.principalID)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 || mine[0].ProjectPath != f.nsSlug+"/work" || mine[0].ItemText != "Smoke test" || people[bob.principalID].Name != "Bob" {
		t.Errorf("my presence %+v", mine)
	}
}

func TestSearchFindsTextThatIsStoredEncrypted(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	p := f.plan(alice, "Upgrade Elasticsearch", "Snapshot the cluster", "Check shard allocation")
	f.set(alice, p.Items[0].ID, plans.ItemDone)

	hits, err := f.svc.Search(f.ctx, alice.principalID, f.projectID, "SHARD", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ItemID == nil || *hits[0].ItemID != p.Items[1].ID {
		t.Errorf("hits for SHARD: %+v", hits)
	}
	hits, _ = f.svc.Search(f.ctx, alice.principalID, f.projectID, "elasticsearch", "")
	if len(hits) != 1 || hits[0].ItemID != nil {
		t.Errorf("hits for the plan title: %+v", hits)
	}
	hits, _ = f.svc.Search(f.ctx, alice.principalID, f.projectID, "", plans.ItemDone)
	if len(hits) != 1 || hits[0].Text != "Snapshot the cluster" {
		t.Errorf("done items: %+v", hits)
	}
	if _, err := f.svc.Search(f.ctx, alice.principalID, f.projectID, "", ""); !errors.Is(err, plans.ErrInvalid) {
		t.Errorf("an empty search returned %v", err)
	}
}

func TestPlanTextCountsAgainstTheQuota(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	used := func() int64 {
		var n int64
		_ = db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
			return tx.QueryRow(f.ctx, `SELECT COALESCE((SELECT s.used_bytes FROM namespace_storage s JOIN projects p ON p.namespace_id = s.namespace_id WHERE p.id = $1), 0)`,
				f.projectID).Scan(&n)
		})
		return n
	}
	p := f.plan(alice, "Counted", "one")
	first := used()
	if first == 0 {
		t.Fatal("a plan's text was not counted")
	}
	if _, err := f.svc.AddItems(f.ctx, alice.principalID, f.projectID, p.ID, []plans.NewItem{{Text: "a considerably longer second item than the first"}}); err != nil {
		t.Fatal(err)
	}
	if grown := used(); grown <= first {
		t.Errorf("used %d after growing the plan, was %d", grown, first)
	}
	if err := f.svc.DeletePlan(f.ctx, alice.principalID, &alice.clientID, f.projectID, p.ID); err != nil {
		t.Fatal(err)
	}
	if after := used(); after != 0 {
		t.Errorf("used %d after deleting the only plan, want 0", after)
	}
}

// A client hook knows what a turn cost and not which step it was on; the
// presence row knows the step. ReportUsage joins the two without moving the
// client, and counts as a heartbeat.
func TestAHooksUsageLandsWhereTheClientSaidItWas(t *testing.T) {
	f := newFixture(t)
	agent := f.join(f.projectID, "Claude", projects.RoleContributor)
	p := f.plan(agent, "Steps", "the step")
	item := p.Items[0].ID

	if _, ok, err := f.svc.ReportUsage(f.ctx, agent.principalID, &agent.clientID, plans.Effort{TokensIn: 5}); err != nil || ok {
		t.Fatalf("usage before any presence: ok=%v err=%v, want dropped", ok, err)
	}

	if _, err := f.svc.Report(f.ctx, agent.principalID, &agent.clientID, f.projectID,
		plans.Report{ItemID: &item, Activity: "editing the parser"}); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(4 * time.Minute)
	usage := plans.Effort{Seconds: 99999, TokensIn: 12, TokensOut: 3400, CacheReadTokens: 2_500_000, CacheWriteTokens: 40_000}
	pr, ok, err := f.svc.ReportUsage(f.ctx, agent.principalID, &agent.clientID, usage)
	if err != nil || !ok || pr.ItemID == nil || *pr.ItemID != item {
		t.Fatalf("ReportUsage: %+v ok=%v err=%v", pr, ok, err)
	}

	got, _ := f.svc.GetPlan(f.ctx, agent.principalID, f.projectID, p.ID)
	e := got.Items[0].Effort
	if e.Seconds != 240 {
		t.Errorf("seconds = %d, want 240 — the hook's call is a heartbeat, and a Seconds it sends is ignored", e.Seconds)
	}
	if e.TokensIn != 12 || e.TokensOut != 3400 || e.CacheReadTokens != 2_500_000 || e.CacheWriteTokens != 40_000 {
		t.Errorf("tokens %+v", e)
	}

	b, _ := f.svc.Board(f.ctx, agent.principalID, f.projectID)
	if len(b.Presence) != 1 || b.Presence[0].Activity != "editing the parser" || *b.Presence[0].ItemID != item {
		t.Errorf("the hook moved or blanked the presence: %+v", b.Presence)
	}
	if _, _, err := f.svc.ReportUsage(f.ctx, agent.principalID, &agent.clientID, plans.Effort{CacheReadTokens: -1}); !errors.Is(err, plans.ErrInvalid) {
		t.Errorf("a negative count returned %v", err)
	}
}
