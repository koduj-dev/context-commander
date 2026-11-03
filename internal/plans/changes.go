package plans

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
)

// Push (docs/adr/0022, "Push instead of polling"). A screen watching a board
// holds a stream; when something on that board changes, the stream is told
// "changed" and nothing else, and the screen re-reads the board through the
// ordinary authorized, decrypting read. The notification carries no content,
// so it needs no encryption and cannot show anyone more than the read would.
//
// The fan-out is in process. One ccd per installation is the supported
// deployment (docs/testing.md), and every write to a board goes through this
// Service, so the process that commits a change is the process holding the
// streams. A second process would need Postgres LISTEN/NOTIFY here instead;
// nothing outside this file would change.

var (
	// ErrTooManyStreams is a credential that already holds
	// StreamsPerCredential open streams.
	ErrTooManyStreams = errors.New("plans: too many open streams for this credential")
	// ErrShuttingDown is a stream asked for while the server is stopping.
	ErrShuttingDown = errors.New("plans: the server is shutting down")
)

// StreamsPerCredential caps how many streams one credential may hold open.
// A browser tab holds one while visible; the cap is what keeps a runaway
// script, or a leaked session, from pinning a goroutine per request forever.
const StreamsPerCredential = 4

// changes fans "this project changed" out to the open subscriptions.
type changes struct {
	mu      sync.Mutex
	subs    map[*Subscription]struct{}
	perCred map[uuid.UUID]int
	closed  bool
	done    chan struct{}
}

func newChanges() *changes {
	return &changes{subs: map[*Subscription]struct{}{}, perCred: map[uuid.UUID]int{}, done: make(chan struct{})}
}

// Subscription is one open stream's view of the changes it asked for.
type Subscription struct {
	owner *changes
	cred  uuid.UUID
	// projects is what this stream is told about: one project for a board,
	// every project the reader is in for the dashboard.
	projects map[uuid.UUID]bool
	// presenceOnly streams are told only about changes to where people are,
	// which is all the dashboard shows.
	presenceOnly bool
	c            chan struct{}
	once         sync.Once
}

// C receives once per change, coalesced: a burst of ticks while the reader
// is still re-reading is one more read, not one per tick.
func (s *Subscription) C() <-chan struct{} { return s.c }

// Done is closed when the server is shutting down and every stream must end.
func (s *Subscription) Done() <-chan struct{} { return s.owner.done }

// Close releases the subscription and its place under the credential's cap.
// Safe to call more than once.
func (s *Subscription) Close() {
	s.once.Do(func() {
		c := s.owner
		c.mu.Lock()
		defer c.mu.Unlock()
		if _, ok := c.subs[s]; !ok {
			return
		}
		delete(c.subs, s)
		if c.perCred[s.cred]--; c.perCred[s.cred] <= 0 {
			delete(c.perCred, s.cred)
		}
	})
}

func (c *changes) subscribe(cred uuid.UUID, projects []uuid.UUID, presenceOnly bool) (*Subscription, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrShuttingDown
	}
	if c.perCred[cred] >= StreamsPerCredential {
		return nil, ErrTooManyStreams
	}
	s := &Subscription{owner: c, cred: cred, projects: map[uuid.UUID]bool{}, presenceOnly: presenceOnly, c: make(chan struct{}, 1)}
	for _, p := range projects {
		s.projects[p] = true
	}
	c.subs[s] = struct{}{}
	c.perCred[cred]++
	return s, nil
}

// publish tells every interested stream that a project changed. presence
// says whether where-people-are changed too. Never blocks: a stream that has
// not taken its previous notification already has one pending.
func (c *changes) publish(projectID uuid.UUID, presence bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for s := range c.subs {
		if !s.projects[projectID] || (s.presenceOnly && !presence) {
			continue
		}
		select {
		case s.c <- struct{}{}:
		default:
		}
	}
}

// Close ends every stream and refuses new ones. For server shutdown.
func (c *changes) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.done)
	}
}

// CloseStreams ends every open stream and refuses new ones. Called when the
// server shuts down: http.Server.Shutdown waits for handlers to return, and a
// stream would otherwise hold it until its deadline.
func (s *Service) CloseStreams() { s.changes.close() }

// SubscribeBoard opens a stream of "the board changed" for one project. The
// caller must be able to read the board; the stream is checked once, here,
// and the adapter bounds how long it may stay open, so a reader removed from
// the project stops hearing about it within that bound.
func (s *Service) SubscribeBoard(ctx context.Context, principalID, credentialID, projectID uuid.UUID) (*Subscription, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return nil, err
	}
	return s.changes.subscribe(credentialID, []uuid.UUID{projectID}, false)
}

// SubscribePresence opens a stream of "where people are changed" across
// every project the caller can see presence in — the same projects
// MyPresence reads, by the same RLS predicate.
func (s *Service) SubscribePresence(ctx context.Context, principalID, credentialID uuid.UUID) (*Subscription, error) {
	var ids []uuid.UUID
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id FROM projects WHERE cc_has_project_access(current_setting('app.current_principal_id', true), id)`)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("plans: subscribe presence: %w", err)
	}
	return s.changes.subscribe(credentialID, ids, true)
}
