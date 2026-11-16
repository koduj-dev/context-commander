package portability_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
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
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/plans"
	"github.com/koduj-dev/context-commander/internal/portability"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

// side is one installation: its database, its own root key and storage,
// and every service wired to one pool.
type side struct {
	t                *testing.T
	ctx              context.Context
	owner, app       *db.Pool
	artifacts        *artifacts.Service
	memories         *memories.Service
	plans            *plans.Service
	ideas            *ideas.Service
	handoffs         *handoffs.Service
	history          *history.Service
	appPort, ownPort *portability.Service
}

func ck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newSide(t *testing.T, keyEnv string) *side {
	t.Helper()
	ctx := context.Background()
	dsns := dbtest.New(t)
	connect := func(dsn string) *db.Pool {
		p, err := db.NewPool(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		return p
	}
	key, _ := crypto.GenerateRootKeyBase64()
	t.Setenv(keyEnv, key)
	root, err := crypto.NewEnvRootKeyProvider(keyEnv)
	if err != nil {
		t.Fatal(err)
	}
	store, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &side{t: t, ctx: ctx, owner: connect(dsns.Owner), app: connect(dsns.App)}
	build := func(pool *db.Pool) (*artifacts.Service, *memories.Service, *plans.Service, *ideas.Service, *handoffs.Service, *history.Service, *portability.Service) {
		cp := crypto.NewProvider(root)
		authz := auth.NewAuthzService(pool, projects.NewStore())
		pl := plans.NewService(pool, cp, authz)
		a := artifacts.NewService(pool, cp, store, authz, gates.NewEngine(pool))
		m := memories.NewService(pool, cp, authz)
		i := ideas.NewService(pool, cp, authz, pl)
		h := handoffs.NewService(pool, cp, authz)
		hi := history.NewService(pool, cp, authz)
		return a, m, pl, i, h, hi, portability.NewService(pool, authz, a, m, pl, i, h, hi)
	}
	s.artifacts, s.memories, s.plans, s.ideas, s.handoffs, s.history, s.appPort = build(s.app)
	_, _, _, _, _, _, s.ownPort = build(s.owner)
	return s
}

// user creates a user with a personal namespace named like the email's
// local part, and returns its principal and user ids.
func (s *side) user(email string) (principal, user uuid.UUID) {
	s.t.Helper()
	err := db.WithoutPrincipal(s.ctx, s.owner, func(tx pgx.Tx) error {
		st := identity.NewStore()
		u, err := st.CreateUser(s.ctx, tx, email, strings.Split(email, "@")[0])
		if err != nil {
			return err
		}
		p, err := st.CreatePrincipalForUser(s.ctx, tx, u.ID)
		if err != nil {
			return err
		}
		principal, user = p.ID, u.ID
		_, err = projects.NewStore().CreatePersonalNamespace(s.ctx, tx, u.ID, strings.Split(email, "@")[0])
		return err
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return principal, user
}

func (s *side) project(owner, ownerUser uuid.UUID, path string, members map[uuid.UUID]projects.Role) uuid.UUID {
	s.t.Helper()
	var id uuid.UUID
	err := db.WithPrincipal(s.ctx, s.owner, db.Principal{ID: owner}, func(tx pgx.Tx) error {
		p, err := projects.NewStore().CreateForOwner(s.ctx, tx, ownerUser, owner, path, "Work")
		if err != nil {
			return err
		}
		id = p.ID
		for m, role := range members {
			if _, err := projects.NewStore().AddMember(s.ctx, tx, p.ID, m, role); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return id
}

func (s *side) exec(sql string, args ...any) {
	s.t.Helper()
	if err := db.WithoutPrincipal(s.ctx, s.owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(s.ctx, sql, args...)
		return err
	}); err != nil {
		s.t.Fatal(err)
	}
}

// A project exported from one installation and imported into another, under
// a different root key: content, versions, statuses, cross-references and
// authors (mapped by email where the target knows the person) all survive.
func TestExportThenImportIntoAnotherInstallation(t *testing.T) {
	src := newSide(t, "CC_ROOT_KEY_SRC")
	ctx := src.ctx
	alice, aliceUser := src.user("alice@example.test")
	bob, _ := src.user("bob@example.test")
	p := src.project(alice, aliceUser, "alice/work", map[uuid.UUID]projects.Role{bob: projects.RoleContributor})

	if _, err := src.artifacts.Write(ctx, alice, nil, p, "docs/a.md", "text/markdown", []byte("first"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := src.artifacts.Write(ctx, bob, nil, p, "docs/a.md", "text/markdown", []byte("second"), 1); err != nil {
		t.Fatal(err)
	}
	phase, err := src.plans.CreatePhase(ctx, alice, p, "Search", "find things")
	ck(t, err)
	plan, err := src.plans.CreatePlan(ctx, alice, nil, p, &phase.ID, "Wire it", "", []plans.NewItem{{Text: "index"}, {Text: "query"}})
	ck(t, err)
	done, blocked := plans.ItemDone, plans.ItemBlocked
	note := "needs the VPN"
	if _, err := src.plans.UpdateItem(ctx, bob, nil, p, plan.Items[0].ID, plans.ItemUpdate{Status: &done}); err != nil {
		t.Fatal(err)
	}
	if _, err := src.plans.UpdateItem(ctx, bob, nil, p, plan.Items[1].ID, plans.ItemUpdate{Status: &blocked, Note: &note}); err != nil {
		t.Fatal(err)
	}
	old, err := src.memories.Create(ctx, bob, nil, p, memories.NewMemory{Kind: "decision", Title: "Use pgvector", PlanID: &plan.ID})
	ck(t, err)
	repl, err := src.memories.Create(ctx, alice, nil, p, memories.NewMemory{Kind: "decision", Title: "Vectors stay encrypted", Body: "why"})
	ck(t, err)
	if _, err := src.memories.Supersede(ctx, alice, p, old.ID, repl.ID); err != nil {
		t.Fatal(err)
	}
	idea, err := src.ideas.Create(ctx, bob, nil, p, ideas.NewIdea{Title: "Stream the export"})
	ck(t, err)
	if err := src.ideas.RestoreText(ctx, bob, p, idea.ID, "Use a pipe", ""); err != nil {
		t.Fatal(err)
	}
	src.exec(`UPDATE ideas SET status = 'proposed', proposed_at = now(), claimed_by_principal = $1 WHERE id = $2`, bob, idea.ID)
	h, err := src.handoffs.Create(ctx, alice, nil, p, nil, handoffs.Body{Summary: "over to you"})
	ck(t, err)
	if _, err := src.handoffs.Claim(ctx, bob, nil, p, h.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := src.handoffs.Close(ctx, bob, nil, p, h.ID); err != nil {
		t.Fatal(err)
	}
	conv, err := src.history.CreateConversation(ctx, alice, p)
	ck(t, err)
	sess, err := src.history.CreateExternalSession(ctx, alice, p, conv.ID, "anthropic", "claude-code", "s-1", nil, "started", time.Now().UTC())
	ck(t, err)
	raw, err := src.history.AppendHistoryObject(ctx, alice, p, conv.ID, sess.ID, "raw", nil, []byte("hello world"))
	ck(t, err)
	if _, err := src.history.AppendHistoryObject(ctx, alice, p, conv.ID, sess.ID, "compacted", &raw.ID, []byte("hello")); err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	manifest, err := src.appPort.Export(ctx, alice, p, &archive)
	ck(t, err)
	if !manifest.Complete || manifest.Counts["artifacts"] != 1 || manifest.Counts["memories"] != 2 {
		t.Fatalf("manifest %+v", manifest)
	}
	if _, err := src.appPort.Export(ctx, bob, p, io.Discard); !errors.Is(err, portability.ErrForbidden) {
		t.Errorf("a CONTRIBUTOR exporting: %v", err)
	}

	// The target knows bob by email, not alice; carol imports and owns it.
	dst := newSide(t, "CC_ROOT_KEY_DST")
	carol, carolUser := dst.user("carol@example.test")
	bobHere, _ := dst.user("bob@example.test")
	target := dst.project(carol, carolUser, "carol/moved", nil)
	files, _, err := portability.ReadArchive(bytes.NewReader(archive.Bytes()))
	ck(t, err)
	if err := portability.Validate(files); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.appPort.Import(ctx, carol, target, files); err == nil || !strings.Contains(err.Error(), "cc_owner") {
		t.Fatalf("import through cc_app: %v", err)
	}
	report, err := dst.ownPort.Import(ctx, carol, target, files)
	ck(t, err)
	if report.Mapped != 1 || report.Unmapped != 1 {
		t.Errorf("people mapped %d, unmapped %d", report.Mapped, report.Unmapped)
	}

	// Read back through the target's own services, as carol, under the
	// target's root key.
	versions, err := dst.artifacts.History(ctx, carol, target, "docs/a.md")
	ck(t, err)
	if len(versions) != 2 {
		t.Fatalf("versions %+v", versions)
	}
	authors := map[int]uuid.UUID{}
	for _, v := range versions {
		authors[v.Version] = v.CreatedByPrincipal
		n := v.Version
		_, content, err := dst.artifacts.Read(ctx, carol, target, "docs/a.md", &n)
		ck(t, err)
		if want := map[int]string{1: "first", 2: "second"}[n]; string(content) != want {
			t.Errorf("v%d = %q", n, content)
		}
	}
	if authors[1] != carol || authors[2] != bobHere {
		t.Errorf("authors %v, want v1 carol (alice unknown here) and v2 bob", authors)
	}

	phases, list, err := dst.plans.Snapshot(ctx, carol, target)
	ck(t, err)
	if len(phases) != 1 || len(list) != 1 || list[0].PhaseID == nil || *list[0].PhaseID != phases[0].ID {
		t.Fatalf("plans %+v %+v", phases, list)
	}
	items := list[0].Items
	if items[0].Status != plans.ItemDone || items[0].CompletedAt == nil || items[1].Status != plans.ItemBlocked ||
		items[1].Note != note || items[1].UpdatedBy == nil || *items[1].UpdatedBy != bobHere {
		t.Errorf("items %+v", items)
	}

	mems, err := dst.memories.List(ctx, carol, target, memories.Filter{Status: memories.StatusAny}, paging.Options{})
	ck(t, err)
	byTitle := map[string]memories.Memory{}
	for _, m := range mems.Items {
		byTitle[m.Title] = m
	}
	oldHere, replHere := byTitle["Use pgvector"], byTitle["Vectors stay encrypted"]
	if oldHere.SupersededBy == nil || *oldHere.SupersededBy != replHere.ID || oldHere.CreatedBy != bobHere ||
		oldHere.PlanID == nil || *oldHere.PlanID != list[0].ID {
		t.Errorf("memories %+v", byTitle)
	}

	ideaList, err := dst.ideas.List(ctx, carol, target, nil, paging.Options{})
	ck(t, err)
	if len(ideaList.Items) != 1 || ideaList.Items[0].Status != ideas.StatusProposed {
		t.Fatalf("ideas %+v", ideaList.Items)
	}
	ideaHere, err := dst.ideas.Get(ctx, carol, target, ideaList.Items[0].ID)
	ck(t, err)
	if ideaHere.Proposal != "Use a pipe" || ideaHere.ClaimedBy == nil || *ideaHere.ClaimedBy != bobHere {
		t.Errorf("idea %+v", ideaHere)
	}

	hs, err := dst.handoffs.List(ctx, carol, target, nil, paging.Options{})
	ck(t, err)
	if len(hs.Items) != 1 || hs.Items[0].Status != handoffs.StatusClosed || hs.Items[0].ClosedByPrincipal == nil || *hs.Items[0].ClosedByPrincipal != bobHere {
		t.Errorf("handoffs %+v", hs.Items)
	}

	convs, err := dst.history.ListConversations(ctx, carol, target, paging.Options{})
	ck(t, err)
	objs, err := dst.history.ListHistoryObjects(ctx, carol, target, convs.Items[0].ID, paging.Options{})
	ck(t, err)
	var rawHere uuid.UUID
	for _, o := range objs.Items {
		if o.Type == "raw" {
			rawHere = o.ID
		}
	}
	for _, o := range objs.Items {
		if o.Type == "compacted" && (o.DerivedFrom == nil || *o.DerivedFrom != rawHere || string(o.Body) != "hello") {
			t.Errorf("compacted object %+v", o)
		}
	}
}

// An archive whose export did not finish, or whose blob was altered, is
// refused before anything is written.
func TestBrokenArchivesAreRefused(t *testing.T) {
	src := newSide(t, "CC_ROOT_KEY_SRC")
	alice, aliceUser := src.user("alice@example.test")
	p := src.project(alice, aliceUser, "alice/work", nil)
	if _, err := src.artifacts.Write(src.ctx, alice, nil, p, "a.md", "text/markdown", []byte("content"), 0); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if _, err := src.appPort.Export(src.ctx, alice, p, &archive); err != nil {
		t.Fatal(err)
	}

	rewrite := func(change func(name string, data []byte) []byte) []byte {
		gz, _ := gzip.NewReader(bytes.NewReader(archive.Bytes()))
		tr := tar.NewReader(gz)
		var out bytes.Buffer
		zw := gzip.NewWriter(&out)
		tw := tar.NewWriter(zw)
		for {
			h, err := tr.Next()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(tr)
			if data = change(h.Name, data); data == nil {
				continue
			}
			h.Size = int64(len(data))
			_ = tw.WriteHeader(h)
			_, _ = tw.Write(data)
		}
		_ = tw.Close()
		_ = zw.Close()
		return out.Bytes()
	}

	noManifest := rewrite(func(name string, d []byte) []byte {
		if name == "manifest.json" {
			return nil
		}
		return d
	})
	if _, _, err := portability.ReadArchive(bytes.NewReader(noManifest)); !errors.Is(err, portability.ErrBadArchive) {
		t.Errorf("no manifest: %v", err)
	}
	incomplete := rewrite(func(name string, d []byte) []byte {
		if name == "manifest.json" {
			return bytes.Replace(d, []byte(`"complete": true`), []byte(`"complete": false`), 1)
		}
		return d
	})
	if _, _, err := portability.ReadArchive(bytes.NewReader(incomplete)); !errors.Is(err, portability.ErrBadArchive) {
		t.Errorf("incomplete: %v", err)
	}
	tampered := rewrite(func(name string, d []byte) []byte {
		if strings.HasPrefix(name, "blobs/") {
			return []byte("something else")
		}
		return d
	})
	files, _, err := portability.ReadArchive(bytes.NewReader(tampered))
	if err != nil {
		t.Fatal(err)
	}
	if err := portability.Validate(files); !errors.Is(err, portability.ErrBadArchive) {
		t.Errorf("tampered blob: %v", err)
	}
}
