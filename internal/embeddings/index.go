package embeddings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/memories"
)

// Options tune an Index. The zero value is the defaults.
type Options struct {
	// QueueSize bounds the jobs waiting to be embedded. A write whose job
	// does not fit is not lost: the sweep finds the memory later.
	QueueSize int
	// MinSimilarity is the cosine similarity below which a semantic hit is
	// left out. Model-dependent: bge-m3 rates unrelated text around 0.3–0.4
	// and related text above 0.5.
	MinSimilarity float64
	// SweepInterval is how often the index looks for memories with a
	// missing or stale vector. Zero means DefaultSweepInterval; negative
	// turns the periodic sweep off (it still runs once at start).
	SweepInterval time.Duration
	// MaxAttempts is how many times one batch is tried before it is left to
	// the next sweep.
	MaxAttempts int
	// Backoff is the first retry's delay; it doubles up to MaxBackoff.
	Backoff    time.Duration
	MaxBackoff time.Duration
}

const (
	DefaultQueueSize     = 1000
	DefaultMinSimilarity = 0.45
	DefaultSweepInterval = 10 * time.Minute
	defaultMaxAttempts   = 5
	defaultBackoff       = time.Second
	defaultMaxBackoff    = 2 * time.Minute
	// sweepPage is how many pending memories one sweep query returns.
	sweepPage = 200
)

func (o Options) withDefaults() Options {
	if o.QueueSize <= 0 {
		o.QueueSize = DefaultQueueSize
	}
	if o.MinSimilarity == 0 {
		o.MinSimilarity = DefaultMinSimilarity
	}
	if o.SweepInterval == 0 {
		o.SweepInterval = DefaultSweepInterval
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = defaultMaxAttempts
	}
	if o.Backoff <= 0 {
		o.Backoff = defaultBackoff
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = defaultMaxBackoff
	}
	return o
}

// Index is semantic search for memories: it implements memories.Semantic.
// It embeds memories in the background after they are written, sweeps for
// memories whose vector is missing or stale, and ranks search candidates by
// similarity.
//
// It reads memories through memories.Service, so every read is authorized
// and under RLS like a request, as the principal a Job names.
type Index struct {
	provider Provider
	store    Store
	pool     *db.Pool
	memories *memories.Service
	opts     Options

	queue chan Job
	// queued de-duplicates: a memory edited three times while the provider
	// was down is embedded once, and the sweep does not stack a second job
	// on one already waiting.
	mu       sync.Mutex
	queued   map[uuid.UUID]bool
	inFlight atomic.Int64
	dropped  atomic.Int64
	indexed  atomic.Int64

	health struct {
		sync.Mutex
		lastOK    time.Time
		lastErr   string
		lastErrAt time.Time
	}
}

// NewIndex wires a provider and a store to the memories service. It does
// not start anything: Run does, and EnableSemantic on the service is the
// caller's to do once Run is going.
func NewIndex(p Provider, s Store, pool *db.Pool, m *memories.Service, o Options) *Index {
	o = o.withDefaults()
	return &Index{provider: p, store: s, pool: pool, memories: m, opts: o,
		queue: make(chan Job, o.QueueSize), queued: map[uuid.UUID]bool{}}
}

func (ix *Index) Provider() Provider { return ix.provider }
func (ix *Index) Store() Store       { return ix.store }

// Enqueue asks for a memory to be embedded, as principalID. It never
// blocks: a full queue drops the job and counts it, and the sweep picks the
// memory up later. Part of memories.Semantic.
func (ix *Index) Enqueue(projectID, memoryID, principalID uuid.UUID) {
	ix.offer(Job{ProjectID: projectID, MemoryID: memoryID, PrincipalID: principalID})
}

func (ix *Index) offer(j Job) bool {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.queued[j.MemoryID] {
		return true
	}
	select {
	case ix.queue <- j:
		ix.queued[j.MemoryID] = true
		return true
	default:
		ix.dropped.Add(1)
		return false
	}
}

func (ix *Index) done(j Job) {
	ix.mu.Lock()
	delete(ix.queued, j.MemoryID)
	ix.mu.Unlock()
}

// Run embeds queued memories until ctx ends. It sweeps once at start, so a
// restart catches up on whatever was written while the server was down or
// the queue was full, and then every SweepInterval.
func (ix *Index) Run(ctx context.Context) {
	go ix.sweepLoop(ctx)
	for {
		var batch []Job
		select {
		case <-ctx.Done():
			return
		case j := <-ix.queue:
			batch = append(batch, j)
		}
		// Whatever else is already waiting rides along, up to one request.
	fill:
		for len(batch) < MaxBatch {
			select {
			case j := <-ix.queue:
				batch = append(batch, j)
			default:
				break fill
			}
		}
		ix.inFlight.Store(int64(len(batch)))
		for _, j := range batch {
			ix.done(j)
		}
		ix.processWithRetry(ctx, batch)
		ix.inFlight.Store(0)
	}
}

func (ix *Index) sweepLoop(ctx context.Context) {
	ix.sweep(ctx)
	if ix.opts.SweepInterval < 0 {
		return
	}
	t := time.NewTicker(ix.opts.SweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ix.sweep(ctx)
		}
	}
}

// sweep queues every memory whose vector is missing or stale, as far as the
// queue has room; the rest wait for the next sweep.
func (ix *Index) sweep(ctx context.Context) {
	after := uuid.Nil
	for {
		jobs, err := ix.pending(ctx, after, sweepPage, false)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("embeddings: sweep for memories to embed failed", "error", err)
			}
			return
		}
		for _, j := range jobs {
			if j.PrincipalID == uuid.Nil {
				continue
			}
			if !ix.offer(j) {
				return
			}
		}
		if len(jobs) < sweepPage {
			return
		}
		after = jobs[len(jobs)-1].MemoryID
	}
}

func (ix *Index) pending(ctx context.Context, after uuid.UUID, limit int, all bool) ([]Job, error) {
	var jobs []Job
	err := db.WithoutPrincipal(ctx, ix.pool, func(tx pgx.Tx) error {
		var err error
		jobs, err = ix.store.Pending(ctx, tx, ix.provider.Model(), after, limit, all)
		return err
	})
	return jobs, err
}

// processWithRetry embeds one batch, backing off and retrying while the
// provider fails. After MaxAttempts it gives up on the batch: the memories
// stay pending in the database and the next sweep retries them.
func (ix *Index) processWithRetry(ctx context.Context, batch []Job) {
	delay := ix.opts.Backoff
	for attempt := 1; ; attempt++ {
		err := ix.process(ctx, batch)
		if err == nil || ctx.Err() != nil {
			return
		}
		if attempt >= ix.opts.MaxAttempts {
			slog.Warn("embeddings: giving up on a batch until the next sweep", "memories", len(batch), "attempts", attempt, "error", err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(2*delay, ix.opts.MaxBackoff)
	}
}

// process embeds a batch and stores the vectors. A memory deleted or no
// longer readable by the job's principal is skipped, not retried; only a
// provider or database failure fails the batch.
func (ix *Index) process(ctx context.Context, batch []Job) error {
	var mems []memories.Memory
	var jobs []Job
	for _, j := range batch {
		m, err := ix.memories.Get(ctx, j.PrincipalID, j.ProjectID, j.MemoryID)
		if errors.Is(err, memories.ErrNotFound) || errors.Is(err, memories.ErrForbidden) {
			continue
		}
		if err != nil {
			return err
		}
		mems = append(mems, m)
		jobs = append(jobs, j)
	}
	if len(mems) == 0 {
		return nil
	}
	texts := make([]string, len(mems))
	for i, m := range mems {
		texts[i] = Text(m)
	}
	vectors, err := ix.embed(ctx, texts)
	if err != nil {
		return err
	}
	for i, m := range mems {
		v := Vector{ProjectID: m.ProjectID, MemoryID: m.ID, MemoryVersion: m.Version, Model: ix.provider.Model(), Values: vectors[i]}
		err := db.WithPrincipal(ctx, ix.pool, db.Principal{ID: jobs[i].PrincipalID}, func(tx pgx.Tx) error {
			return ix.store.Put(ctx, tx, v)
		})
		if err != nil {
			return fmt.Errorf("embeddings: store: %w", err)
		}
		ix.indexed.Add(1)
	}
	return nil
}

// Text is what is embedded for a memory: its title, which says what it is
// about in one line, then its body.
func Text(m memories.Memory) string {
	if m.Body == "" {
		return m.Title
	}
	return m.Title + "\n\n" + m.Body
}

// embed calls the provider and records how it went, for Status.
func (ix *Index) embed(ctx context.Context, texts []string) ([][]float32, error) {
	vectors, err := ix.provider.Embed(ctx, texts)
	ix.health.Lock()
	defer ix.health.Unlock()
	if err != nil {
		if ctx.Err() == nil {
			ix.health.lastErr, ix.health.lastErrAt = err.Error(), time.Now()
		}
		return nil, err
	}
	ix.health.lastOK = time.Now()
	return vectors, nil
}

// Probe embeds one short text, to learn the dimensions and whether the
// provider answers. serve calls it at startup and logs the outcome; a
// provider that is down does not stop the server.
func (ix *Index) Probe(ctx context.Context) error {
	_, err := ix.embed(ctx, []string{"ping"})
	return err
}

// ---------------------------------------------------------------- ranking

// Rank orders candidates by similarity of their stored vectors to the
// query's, most similar first, leaving out those below MinSimilarity and
// those not embedded yet. Vectors are read per project under the caller's
// RLS context, and only for the candidates the caller's own read returned.
// Part of memories.Semantic (memories.Ranker).
func (ix *Index) Rank(ctx context.Context, principalID uuid.UUID, query string, candidates []memories.Memory) ([]memories.Hit, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	vectors, err := ix.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	return ix.rankBy(ctx, principalID, vectors[0], candidates, 0)
}

// Similar is the candidates most similar to one memory, by its stored
// vector, or by embedding its text now when it has none at its current
// version yet. Part of memories.Semantic.
func (ix *Index) Similar(ctx context.Context, principalID uuid.UUID, to memories.Memory, candidates []memories.Memory, k int) ([]memories.Hit, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	var q []float32
	err := db.WithPrincipal(ctx, ix.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		v, ok, err := ix.store.Get(ctx, tx, to.ProjectID, to.ID, ix.provider.Model())
		if ok && v.MemoryVersion == to.Version {
			q = v.Values
		}
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("embeddings: read vector: %w", err)
	}
	if q == nil {
		vectors, err := ix.embed(ctx, []string{Text(to)})
		if err != nil {
			return nil, err
		}
		q = vectors[0]
	}
	return ix.rankBy(ctx, principalID, q, candidates, k)
}

func (ix *Index) rankBy(ctx context.Context, principalID uuid.UUID, q []float32, candidates []memories.Memory, k int) ([]memories.Hit, error) {
	byProject := map[uuid.UUID][]Ref{}
	byID := make(map[uuid.UUID]memories.Memory, len(candidates))
	var projects []uuid.UUID
	for _, m := range candidates {
		if _, ok := byProject[m.ProjectID]; !ok {
			projects = append(projects, m.ProjectID)
		}
		byProject[m.ProjectID] = append(byProject[m.ProjectID], Ref{ID: m.ID, Version: m.Version})
		byID[m.ID] = m
	}
	var scored []Scored
	err := db.WithPrincipal(ctx, ix.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		for _, p := range projects {
			s, err := ix.store.Score(ctx, tx, p, ix.provider.Model(), q, byProject[p])
			if err != nil {
				return err
			}
			scored = append(scored, s...)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("embeddings: score: %w", err)
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Similarity > scored[j].Similarity })
	hits := make([]memories.Hit, 0, len(scored))
	for _, s := range scored {
		if s.Similarity < ix.opts.MinSimilarity {
			break
		}
		m, ok := byID[s.ID]
		if !ok {
			continue
		}
		hits = append(hits, memories.Hit{Memory: m, Match: memories.MatchSemantic, Snippet: memories.Snippet(m.Body, ""), Score: s.Similarity})
		if k > 0 && len(hits) == k {
			break
		}
	}
	return hits, nil
}

// ---------------------------------------------------------------- reindex

// ReindexResult is what a reindex did.
type ReindexResult struct {
	Embedded int
	// Skipped memories had no principal to read them as (a project with no
	// members left) or were deleted meanwhile.
	Skipped int
}

// Reindex embeds every pending memory now, in the caller's goroutine,
// instead of through the queue: `ccd embeddings reindex` backfills an
// installation that just turned semantic search on or changed its model.
// all re-embeds every memory, not only missing and stale ones. progress, if
// not nil, is called after each batch.
func (ix *Index) Reindex(ctx context.Context, all bool, progress func(ReindexResult)) (ReindexResult, error) {
	var res ReindexResult
	after := uuid.Nil
	for {
		jobs, err := ix.pending(ctx, after, sweepPage, all)
		if err != nil {
			return res, fmt.Errorf("embeddings: list pending: %w", err)
		}
		var ready []Job
		for _, j := range jobs {
			if j.PrincipalID == uuid.Nil {
				res.Skipped++
				continue
			}
			ready = append(ready, j)
		}
		for start := 0; start < len(ready); start += MaxBatch {
			batch := ready[start:min(start+MaxBatch, len(ready))]
			before := ix.indexed.Load()
			if err := ix.processReindexBatch(ctx, batch); err != nil {
				return res, err
			}
			n := int(ix.indexed.Load() - before)
			res.Embedded += n
			res.Skipped += len(batch) - n
			if progress != nil {
				progress(res)
			}
		}
		if len(jobs) < sweepPage {
			return res, nil
		}
		after = jobs[len(jobs)-1].MemoryID
	}
}

// processReindexBatch retries like the worker, but reports the final error:
// an operator running the command should see why it stopped.
func (ix *Index) processReindexBatch(ctx context.Context, batch []Job) error {
	delay := ix.opts.Backoff
	for attempt := 1; ; attempt++ {
		err := ix.process(ctx, batch)
		if err == nil {
			return nil
		}
		if attempt >= ix.opts.MaxAttempts || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay = min(2*delay, ix.opts.MaxBackoff)
	}
}

// ---------------------------------------------------------------- status

// Status is what an administrator sees about semantic search: configuration
// and health, counts, and the last error. Never content: the last error is
// the provider error, which by construction carries no text or response
// body (ErrProvider).
type Status struct {
	Provider      string
	Model         string
	Endpoint      string
	Store         string
	Dimensions    int
	Reachable     *bool
	LastSuccess   *time.Time
	LastError     string
	LastErrorAt   *time.Time
	Queue         int
	QueueCapacity int
	Dropped       int64
	// Pending is memories with no vector under the model, or a stale one,
	// installation-wide. -1 when the count could not be read.
	Pending int64
	// CacheBytes and CacheLimit are the decrypted-vector cache of the
	// encrypted store; zero for pgvector.
	CacheBytes int64
	CacheLimit int64
}

func (ix *Index) Status(ctx context.Context) Status {
	st := Status{
		Provider: ix.provider.Name(), Model: ix.provider.Model(), Endpoint: ix.provider.Endpoint(),
		Store: ix.store.Kind(), Dimensions: ix.provider.Dimensions(),
		Queue: len(ix.queue) + int(ix.inFlight.Load()), QueueCapacity: cap(ix.queue), Dropped: ix.dropped.Load(),
		Pending: -1,
	}
	ix.health.Lock()
	ok, bad := ix.health.lastOK, ix.health.lastErrAt
	if !ok.IsZero() {
		st.LastSuccess = &ok
	}
	if !bad.IsZero() {
		st.LastError, st.LastErrorAt = ix.health.lastErr, &bad
	}
	ix.health.Unlock()
	// Reachable is the latest outcome; unknown until the first call.
	if !ok.IsZero() || !bad.IsZero() {
		r := ok.After(bad)
		st.Reachable = &r
	}
	if es, isEnc := ix.store.(*EncryptedStore); isEnc {
		st.CacheBytes, st.CacheLimit = es.CacheBytes()
	}
	_ = db.WithoutPrincipal(ctx, ix.pool, func(tx pgx.Tx) error {
		n, err := ix.store.PendingCount(ctx, tx, ix.provider.Model())
		if err == nil {
			st.Pending = n
		}
		return err
	})
	return st
}
