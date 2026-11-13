package embeddings_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/embeddings"
	"github.com/koduj-dev/context-commander/internal/embeddings/embeddingstest"
	"github.com/koduj-dev/context-commander/internal/memories"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type fixture struct {
	t         *testing.T
	ctx       context.Context
	ownerPool *db.Pool
	appPool   *db.Pool
	crypto    *crypto.Provider
	mem       *memories.Service
	fake      *embeddingstest.Fake
	store     embeddings.Store
	ix        *embeddings.Index
}

func newFixture(t *testing.T, dsns dbtest.DSNs, pgvector bool) *fixture {
	t.Helper()
	ctx := context.Background()
	ownerPool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatal(err)
	}
	appPool, err := db.NewPool(ctx, dsns.App)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ownerPool.Close(); appPool.Close() })
	key, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_ROOT_KEY", key)
	root, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, ctx: ctx, ownerPool: ownerPool, appPool: appPool, crypto: crypto.NewProvider(root), fake: &embeddingstest.Fake{}}
	f.mem = memories.NewService(appPool, f.crypto, auth.NewAuthzService(appPool, projects.NewStore()))
	if pgvector {
		if err := db.CheckPgvectorSchema(ctx, appPool); err != nil {
			t.Fatalf("pgvector schema check as cc_app: %v", err)
		}
		f.store = embeddings.NewPgvectorStore()
	} else {
		f.store = embeddings.NewEncryptedStore(f.crypto, 0)
	}
	f.ix = f.newIndex(f.fake)
	return f
}

func (f *fixture) newIndex(p embeddings.Provider) *embeddings.Index {
	return embeddings.NewIndex(p, f.store, f.appPool, f.mem, embeddings.Options{
		MinSimilarity: 0.5, SweepInterval: -1, Backoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond, MaxAttempts: 3,
	})
}

// run starts the index's worker until the test ends.
func (f *fixture) run() {
	ctx, cancel := context.WithCancel(f.ctx)
	f.t.Cleanup(cancel)
	go f.ix.Run(ctx)
}

// project seeds a project with one owner and returns both.
func (f *fixture) project(slug string) (projectID, owner uuid.UUID) {
	f.t.Helper()
	projectID, owner = uuid.New(), uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		user, ns := uuid.New(), uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, 'Owner')`, user, user.String()+"@example.test"); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, owner, user); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`, ns, "ns-"+ns.String()[:8], user); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, $3, $3)`, projectID, ns, slug); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'OWNER')`, projectID, owner)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed project: %v", err)
	}
	return projectID, owner
}

func (f *fixture) create(projectID, principal uuid.UUID, title, body string) memories.Memory {
	f.t.Helper()
	m, err := f.mem.Create(f.ctx, principal, nil, projectID, memories.NewMemory{Title: title, Body: body})
	if err != nil {
		f.t.Fatalf("create memory: %v", err)
	}
	return m
}

func (f *fixture) pendingCount() int64 {
	return f.ix.Status(f.ctx).Pending
}

// waitIndexed waits until nothing is pending, or fails.
func (f *fixture) waitIndexed() {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if f.pendingCount() == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("still %d memories pending after 10s", f.pendingCount())
}

// A memory is embedded after it is written, without the write waiting, and
// a search by meaning finds it in another language and with none of its
// words, while a text search for the same query finds nothing.
func TestMemoriesAreEmbeddedAndFoundByMeaning(t *testing.T) {
	f := newFixture(t, dbtest.New(t), false)
	testFoundByMeaning(t, f)
}

func testFoundByMeaning(t *testing.T, f *fixture) {
	projectID, owner := f.project("work")
	f.mem.EnableSemantic(f.ix)
	f.run()
	slow := f.create(projectID, owner, "The database is slow under load", "Every list query does a sequential scan.")
	f.create(projectID, owner, "Release checklist", "Tag, build, publish the deployment notes.")
	f.waitIndexed()

	const czech = "pomalá databáze"
	text, err := f.mem.Search(f.ctx, owner, projectID, czech, memories.ModeText, memories.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(text.Hits) != 0 {
		t.Fatalf("text search for %q found %d hits; the test needs a query with none of the words", czech, len(text.Hits))
	}
	sem, err := f.mem.Search(f.ctx, owner, projectID, czech, memories.ModeSemantic, memories.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sem.Hits) != 1 || sem.Hits[0].Memory.ID != slow.ID || sem.Hits[0].Match != memories.MatchSemantic || sem.Hits[0].Score < 0.5 {
		t.Fatalf("semantic search: %+v", sem.Hits)
	}
	if sem.Hits[0].Snippet == "" {
		t.Fatal("a semantic hit has no snippet")
	}
	hybrid, err := f.mem.Search(f.ctx, owner, projectID, czech, "", memories.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if hybrid.Mode != memories.ModeHybrid || len(hybrid.Hits) != 1 || hybrid.Degraded {
		t.Fatalf("default search with a provider: mode %s hits %d degraded %v", hybrid.Mode, len(hybrid.Hits), hybrid.Degraded)
	}

	// Metadata filters still apply to a semantic search: archived is out.
	if _, err := f.mem.SetStatus(f.ctx, owner, projectID, slow.ID, memories.StatusArchived); err != nil {
		t.Fatal(err)
	}
	sem, _ = f.mem.Search(f.ctx, owner, projectID, czech, memories.ModeSemantic, memories.Filter{})
	if len(sem.Hits) != 0 {
		t.Fatalf("an archived memory was a semantic hit: %+v", sem.Hits)
	}
	sem, _ = f.mem.Search(f.ctx, owner, projectID, czech, memories.ModeSemantic, memories.Filter{Status: memories.StatusAny})
	if len(sem.Hits) != 1 {
		t.Fatalf("status=any should include the archived memory: %+v", sem.Hits)
	}
}

// Editing a memory's text makes its vector stale; the index embeds the new
// version, and the search follows the new meaning.
func TestAnEditIsEmbeddedAgain(t *testing.T) {
	f := newFixture(t, dbtest.New(t), false)
	projectID, owner := f.project("work")
	f.mem.EnableSemantic(f.ix)
	f.run()
	m := f.create(projectID, owner, "Webhook callbacks arrive twice", "")
	f.waitIndexed()
	title := "Login fails after a password change"
	if _, err := f.mem.Update(f.ctx, owner, projectID, m.ID, memories.Update{ExpectedVersion: 1, Title: &title}); err != nil {
		t.Fatal(err)
	}
	f.waitIndexed()
	res, err := f.mem.Search(f.ctx, owner, projectID, "přihlášení heslo", memories.ModeSemantic, memories.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("the edited meaning is not found: %+v", res.Hits)
	}
	res, _ = f.mem.Search(f.ctx, owner, projectID, "webhook notifikace", memories.ModeSemantic, memories.Filter{})
	if len(res.Hits) != 0 {
		t.Fatalf("the old meaning is still found: %+v", res.Hits)
	}
}

// The vector is stored sealed: the row's bytes are not the vector, it
// opens only with the project's key, and it decrypts back to what the
// provider returned.
func TestEncryptedStoreSealsVectors(t *testing.T) {
	f := newFixture(t, dbtest.New(t), false)
	projectID, owner := f.project("work")
	m := f.create(projectID, owner, "Webhook retries need backoff", "")
	if _, err := f.ix.Reindex(f.ctx, false, nil); err != nil {
		t.Fatal(err)
	}
	var ciphertext []byte
	var dims, version int
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `SELECT ciphertext, dimensions, memory_version FROM memory_embeddings WHERE memory_id = $1`, m.ID).Scan(&ciphertext, &dims, &version)
	})
	if err != nil {
		t.Fatal(err)
	}
	want := embeddingstest.Vector(embeddings.Text(m))
	plain := make([]byte, 4*len(want))
	for i, x := range want {
		binary.LittleEndian.PutUint32(plain[4*i:], math.Float32bits(x))
	}
	if bytes.Contains(ciphertext, plain[:16]) || dims != len(want) || version != 1 {
		t.Fatalf("stored row: dims %d version %d, plaintext visible %v", dims, version, bytes.Contains(ciphertext, plain[:16]))
	}
	// A fresh store has an empty cache, so this decrypts from the row.
	fresh := embeddings.NewEncryptedStore(f.crypto, 0)
	var got embeddings.Vector
	var ok bool
	err = db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: owner}, func(tx pgx.Tx) error {
		var err error
		got, ok, err = fresh.Get(f.ctx, tx, projectID, m.ID, f.fake.Model())
		return err
	})
	if err != nil || !ok {
		t.Fatalf("get: ok %v err %v", ok, err)
	}
	for i := range want {
		if got.Values[i] != want[i] {
			t.Fatalf("decrypted %v, want %v", got.Values, want)
		}
	}
}

// RLS on the vectors, as cc_app: a member of another project sees none of
// them, neither by reading the table nor through the store.
func TestVectorsAreInvisibleAcrossProjects(t *testing.T) {
	f := newFixture(t, dbtest.New(t), false)
	testVectorsInvisibleAcrossProjects(t, f, "memory_embeddings")
}

func testVectorsInvisibleAcrossProjects(t *testing.T, f *fixture, table string) {
	projectA, alice := f.project("a")
	projectB, bob := f.project("b")
	m := f.create(projectA, alice, "Database is slow", "")
	f.create(projectB, bob, "Unrelated", "")
	if _, err := f.ix.Reindex(f.ctx, false, nil); err != nil {
		t.Fatal(err)
	}
	count := func(principal uuid.UUID) int {
		var n int
		err := db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: principal}, func(tx pgx.Tx) error {
			// table is one of two constants in this file.
			return tx.QueryRow(f.ctx, `SELECT count(*) FROM `+table+` WHERE memory_id = $1`, m.ID).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(alice) != 1 {
		t.Fatal("the author cannot see the vector; the negative check below would prove nothing")
	}
	if n := count(bob); n != 0 {
		t.Fatalf("a member of another project sees %d vectors of this one", n)
	}
	var scored []embeddings.Scored
	err := db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: bob}, func(tx pgx.Tx) error {
		var err error
		scored, err = f.store.Score(f.ctx, tx, projectA, f.fake.Model(), embeddingstest.Vector("database slow"),
			[]embeddings.Ref{{ID: m.ID, Version: m.Version}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(scored) != 0 {
		t.Fatalf("another project's member scored this project's vector: %+v", scored)
	}
	// And the search itself refuses before any of that.
	if _, err := f.mem.Search(f.ctx, bob, projectA, "database", memories.ModeText, memories.Filter{}); err == nil {
		t.Fatal("a non-member searched the project")
	}
}

// Reindex backfills memories written before semantic search was on, and a
// model change makes every vector stale until it runs again.
func TestReindexBackfillsAndFollowsTheModel(t *testing.T) {
	f := newFixture(t, dbtest.New(t), false)
	projectID, owner := f.project("work")
	for _, title := range []string{"Webhook retries", "Slow database", "Release notes"} {
		f.create(projectID, owner, title, "")
	}
	if n := f.pendingCount(); n != 3 {
		t.Fatalf("pending before reindex = %d, want 3", n)
	}
	res, err := f.ix.Reindex(f.ctx, false, nil)
	if err != nil || res.Embedded != 3 {
		t.Fatalf("reindex: %+v %v", res, err)
	}
	if n := f.pendingCount(); n != 0 {
		t.Fatalf("pending after reindex = %d", n)
	}
	if res, _ := f.ix.Reindex(f.ctx, false, nil); res.Embedded != 0 {
		t.Fatalf("a second reindex embedded %d memories again", res.Embedded)
	}
	if res, _ := f.ix.Reindex(f.ctx, true, nil); res.Embedded != 3 {
		t.Fatalf("reindex --all embedded %d, want 3", res.Embedded)
	}

	other := &embeddingstest.Fake{ModelName: "other-model"}
	f.ix = f.newIndex(other)
	if n := f.pendingCount(); n != 3 {
		t.Fatalf("after a model change pending = %d, want 3", n)
	}
	if res, err := f.ix.Reindex(f.ctx, false, nil); err != nil || res.Embedded != 3 {
		t.Fatalf("reindex for the new model: %+v %v", res, err)
	}
	// Vectors of the old model are dropped when the new one is stored, so
	// a model change does not leave a second copy of every memory behind.
	var rows int
	_ = db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `SELECT count(*) FROM memory_embeddings`).Scan(&rows)
	})
	if rows != 3 {
		t.Fatalf("%d vector rows after a model change, want 3", rows)
	}
}

// The sweep at start embeds what was written while nothing was running,
// and a provider that is down is retried, not fatal: the memory stays
// pending until it answers.
func TestTheSweepCatchesUpAfterAnOutage(t *testing.T) {
	f := newFixture(t, dbtest.New(t), false)
	projectID, owner := f.project("work")
	f.create(projectID, owner, "Written before the server started", "")
	f.fake.SetDown(true)
	f.mem.EnableSemantic(f.ix)
	f.run()
	f.create(projectID, owner, "Written while the provider was down", "")
	time.Sleep(200 * time.Millisecond)
	if n := f.pendingCount(); n != 2 {
		t.Fatalf("pending while the provider is down = %d, want 2", n)
	}
	st := f.ix.Status(f.ctx)
	if st.Reachable == nil || *st.Reachable || st.LastError == "" {
		t.Fatalf("status while down: %+v", st)
	}
	// Hybrid search keeps working, by text, and says so.
	res, err := f.mem.Search(f.ctx, owner, projectID, "provider", "", memories.Filter{})
	if err != nil || !res.Degraded || len(res.Hits) != 1 {
		t.Fatalf("hybrid while down: %+v %v", res, err)
	}
	f.fake.SetDown(false)
	// A fresh index sweeps at start, as a restarted server would.
	f.ix = f.newIndex(f.fake)
	f.run()
	f.waitIndexed()
	st = f.ix.Status(f.ctx)
	if st.Reachable == nil || !*st.Reachable {
		t.Fatalf("status after recovery: %+v", st)
	}
}

// Related is the nearest memories by meaning, never the memory itself, and
// works for a memory not embedded yet by embedding it on the spot.
func TestRelatedMemories(t *testing.T) {
	f := newFixture(t, dbtest.New(t), false)
	projectID, owner := f.project("work")
	f.mem.EnableSemantic(f.ix)
	target := f.create(projectID, owner, "Webhook callbacks need retries with backoff", "")
	near := f.create(projectID, owner, "Zpětné volání webhooku: opakování", "")
	f.create(projectID, owner, "Login page copy", "")
	if _, err := f.ix.Reindex(f.ctx, false, nil); err != nil {
		t.Fatal(err)
	}
	res, err := f.mem.Related(f.ctx, owner, projectID, target.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Memory.ID != near.ID {
		t.Fatalf("related: %+v", res.Hits)
	}
	fresh := f.create(projectID, owner, "Webhook notifikace", "") // not indexed: no worker running
	res, err = f.mem.Related(f.ctx, owner, projectID, fresh.ID, 5)
	if err != nil || len(res.Hits) < 2 {
		t.Fatalf("related for an unindexed memory: %+v %v", res.Hits, err)
	}
	for _, h := range res.Hits {
		if h.Memory.ID == fresh.ID {
			t.Fatal("a memory is related to itself")
		}
	}
	if _, err := f.mem.Related(f.ctx, owner, projectID, target.ID, memories.MaxRelated+1); err == nil {
		t.Fatal("an oversized limit was accepted")
	}
}

// The same behaviour on plaintext pgvector storage, on the image that has
// the extension, with the optional migration set applied as cc_owner.
func TestPgvectorStore(t *testing.T) {
	dsns := dbtest.NewPgvector(t)
	t.Run("found by meaning", func(t *testing.T) {
		testFoundByMeaning(t, newFixture(t, dsns, true))
	})
	t.Run("invisible across projects", func(t *testing.T) {
		testVectorsInvisibleAcrossProjects(t, newFixture(t, dbtest.NewPgvector(t), true), "memory_vectors")
	})
}

// The server refuses pgvector storage on a database without the optional
// schema, which is every database the core migrations alone produced.
func TestPgvectorSchemaIsRequired(t *testing.T) {
	dsns := dbtest.New(t)
	pool, err := db.NewPool(context.Background(), dsns.App)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.CheckPgvectorSchema(context.Background(), pool); err == nil {
		t.Fatal("pgvector storage accepted on a database without the pgvector schema")
	}
	if err := db.MigratePgvector(context.Background(), dsns.Owner); err == nil {
		t.Fatal("the pgvector migrations ran on a database without the extension")
	}
}
