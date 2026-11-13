package embeddings

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/koduj-dev/context-commander/internal/crypto"
)

// Store kinds, as the --embeddings-store flag spells them.
const (
	StoreEncrypted = "encrypted"
	StorePgvector  = "pgvector"
)

// Vector is one memory's embedding under one model. MemoryVersion is the
// memory version whose text it was made from: a memory edited since is
// stale and gets embedded again.
type Vector struct {
	ProjectID     uuid.UUID
	MemoryID      uuid.UUID
	MemoryVersion int
	Model         string
	Values        []float32
}

// Ref is a memory a search may return, as the caller read it under RLS.
type Ref struct {
	ID      uuid.UUID
	Version int
}

// Scored is a memory's similarity to a query.
type Scored struct {
	ID         uuid.UUID
	Similarity float64
}

// Job is a memory to embed and the principal to read it as. The indexer is
// not a request, but it reads and writes under RLS like one: as the writer
// who caused it, or, for the sweep, as a current member of the project
// (docs/adr/0028).
type Job struct {
	ProjectID   uuid.UUID
	MemoryID    uuid.UUID
	PrincipalID uuid.UUID
}

// Store keeps vectors. Every method runs in the caller's transaction, so it
// is under the RLS context the caller set.
type Store interface {
	Kind() string
	// Put stores v, replacing an older vector for the same memory and model
	// and dropping the memory's vectors under any other model. A vector for
	// an older memory version than the one stored is ignored, so a slow
	// embed cannot overwrite a newer one.
	Put(ctx context.Context, tx pgx.Tx, v Vector) error
	Get(ctx context.Context, tx pgx.Tx, projectID, memoryID uuid.UUID, model string) (Vector, bool, error)
	// Score is the similarity of q to each of refs that has a vector under
	// model. Refs without one are left out; the indexer will get to them.
	Score(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, model string, q []float32, refs []Ref) ([]Scored, error)
	// Pending lists memories with no vector under model, or one made from
	// an older version, by memory id after after. all lists every memory.
	Pending(ctx context.Context, tx pgx.Tx, model string, after uuid.UUID, limit int, all bool) ([]Job, error)
	PendingCount(ctx context.Context, tx pgx.Tx, model string) (int64, error)
}

// isMissingMemory is a Put for a memory deleted while it was being
// embedded: the foreign key refuses it, and there is nothing left to index.
func isMissingMemory(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23503"
}

func pending(ctx context.Context, tx pgx.Tx, fn, model string, after uuid.UUID, limit int, all bool) ([]Job, error) {
	// fn is one of two constant names from this package, never input.
	rows, err := tx.Query(ctx, `SELECT project_id, memory_id, principal_id FROM `+fn+`($1, $2, $3, $4)`, model, after, limit, all)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Job, error) {
		var j Job
		var p *uuid.UUID
		if err := r.Scan(&j.ProjectID, &j.MemoryID, &p); err != nil {
			return j, err
		}
		// A project with no member left has nobody to read it as; the job
		// comes back with no principal and is skipped.
		if p != nil {
			j.PrincipalID = *p
		}
		return j, nil
	})
}

// ---------------------------------------------------------------- encrypted

// EncryptedStore keeps each vector sealed with the project key, as CONTENT
// PAYLOAD (docs/adr/0011): a vector says what the text means, and anyone who
// can read the database should learn no more from it than from the text.
// Similarity is computed in Go over vectors decrypted into a bounded cache.
type EncryptedStore struct {
	crypto *crypto.Provider
	cache  *vectorCache
}

// DefaultCacheBytes bounds the decrypted vectors held in memory: 64 MiB is
// about 16,000 vectors of 1024 dimensions (bge-m3), or sixteen projects
// searched at the full 1000-candidate scan.
const DefaultCacheBytes = 64 << 20

// NewEncryptedStore keeps at most cacheBytes of decrypted vectors in memory.
// Zero or less means DefaultCacheBytes.
func NewEncryptedStore(c *crypto.Provider, cacheBytes int64) *EncryptedStore {
	if cacheBytes <= 0 {
		cacheBytes = DefaultCacheBytes
	}
	return &EncryptedStore{crypto: c, cache: newVectorCache(cacheBytes)}
}

func (*EncryptedStore) Kind() string { return StoreEncrypted }

func embeddingContext(projectID, memoryID uuid.UUID, version int) crypto.EncryptionContext {
	return crypto.EncryptionContext{
		ProjectID: projectID, ResourceType: "memory_embedding",
		ResourceID: memoryID, ResourceVersion: uint64(version), //nolint:gosec // a small positive counter
	}
}

func (s *EncryptedStore) Put(ctx context.Context, tx pgx.Tx, v Vector) error {
	ct, nonce, dek, keyVersion, err := s.crypto.Encrypt(ctx, tx, embeddingContext(v.ProjectID, v.MemoryID, v.MemoryVersion), encode(v.Values))
	if err != nil {
		return fmt.Errorf("embeddings: encrypt: %w", err)
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO memory_embeddings (memory_id, model, project_id, memory_version, dimensions, ciphertext, nonce, wrapped_dek, project_key_version)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (memory_id, model) DO UPDATE SET memory_version = EXCLUDED.memory_version, dimensions = EXCLUDED.dimensions,
		   ciphertext = EXCLUDED.ciphertext, nonce = EXCLUDED.nonce, wrapped_dek = EXCLUDED.wrapped_dek,
		   project_key_version = EXCLUDED.project_key_version, updated_at = now()
		 WHERE memory_embeddings.memory_version <= EXCLUDED.memory_version`,
		v.MemoryID, v.Model, v.ProjectID, v.MemoryVersion, len(v.Values), ct, nonce, dek, keyVersion)
	if isMissingMemory(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM memory_embeddings WHERE memory_id = $1 AND model <> $2`, v.MemoryID, v.Model); err != nil {
		return err
	}
	// Dropped rather than replaced: the transaction may still roll back,
	// and a cache that ran ahead of the database would serve a vector that
	// was never stored. The next read loads it.
	s.cache.forget(v.MemoryID)
	return nil
}

type sealedRow struct {
	memoryID       uuid.UUID
	version        int
	ct, nonce, dek []byte
	keyVersion     int
}

func (s *EncryptedStore) load(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, model string, ids []uuid.UUID) ([]Vector, error) {
	rows, err := tx.Query(ctx,
		`SELECT memory_id, memory_version, ciphertext, nonce, wrapped_dek, project_key_version
		 FROM memory_embeddings WHERE project_id = $1 AND model = $2 AND memory_id = ANY($3)`, projectID, model, ids)
	if err != nil {
		return nil, err
	}
	sealed, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (sealedRow, error) {
		var x sealedRow
		err := r.Scan(&x.memoryID, &x.version, &x.ct, &x.nonce, &x.dek, &x.keyVersion)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	// Decrypted after the rows are closed: Decrypt runs a query of its own.
	out := make([]Vector, 0, len(sealed))
	for _, x := range sealed {
		plain, err := s.crypto.Decrypt(ctx, tx, embeddingContext(projectID, x.memoryID, x.version), x.ct, x.nonce, x.dek, x.keyVersion)
		if err != nil {
			return nil, fmt.Errorf("embeddings: decrypt: %w", err)
		}
		values, err := decode(plain)
		if err != nil {
			return nil, err
		}
		v := Vector{ProjectID: projectID, MemoryID: x.memoryID, MemoryVersion: x.version, Model: model, Values: values}
		s.cache.put(v)
		out = append(out, v)
	}
	return out, nil
}

func (s *EncryptedStore) Get(ctx context.Context, tx pgx.Tx, projectID, memoryID uuid.UUID, model string) (Vector, bool, error) {
	if v, ok := s.cache.get(memoryID, model); ok && v.ProjectID == projectID {
		return v, true, nil
	}
	list, err := s.load(ctx, tx, projectID, model, []uuid.UUID{memoryID})
	if err != nil || len(list) == 0 {
		return Vector{}, false, err
	}
	return list[0], true, nil
}

// Score serves what the cache holds at the version the caller read and
// decrypts the rest in one query. The cache is keyed by memory id and
// checked against the project and version the caller's RLS read returned,
// so it never answers for a memory the caller could not see.
func (s *EncryptedStore) Score(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, model string, q []float32, refs []Ref) ([]Scored, error) {
	var have []Vector
	var miss []uuid.UUID
	for _, r := range refs {
		if v, ok := s.cache.get(r.ID, model); ok && v.ProjectID == projectID && v.MemoryVersion == r.Version {
			have = append(have, v)
			continue
		}
		miss = append(miss, r.ID)
	}
	if len(miss) > 0 {
		loaded, err := s.load(ctx, tx, projectID, model, miss)
		if err != nil {
			return nil, err
		}
		have = append(have, loaded...)
	}
	out := make([]Scored, 0, len(have))
	for _, v := range have {
		if sim, ok := Cosine(q, v.Values); ok {
			out = append(out, Scored{ID: v.MemoryID, Similarity: sim})
		}
	}
	return out, nil
}

func (s *EncryptedStore) Pending(ctx context.Context, tx pgx.Tx, model string, after uuid.UUID, limit int, all bool) ([]Job, error) {
	return pending(ctx, tx, "cc_memory_embeddings_pending", model, after, limit, all)
}

func (s *EncryptedStore) PendingCount(ctx context.Context, tx pgx.Tx, model string) (int64, error) {
	var n int64
	err := tx.QueryRow(ctx, `SELECT cc_memory_embeddings_pending_count($1)`, model).Scan(&n)
	return n, err
}

// CacheBytes is what the decrypted-vector cache holds now and at most.
func (s *EncryptedStore) CacheBytes() (used, limit int64) { return s.cache.size() }

// ---------------------------------------------------------------- cache

// vectorCache is a least-recently-used cache of decrypted vectors, bounded
// by bytes rather than entries because a vector's size depends on the model.
type vectorCache struct {
	mu      sync.Mutex
	max     int64
	used    int64
	order   *list.List // front is most recent; values are *Vector
	entries map[uuid.UUID]*list.Element
}

func newVectorCache(max int64) *vectorCache {
	return &vectorCache{max: max, order: list.New(), entries: map[uuid.UUID]*list.Element{}}
}

// entryBytes is a vector's share of the bound: its floats plus the
// bookkeeping around them, so a cache of tiny vectors is bounded too.
func entryBytes(v *Vector) int64 { return int64(4*len(v.Values)) + 128 }

func (c *vectorCache) get(id uuid.UUID, model string) (Vector, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok {
		return Vector{}, false
	}
	v := e.Value.(*Vector)
	if v.Model != model {
		return Vector{}, false
	}
	c.order.MoveToFront(e)
	return *v, true
}

func (c *vectorCache) put(v Vector) {
	size := entryBytes(&v)
	if size > c.max {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[v.MemoryID]; ok {
		c.used -= entryBytes(e.Value.(*Vector))
		c.order.Remove(e)
	}
	c.entries[v.MemoryID] = c.order.PushFront(&v)
	c.used += size
	for c.used > c.max {
		last := c.order.Back()
		old := last.Value.(*Vector)
		c.order.Remove(last)
		delete(c.entries, old.MemoryID)
		c.used -= entryBytes(old)
	}
}

func (c *vectorCache) forget(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[id]; ok {
		c.used -= entryBytes(e.Value.(*Vector))
		c.order.Remove(e)
		delete(c.entries, id)
	}
}

func (c *vectorCache) size() (int64, int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.used, c.max
}

// ---------------------------------------------------------------- pgvector

// PgvectorStore keeps vectors in a plaintext pgvector column and computes
// similarity in the database. It is the documented exception to ADR 0011: a
// vector leaks what the text is about to anyone who can read the database.
// Opt-in only, and only on a database with the pgvector migrations applied
// (ccd migrate --pgvector; docs/adr/0028).
type PgvectorStore struct{}

func NewPgvectorStore() *PgvectorStore { return &PgvectorStore{} }

func (*PgvectorStore) Kind() string { return StorePgvector }

func (*PgvectorStore) Put(ctx context.Context, tx pgx.Tx, v Vector) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO memory_vectors (memory_id, model, project_id, memory_version, embedding)
		 VALUES ($1, $2, $3, $4, $5::vector)
		 ON CONFLICT (memory_id, model) DO UPDATE SET memory_version = EXCLUDED.memory_version,
		   embedding = EXCLUDED.embedding, updated_at = now()
		 WHERE memory_vectors.memory_version <= EXCLUDED.memory_version`,
		v.MemoryID, v.Model, v.ProjectID, v.MemoryVersion, pgvectorText(v.Values))
	if isMissingMemory(err) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM memory_vectors WHERE memory_id = $1 AND model <> $2`, v.MemoryID, v.Model)
	return err
}

func (*PgvectorStore) Get(ctx context.Context, tx pgx.Tx, projectID, memoryID uuid.UUID, model string) (Vector, bool, error) {
	v := Vector{ProjectID: projectID, MemoryID: memoryID, Model: model}
	var text string
	err := tx.QueryRow(ctx, `SELECT memory_version, embedding::text FROM memory_vectors WHERE project_id = $1 AND memory_id = $2 AND model = $3`,
		projectID, memoryID, model).Scan(&v.MemoryVersion, &text)
	if errors.Is(err, pgx.ErrNoRows) {
		return Vector{}, false, nil
	}
	if err != nil {
		return Vector{}, false, err
	}
	if v.Values, err = parsePgvector(text); err != nil {
		return Vector{}, false, err
	}
	return v, true, nil
}

// Score computes cosine similarity in SQL with pgvector's <=> operator.
// Exact, not approximate: the candidates are at most a search's scan bound,
// and an approximate index would need a fixed dimension per column, which a
// configurable model cannot promise (docs/adr/0028).
func (*PgvectorStore) Score(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, model string, q []float32, refs []Ref) ([]Scored, error) {
	ids := make([]uuid.UUID, len(refs))
	for i, r := range refs {
		ids[i] = r.ID
	}
	rows, err := tx.Query(ctx,
		`SELECT memory_id, 1 - (embedding <=> $3::vector) FROM memory_vectors
		 WHERE project_id = $1 AND model = $2 AND memory_id = ANY($4) AND vector_dims(embedding) = $5`,
		projectID, model, pgvectorText(q), ids, len(q))
	if err != nil {
		return nil, err
	}
	scored, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Scored, error) {
		var s Scored
		err := r.Scan(&s.ID, &s.Similarity)
		return s, err
	})
	if err != nil {
		return nil, err
	}
	// A zero vector has no direction and <=> answers NaN for it.
	out := scored[:0]
	for _, s := range scored {
		if !math.IsNaN(s.Similarity) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (*PgvectorStore) Pending(ctx context.Context, tx pgx.Tx, model string, after uuid.UUID, limit int, all bool) ([]Job, error) {
	return pending(ctx, tx, "cc_memory_vectors_pending", model, after, limit, all)
}

func (*PgvectorStore) PendingCount(ctx context.Context, tx pgx.Tx, model string) (int64, error) {
	var n int64
	err := tx.QueryRow(ctx, `SELECT cc_memory_vectors_pending_count($1)`, model).Scan(&n)
	return n, err
}
