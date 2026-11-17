package retention_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/ideas"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/memories"
	"github.com/koduj-dev/context-commander/internal/plans"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/retention"
	"github.com/koduj-dev/context-commander/internal/storage"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

type bench struct {
	t         *testing.T
	ctx       context.Context
	owner     *db.Pool
	store     storage.ObjectStore
	artifacts *artifacts.Service
	memories  *memories.Service
	plans     *plans.Service
	ideas     *ideas.Service
	handoffs  *handoffs.Service
	history   *history.Service
	alice     uuid.UUID
	aliceUser uuid.UUID
}

func ck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newBench(t *testing.T) *bench {
	t.Helper()
	ctx := context.Background()
	dsns := dbtest.New(t)
	owner, err := db.NewPool(ctx, dsns.Owner)
	ck(t, err)
	t.Cleanup(owner.Close)
	app, err := db.NewPool(ctx, dsns.App)
	ck(t, err)
	t.Cleanup(app.Close)
	key, _ := crypto.GenerateRootKeyBase64()
	t.Setenv("CC_ROOT_KEY", key)
	root, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
	ck(t, err)
	store, err := filesystem.New(t.TempDir())
	ck(t, err)
	cp := crypto.NewProvider(root)
	authz := auth.NewAuthzService(app, projects.NewStore())
	pl := plans.NewService(app, cp, authz)
	b := &bench{t: t, ctx: ctx, owner: owner, store: store,
		artifacts: artifacts.NewService(app, cp, store, authz, gates.NewEngine(app)),
		memories:  memories.NewService(app, cp, authz), plans: pl, ideas: ideas.NewService(app, cp, authz, pl),
		handoffs: handoffs.NewService(app, cp, authz), history: history.NewService(app, cp, authz)}
	ck(t, db.WithoutPrincipal(ctx, owner, func(tx pgx.Tx) error {
		st := identity.NewStore()
		u, err := st.CreateUser(ctx, tx, "alice@example.test", "Alice")
		if err != nil {
			return err
		}
		p, err := st.CreatePrincipalForUser(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		b.alice, b.aliceUser = p.ID, u.ID
		_, err = projects.NewStore().CreatePersonalNamespace(ctx, tx, u.ID, "alice")
		return err
	}))
	return b
}

// project creates a project filled with one of every kind of content.
func (b *bench) project(slug string) uuid.UUID {
	b.t.Helper()
	var id uuid.UUID
	ck(b.t, db.WithPrincipal(b.ctx, b.owner, db.Principal{ID: b.alice}, func(tx pgx.Tx) error {
		p, err := projects.NewStore().CreateForOwner(b.ctx, tx, b.aliceUser, b.alice, "alice/"+slug, slug)
		id = p.ID
		return err
	}))
	_, err := b.artifacts.Write(b.ctx, b.alice, nil, id, "docs/a.md", "text/markdown", []byte("one"), 0)
	ck(b.t, err)
	_, err = b.artifacts.Write(b.ctx, b.alice, nil, id, "docs/a.md", "text/markdown", []byte("two"), 1)
	ck(b.t, err)
	plan, err := b.plans.CreatePlan(b.ctx, b.alice, nil, id, nil, "Plan", "", []plans.NewItem{{Text: "step"}})
	ck(b.t, err)
	done := plans.ItemDone
	_, err = b.plans.UpdateItem(b.ctx, b.alice, nil, id, plan.Items[0].ID, plans.ItemUpdate{Status: &done})
	ck(b.t, err)
	_, err = b.memories.Create(b.ctx, b.alice, nil, id, memories.NewMemory{Title: "note", PlanID: &plan.ID})
	ck(b.t, err)
	_, err = b.ideas.Create(b.ctx, b.alice, nil, id, ideas.NewIdea{Title: "idea"})
	ck(b.t, err)
	_, err = b.handoffs.Create(b.ctx, b.alice, nil, id, nil, handoffs.Body{Summary: "over"})
	ck(b.t, err)
	conv, err := b.history.CreateConversation(b.ctx, b.alice, id)
	ck(b.t, err)
	sess, err := b.history.CreateExternalSession(b.ctx, b.alice, id, conv.ID, "anthropic", "claude-code", "s", nil, "started", time.Now())
	ck(b.t, err)
	raw, err := b.history.AppendHistoryObject(b.ctx, b.alice, id, conv.ID, sess.ID, "raw", nil, []byte("hi"))
	ck(b.t, err)
	_, err = b.history.AppendHistoryObject(b.ctx, b.alice, id, conv.ID, sess.ID, "compacted", &raw.ID, []byte("h"))
	ck(b.t, err)
	return id
}

func (b *bench) exec(sql string, args ...any) {
	b.t.Helper()
	ck(b.t, db.WithoutPrincipal(b.ctx, b.owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(b.ctx, sql, args...)
		return err
	}))
}

// rowsLeft counts the project's rows in every table that has a project_id.
func (b *bench) rowsLeft(projectID uuid.UUID) map[string]int {
	b.t.Helper()
	left := map[string]int{}
	ck(b.t, db.WithoutPrincipal(b.ctx, b.owner, func(tx pgx.Tx) error {
		rows, err := tx.Query(b.ctx,
			`SELECT table_name FROM information_schema.columns WHERE table_schema = 'public' AND column_name = 'project_id'`)
		if err != nil {
			return err
		}
		tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, table := range append(tables, "projects") {
			if table == "project_purges" || table == "security_events" {
				continue // the record of the purge, and the security log, stay by design
			}
			col := "project_id"
			if table == "projects" {
				col = "id"
			}
			var n int
			if err := tx.QueryRow(b.ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s = $1`, pgx.Identifier{table}.Sanitize(), col), projectID).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				left[table] = n
			}
		}
		return nil
	}))
	return left
}

// A project past its grace period is purged entirely, rows and stored
// objects, leaving only a tombstone; a held one and its neighbours are not
// touched; long-deleted artifacts go, recently deleted ones stay.
func TestPurge(t *testing.T) {
	b := newBench(t)
	gone, kept, held := b.project("gone"), b.project("kept"), b.project("held")
	b.exec(`UPDATE projects SET deletion_requested_at = now() - interval '8 days', deletion_requested_by = $2,
	        purge_after = now() - interval '1 day' WHERE id = $1`, gone, b.alice)
	b.exec(`UPDATE projects SET legal_hold = true WHERE id = $1`, held)
	// Due, but held: the constraint forbids a hold on a scheduled project, so
	// the hold is checked by a direct purge below instead.

	// Deleted artifacts in the kept project: one long ago, one yesterday.
	_, err := b.artifacts.Write(b.ctx, b.alice, nil, kept, "old.md", "text/markdown", []byte("old"), 0)
	ck(t, err)
	_, err = b.artifacts.Write(b.ctx, b.alice, nil, kept, "recent.md", "text/markdown", []byte("recent"), 0)
	ck(t, err)
	ck(t, b.artifacts.Delete(b.ctx, b.alice, nil, kept, "old.md"))
	ck(t, b.artifacts.Delete(b.ctx, b.alice, nil, kept, "recent.md"))
	b.exec(`UPDATE artifacts SET deleted_at = now() - interval '40 days' WHERE project_id = $1 AND logical_path = 'old.md'`, kept)

	purger := retention.NewPurger(b.owner, b.store)
	dry, err := purger.Run(b.ctx, 30, true)
	ck(t, err)
	if len(dry.Projects) != 1 || dry.Artifacts != 1 || len(b.rowsLeft(gone)) == 0 {
		t.Fatalf("dry run %+v changed something or counted wrong", dry)
	}

	r, err := purger.Run(b.ctx, 30, false)
	ck(t, err)
	if len(r.Projects) != 1 || r.Projects[0] != "alice/gone" || r.Artifacts != 1 || r.Objects < 3 {
		t.Errorf("report %+v", r)
	}
	if left := b.rowsLeft(gone); len(left) != 0 {
		t.Errorf("rows left after purge: %v", left)
	}
	objects, err := b.store.List(b.ctx, fmt.Sprintf("projects/%s/", gone))
	ck(t, err)
	if len(objects) != 0 {
		t.Errorf("%d objects left", len(objects))
	}
	var reason string
	ck(t, db.WithoutPrincipal(b.ctx, b.owner, func(tx pgx.Tx) error {
		return tx.QueryRow(b.ctx, `SELECT reason FROM project_purges WHERE project_id = $1 AND path = 'alice/gone'`, gone).Scan(&reason)
	}))

	// The neighbour keeps everything but the long-deleted artifact.
	if _, content, err := b.artifacts.Read(b.ctx, b.alice, kept, "docs/a.md", nil); err != nil || string(content) != "two" {
		t.Errorf("kept project's artifact: %q, %v", content, err)
	}
	var paths []string
	ck(t, db.WithoutPrincipal(b.ctx, b.owner, func(tx pgx.Tx) error {
		rows, err := tx.Query(b.ctx, `SELECT logical_path FROM artifacts WHERE project_id = $1 ORDER BY 1`, kept)
		if err != nil {
			return err
		}
		paths, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}))
	if strings.Join(paths, ",") != "docs/a.md,recent.md" {
		t.Errorf("kept project's artifacts after purge: %v", paths)
	}

	// A held project refuses a direct purge and keeps its rows.
	if _, err := purger.PurgeProject(b.ctx, held, "test"); !errors.Is(err, retention.ErrHeld) {
		t.Errorf("purging a held project: %v", err)
	}
	if left := b.rowsLeft(held); left["artifacts"] == 0 {
		t.Errorf("held project lost rows: %v", left)
	}
	// Placing a hold cancels a scheduled deletion; lifting it allows purge.
	b.exec(`UPDATE projects SET legal_hold = false WHERE id = $1`, held)
	b.exec(`UPDATE projects SET deletion_requested_at = now(), deletion_requested_by = $2, purge_after = now() WHERE id = $1`, held, b.alice)
	ck(t, retention.SetLegalHold(b.ctx, b.owner, held, true, "litigation"))
	r, err = purger.Run(b.ctx, 0, false)
	ck(t, err)
	if len(r.Projects) != 0 {
		t.Errorf("a held project was purged: %+v", r)
	}
}

func (b *bench) count(sql string, args ...any) int {
	b.t.Helper()
	var n int
	ck(b.t, db.WithoutPrincipal(b.ctx, b.owner, func(tx pgx.Tx) error {
		return tx.QueryRow(b.ctx, sql, args...).Scan(&n)
	}))
	return n
}

// Age rules remove only what they name and only what is old enough: never
// an active memory, never a recent record, never anything in a held
// project; with no rules, nothing at all.
func TestRunAge(t *testing.T) {
	b := newBench(t)
	p, held := b.project("aged"), b.project("held")
	for _, id := range []uuid.UUID{p, held} {
		b.exec(`UPDATE memories SET status = 'archived', updated_at = now() - interval '400 days' WHERE project_id = $1`, id)
		_, err := b.memories.Create(b.ctx, b.alice, nil, id, memories.NewMemory{Title: "fresh"})
		ck(t, err)
		b.exec(`UPDATE history_objects SET created_at = now() - interval '400 days' WHERE project_id = $1`, id)
		b.exec(`UPDATE conversations SET created_at = now() - interval '400 days' WHERE project_id = $1`, id)
		b.exec(`UPDATE audit_log SET ts = now() - interval '400 days' WHERE project_id = $1`, id)
	}
	b.exec(`UPDATE projects SET legal_hold = true WHERE id = $1`, held)
	// An old active memory stays whatever its age.
	b.exec(`UPDATE memories SET updated_at = now() - interval '400 days' WHERE project_id = $1 AND status = 'active'`, p)
	auditBefore := b.count(`SELECT count(*) FROM audit_log WHERE project_id = $1`, p)

	purger := retention.NewPurger(b.owner, b.store)
	none, err := purger.RunAge(b.ctx, retention.AgeRules{}, false)
	ck(t, err)
	if none != (retention.AgeReport{}) {
		t.Fatalf("no rules removed %+v", none)
	}
	rules := retention.AgeRules{ArchivedMemoryDays: 365, HistoryDays: 365, AuditDays: 365}
	dry, err := purger.RunAge(b.ctx, rules, true)
	ck(t, err)
	if dry.Memories != 1 || dry.Conversations != 1 || dry.AuditRows != int64(auditBefore) {
		t.Fatalf("dry run %+v (audit rows %d)", dry, auditBefore)
	}
	r, err := purger.RunAge(b.ctx, rules, false)
	ck(t, err)
	if r != dry {
		t.Errorf("removed %+v, dry run said %+v", r, dry)
	}
	if n := b.count(`SELECT count(*) FROM memories WHERE project_id = $1`, p); n != 1 {
		t.Errorf("%d memories left, want the active one", n)
	}
	if n := b.count(`SELECT count(*) FROM conversations WHERE project_id = $1`, p); n != 0 {
		t.Errorf("%d conversations left", n)
	}
	if n := b.count(`SELECT count(*) FROM audit_log WHERE project_id = $1 AND ts < now() - interval '365 days'`, p); n != 0 {
		t.Errorf("%d old audit rows left", n)
	}
	for _, table := range []string{"memories", "conversations", "history_objects", "audit_log"} {
		if n := b.count(fmt.Sprintf(`SELECT count(*) FROM %s WHERE project_id = $1`, table), held); n == 0 {
			t.Errorf("held project lost its %s", table)
		}
	}
}
