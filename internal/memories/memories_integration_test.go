package memories_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/memories"
	"github.com/koduj-dev/context-commander/internal/paging"
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
	appPool   *db.Pool
	svc       *memories.Service
	plans     *plans.Service
	projectID uuid.UUID
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
	f := &fixture{
		t: t, ctx: ctx, ownerPool: ownerPool, appPool: appPool,
		svc: memories.NewService(appPool, cp, authz), plans: plans.NewService(appPool, cp, authz),
	}
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

func (f *fixture) create(m member, projectID uuid.UUID, n memories.NewMemory) memories.Memory {
	f.t.Helper()
	got, err := f.svc.Create(f.ctx, m.principalID, &m.clientID, projectID, n)
	if err != nil {
		f.t.Fatalf("Create %q: %v", n.Title, err)
	}
	return got
}

func titles(list []memories.Memory) []string {
	out := []string{}
	for _, m := range list {
		out = append(out, m.Title)
	}
	return out
}

func hitTitles(hits []memories.Hit) []string {
	out := []string{}
	for _, h := range hits {
		out = append(out, h.Memory.Title)
	}
	return out
}

func TestAMemoryIsStoredEncryptedWithItsMetadataInTheClear(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	created := f.create(alice, f.projectID, memories.NewMemory{
		Kind: memories.KindGotcha, Title: "Replica lag breaks the migration", Body: "Wait for **zero lag** before cutover.",
		Branch: "feature/billing-cutover", TicketKey: "PROJ-123", TicketURL: "https://tracker.example.test/browse/PROJ-123",
		Tags: []string{" Billing ", "billing", "DB"}, Assignee: &alice.principalID,
	})
	if got := fmt.Sprint(created.Tags); got != "[billing db]" {
		t.Errorf("tags were not lowercased and de-duplicated: %s", got)
	}

	got, err := f.svc.Get(f.ctx, alice.principalID, f.projectID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != created.Title || got.Body != created.Body || got.Branch != "feature/billing-cutover" ||
		got.TicketKey != "PROJ-123" || got.Kind != memories.KindGotcha || got.Status != memories.StatusActive ||
		got.Assignee == nil || *got.Assignee != alice.principalID || got.CreatedByClient == nil || got.Version != 1 {
		t.Fatalf("read back %+v", got)
	}

	var ct []byte
	var branch, ticket string
	err = db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `SELECT ciphertext, branch, ticket_key FROM memories WHERE id = $1`, created.ID).Scan(&ct, &branch, &ticket)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"Replica", "zero lag", "cutover"} {
		if bytes.Contains(ct, []byte(plain)) {
			t.Errorf("%q is stored in plaintext", plain)
		}
	}
	// The allowlisted metadata is plaintext on purpose (docs/adr/0025).
	if branch != "feature/billing-cutover" || ticket != "PROJ-123" {
		t.Errorf("metadata columns: branch %q ticket %q", branch, ticket)
	}
}

func TestMetadataIsValidated(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	otherProject := f.newProject("other")
	outsider := f.join(otherProject, "Otto", projects.RoleOwner)
	foreignPlan, err := f.plans.CreatePlan(f.ctx, outsider.principalID, &outsider.clientID, otherProject, nil, "Theirs", "", []plans.NewItem{{Text: "step"}})
	if err != nil {
		t.Fatal(err)
	}
	ownPlan, err := f.plans.CreatePlan(f.ctx, alice.principalID, &alice.clientID, f.projectID, nil, "Ours", "", []plans.NewItem{{Text: "step"}})
	if err != nil {
		t.Fatal(err)
	}

	tooMany := make([]string, memories.MaxTags+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("t%d", i)
	}
	for name, n := range map[string]memories.NewMemory{
		"no title":               {Title: "  "},
		"unknown kind":           {Title: "x", Kind: "rumour"},
		"javascript url":         {Title: "x", TicketURL: "javascript:alert(1)"},
		"relative url":           {Title: "x", TicketURL: "/browse/PROJ-1"},
		"url with credentials":   {Title: "x", TicketURL: "https://me:secret@tracker.example.test/PROJ-1"},
		"branch with a newline":  {Title: "x", Branch: "main\nevil"},
		"ticket with spaces":     {Title: "x", TicketKey: "PROJ 1"},
		"too many tags":          {Title: "x", Tags: tooMany},
		"assignee not a member":  {Title: "x", Assignee: &outsider.principalID},
		"another project's plan": {Title: "x", PlanID: &foreignPlan.ID},
		"a step of another plan": {Title: "x", PlanID: &ownPlan.ID, PlanItemID: &foreignPlan.Items[0].ID},
	} {
		if _, err := f.svc.Create(f.ctx, alice.principalID, &alice.clientID, f.projectID, n); !errors.Is(err, memories.ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}

	// A step alone names its plan.
	m := f.create(alice, f.projectID, memories.NewMemory{Title: "linked", PlanItemID: &ownPlan.Items[0].ID})
	if m.PlanID == nil || *m.PlanID != ownPlan.ID {
		t.Errorf("plan from the step: %v", m.PlanID)
	}
	// And deleting the plan unlinks the memory rather than deleting it.
	if err := f.plans.DeletePlan(f.ctx, alice.principalID, &alice.clientID, f.projectID, ownPlan.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Get(f.ctx, alice.principalID, f.projectID, m.ID)
	if err != nil || got.PlanID != nil || got.PlanItemID != nil {
		t.Errorf("after the plan was deleted: %+v, %v", got, err)
	}
}

func TestEditingNeedsTheCurrentVersion(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	m := f.create(alice, f.projectID, memories.NewMemory{Title: "Draft", Branch: "main"})

	title := "Decided"
	edited, err := f.svc.Update(f.ctx, alice.principalID, f.projectID, m.ID, memories.Update{ExpectedVersion: m.Version, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Version != 2 || edited.Title != title || edited.Branch != "main" {
		t.Errorf("after the edit: %+v", edited)
	}
	clear := ""
	if _, err := f.svc.Update(f.ctx, alice.principalID, f.projectID, m.ID, memories.Update{ExpectedVersion: m.Version, Branch: &clear}); !errors.Is(err, memories.ErrConflict) {
		t.Fatalf("a stale edit returned %v, want ErrConflict", err)
	}
	cleared, err := f.svc.Update(f.ctx, alice.principalID, f.projectID, m.ID, memories.Update{ExpectedVersion: edited.Version, Branch: &clear})
	if err != nil {
		t.Fatal(err)
	}
	// A metadata-only edit re-seals the text under the new version, so it
	// still decrypts.
	got, err := f.svc.Get(f.ctx, alice.principalID, f.projectID, m.ID)
	if err != nil || got.Branch != "" || got.Title != title || got.Version != cleared.Version {
		t.Errorf("after clearing the branch: %+v, %v", got, err)
	}
}

func TestSupersededAndArchivedDropOutOfTheDefaultList(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	old := f.create(alice, f.projectID, memories.NewMemory{Kind: memories.KindDecision, Title: "Use MySQL"})
	replacement := f.create(alice, f.projectID, memories.NewMemory{Kind: memories.KindDecision, Title: "Use Postgres"})
	stale := f.create(alice, f.projectID, memories.NewMemory{Title: "Old status"})

	sup, err := f.svc.Supersede(f.ctx, alice.principalID, f.projectID, old.ID, replacement.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sup.Status != memories.StatusSuperseded || sup.SupersededBy == nil || *sup.SupersededBy != replacement.ID {
		t.Errorf("superseded: %+v", sup)
	}
	if _, err := f.svc.SetStatus(f.ctx, alice.principalID, f.projectID, stale.ID, memories.StatusArchived); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetStatus(f.ctx, alice.principalID, f.projectID, stale.ID, memories.StatusSuperseded); !errors.Is(err, memories.ErrInvalid) {
		t.Errorf("superseding without a replacement: %v", err)
	}
	if _, err := f.svc.Supersede(f.ctx, alice.principalID, f.projectID, old.ID, old.ID); !errors.Is(err, memories.ErrInvalid) {
		t.Errorf("a memory superseding itself: %v", err)
	}

	page, err := f.svc.List(f.ctx, alice.principalID, f.projectID, memories.Filter{}, paging.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(titles(page.Items)); got != "[Use Postgres]" {
		t.Errorf("default list: %s", got)
	}
	page, _ = f.svc.List(f.ctx, alice.principalID, f.projectID, memories.Filter{Status: memories.StatusAny}, paging.Options{})
	if len(page.Items) != 3 {
		t.Errorf("status any: %v", titles(page.Items))
	}
	if _, err := f.svc.List(f.ctx, alice.principalID, f.projectID, memories.Filter{Status: "gone"}, paging.Options{}); !errors.Is(err, memories.ErrInvalid) {
		t.Errorf("an unknown status filter: %v", err)
	}
}

func TestListFiltersByMetadataAndPagesByCursor(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	bob := f.join(f.projectID, "Bob", projects.RoleContributor)
	f.create(alice, f.projectID, memories.NewMemory{Kind: memories.KindFinding, Title: "a1", Branch: "main", TicketKey: "PROJ-1", Tags: []string{"api"}})
	f.create(alice, f.projectID, memories.NewMemory{Kind: memories.KindGotcha, Title: "a2", Branch: "feature/x", TicketKey: "PROJ-2", Assignee: &bob.principalID})
	f.create(bob, f.projectID, memories.NewMemory{Kind: memories.KindFinding, Title: "b1", Branch: "main", Tags: []string{"api", "perf"}})
	f.create(bob, f.projectID, memories.NewMemory{Kind: memories.KindNote, Title: "b2"})

	list := func(fl memories.Filter) string {
		t.Helper()
		page, err := f.svc.List(f.ctx, alice.principalID, f.projectID, fl, paging.Options{})
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(titles(page.Items))
	}
	for name, c := range map[string]struct {
		filter memories.Filter
		want   string
	}{
		"everything, newest first": {memories.Filter{}, "[b2 b1 a2 a1]"},
		"kind":                     {memories.Filter{Kind: memories.KindFinding}, "[b1 a1]"},
		"branch":                   {memories.Filter{Branch: "main"}, "[b1 a1]"},
		"ticket":                   {memories.Filter{TicketKey: "PROJ-2"}, "[a2]"},
		"tag, any case":            {memories.Filter{Tag: "PERF"}, "[b1]"},
		"assignee":                 {memories.Filter{Assignee: &bob.principalID}, "[a2]"},
		"author":                   {memories.Filter{Author: &bob.principalID}, "[b2 b1]"},
		"combined":                 {memories.Filter{Branch: "main", Tag: "api", Author: &alice.principalID}, "[a1]"},
	} {
		if got := list(c.filter); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
	future := time.Now().Add(time.Hour)
	if got := list(memories.Filter{CreatedAfter: &future}); got != "[]" {
		t.Errorf("created after the future: %s", got)
	}
	if got := list(memories.Filter{CreatedBefore: &future}); got != "[b2 b1 a2 a1]" {
		t.Errorf("created before the future: %s", got)
	}

	var walked []string
	opts := paging.Options{Limit: 3}
	for {
		page, err := f.svc.List(f.ctx, alice.principalID, f.projectID, memories.Filter{}, opts)
		if err != nil {
			t.Fatal(err)
		}
		walked = append(walked, titles(page.Items)...)
		if page.NextCursor == "" {
			break
		}
		opts.Cursor = page.NextCursor
	}
	if got := fmt.Sprint(walked); got != "[b2 b1 a2 a1]" {
		t.Errorf("paged walk: %s", got)
	}
	if _, err := f.svc.List(f.ctx, alice.principalID, f.projectID, memories.Filter{}, paging.Options{Cursor: paging.EncodeSeq(4)}); !errors.Is(err, paging.ErrInvalidCursor) {
		t.Errorf("another list's cursor: %v", err)
	}
}

func TestSearchRanksTitleHitsFirstAndCombinesWithFilters(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	f.create(alice, f.projectID, memories.NewMemory{Title: "Billing notes", Body: "the WEBHOOK retries three times", Branch: "main"})
	f.create(alice, f.projectID, memories.NewMemory{Title: "Webhook signatures", Body: "HMAC over the raw body", Branch: "feature/x"})
	f.create(alice, f.projectID, memories.NewMemory{Title: "Unrelated", Body: "nothing here"})
	f.create(alice, f.projectID, memories.NewMemory{Title: "Newer webhook gotcha", Branch: "main"})

	res, err := f.svc.Search(f.ctx, alice.principalID, f.projectID, "webhook", memories.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(hitTitles(res.Hits)); got != "[Newer webhook gotcha Webhook signatures Billing notes]" {
		t.Errorf("ranking: %s", got)
	}
	if res.Hits[2].Match != memories.MatchBody || res.Hits[0].Match != memories.MatchTitle || res.Truncated {
		t.Errorf("matches %+v truncated %v", res.Hits, res.Truncated)
	}
	if res.People[alice.principalID].Name != "Alice" {
		t.Errorf("people: %+v", res.People)
	}
	res, _ = f.svc.Search(f.ctx, alice.principalID, f.projectID, "webhook", memories.Filter{Branch: "main"})
	if got := fmt.Sprint(hitTitles(res.Hits)); got != "[Newer webhook gotcha Billing notes]" {
		t.Errorf("with a branch filter: %s", got)
	}
	if _, err := f.svc.Search(f.ctx, alice.principalID, f.projectID, " ", memories.Filter{}); !errors.Is(err, memories.ErrInvalid) {
		t.Errorf("an empty query: %v", err)
	}
}

func TestSearchSaysWhenABoundCutTheAnswer(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	for i := range memories.MaxSearchHits + 1 {
		f.create(alice, f.projectID, memories.NewMemory{Title: fmt.Sprintf("needle %d", i)})
	}
	res, err := f.svc.Search(f.ctx, alice.principalID, f.projectID, "needle", memories.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != memories.MaxSearchHits || !res.Truncated {
		t.Errorf("hits %d truncated %v, want %d and true", len(res.Hits), res.Truncated, memories.MaxSearchHits)
	}
	if res.Hits[0].Memory.Title != fmt.Sprintf("needle %d", memories.MaxSearchHits) {
		t.Errorf("newest first: %q", res.Hits[0].Memory.Title)
	}
}

func TestSearchAllCoversEveryProjectTheCallerReadsAndNoOther(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	second := f.newProject("second")
	if err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		return f.addMember(tx, second, alice, projects.RoleReader)
	}); err != nil {
		t.Fatal(err)
	}
	foreign := f.newProject("foreign")
	bob := f.join(foreign, "Bob", projects.RoleOwner)
	wendy := f.join(second, "Wendy", projects.RoleContributor)

	f.create(alice, f.projectID, memories.NewMemory{Title: "Cache invalidation on deploy"})
	f.create(wendy, second, memories.NewMemory{Title: "Second project", Body: "cache keys are per tenant"})
	f.create(bob, foreign, memories.NewMemory{Title: "Foreign cache secret"})

	res, err := f.svc.SearchAll(f.ctx, alice.principalID, "cache", memories.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]string{}
	for _, h := range res.Hits {
		got[h.Memory.ProjectID] = h.Memory.Title
	}
	if len(res.Hits) != 2 || got[f.projectID] == "" || got[second] == "" || len(res.Projects) != 2 || res.Truncated {
		t.Errorf("alice's hits %+v projects %+v truncated %v", got, res.Projects, res.Truncated)
	}
	if _, ok := got[foreign]; ok {
		t.Errorf("a project alice is not in was searched")
	}
	res, err = f.svc.SearchAll(f.ctx, bob.principalID, "cache", memories.Filter{})
	if err != nil || len(res.Hits) != 1 || res.Hits[0].Memory.ProjectID != foreign {
		t.Errorf("bob's hits %+v, %v", res.Hits, err)
	}
}

func TestReadersReadWritersWriteAndOutsidersSeeNothing(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	rita := f.join(f.projectID, "Rita", projects.RoleReader)
	otherProject := f.newProject("elsewhere")
	otto := f.join(otherProject, "Otto", projects.RoleOwner)
	m := f.create(alice, f.projectID, memories.NewMemory{Title: "Visible to members"})

	if _, err := f.svc.Get(f.ctx, rita.principalID, f.projectID, m.ID); err != nil {
		t.Errorf("a reader could not read: %v", err)
	}
	if _, err := f.svc.Create(f.ctx, rita.principalID, &rita.clientID, f.projectID, memories.NewMemory{Title: "x"}); !errors.Is(err, memories.ErrForbidden) {
		t.Errorf("a reader wrote: %v", err)
	}
	if err := f.svc.Delete(f.ctx, rita.principalID, &rita.clientID, f.projectID, m.ID); !errors.Is(err, memories.ErrForbidden) {
		t.Errorf("a reader deleted: %v", err)
	}
	if _, err := f.svc.Get(f.ctx, otto.principalID, f.projectID, m.ID); !errors.Is(err, memories.ErrForbidden) {
		t.Errorf("an outsider read: %v", err)
	}
	// The outsider's own project cannot be used to reach this one's memory.
	if _, err := f.svc.Get(f.ctx, otto.principalID, otherProject, m.ID); !errors.Is(err, memories.ErrNotFound) {
		t.Errorf("a memory was reached through another project: %v", err)
	}
	if _, err := f.svc.Supersede(f.ctx, otto.principalID, otherProject, f.create(otto, otherProject, memories.NewMemory{Title: "mine"}).ID, m.ID); !errors.Is(err, memories.ErrInvalid) {
		t.Errorf("superseded by another project's memory: %v", err)
	}

	// Everything above goes through the service, whose capability check and
	// explicit project filter would pass with the policy replaced by
	// USING (true). This is the assertion that exercises RLS itself: the
	// whole table, through cc_app, as the outsider, filtered only to leave out
	// the outsider's own project.
	err := db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: otto.principalID}, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(f.ctx, `SELECT count(*) FROM memories WHERE project_id <> $1`, otherProject).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatalf("cc_app sees %d memory row(s) of another project — RLS is not isolating memories", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMemoryTextCountsAgainstTheQuota(t *testing.T) {
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
	m := f.create(alice, f.projectID, memories.NewMemory{Title: "Counted", Body: "some body"})
	if used() == 0 {
		t.Fatal("a memory's text was not counted")
	}
	if err := f.svc.Delete(f.ctx, alice.principalID, &alice.clientID, f.projectID, m.ID); err != nil {
		t.Fatal(err)
	}
	if after := used(); after != 0 {
		t.Errorf("used %d after deleting the only memory, want 0", after)
	}
	if err := f.svc.Delete(f.ctx, alice.principalID, &alice.clientID, f.projectID, m.ID); !errors.Is(err, memories.ErrNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestRecentIsTheNewestActiveMemories(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	for i := range 4 {
		f.create(alice, f.projectID, memories.NewMemory{Title: fmt.Sprintf("m%d", i)})
	}
	archived := f.create(alice, f.projectID, memories.NewMemory{Title: "archived"})
	if _, err := f.svc.SetStatus(f.ctx, alice.principalID, f.projectID, archived.ID, memories.StatusArchived); err != nil {
		t.Fatal(err)
	}
	list, more, err := f.svc.Recent(f.ctx, alice.principalID, f.projectID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(titles(list)); got != "[m3 m2 m1]" || !more {
		t.Errorf("recent %s more %v", got, more)
	}
}
