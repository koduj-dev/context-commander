package ideas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/plans"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/quota"
)

// Service is the authorization chokepoint for the idea stack
// (docs/adr/0014): every method checks CapIdeaRead or CapIdeaWrite before
// touching data, and Accept and Reject additionally require a person.
type Service struct {
	pool   *db.Pool
	crypto *crypto.Provider
	authz  *auth.AuthzService
	// plans creates a proposal's draft plan and moves it on a decision, in
	// the idea's own transaction; it also carries the board stream the Ideas
	// screen listens to.
	plans *plans.Service
	// now is replaceable so a test can let a lease lapse without sleeping
	// two hours.
	now func() time.Time
}

func NewService(pool *db.Pool, cryptoProvider *crypto.Provider, authz *auth.AuthzService, plansSvc *plans.Service) *Service {
	return &Service{pool: pool, crypto: cryptoProvider, authz: authz, plans: plansSvc, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the service's clock. For tests.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Now is the service's clock, so an adapter computes "lapsed" against the
// same time the service decided by.
func (s *Service) Now() time.Time { return s.now() }

func (s *Service) checkCapability(ctx context.Context, principalID, projectID uuid.UUID, cap auth.Capability) error {
	allowed, err := s.authz.Can(ctx, principalID, projectID, cap)
	if err != nil {
		return fmt.Errorf("ideas: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func checkLen(field, v string, max int) error {
	if utf8.RuneCountInString(v) > max {
		return invalid("%s is longer than %d characters", field, max)
	}
	return nil
}

// checkTicketURL accepts an absolute http(s) URL or nothing. Anything else
// would end up as a link in the interface, and a javascript: link there is
// script running in a colleague's session.
func checkTicketURL(v string) error {
	if v == "" {
		return nil
	}
	if err := checkLen("ticket_url", v, MaxTicketURL); err != nil {
		return err
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return invalid("ticket_url must be an absolute http or https URL")
	}
	return nil
}

func nullIfEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// ---------------------------------------------------------------- storage

// ideaBlob is everything an idea says. One blob, so reading an idea is one
// decrypt and the queue's metadata stays plaintext beside it.
type ideaBlob struct {
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Proposal  string `json:"proposal,omitempty"`
	Rejection string `json:"rejection,omitempty"`
}

type envelope struct {
	ciphertext, nonce, wrappedDEK []byte
	keyVersion                    int
}

func encryptionContext(projectID, id uuid.UUID, version int) crypto.EncryptionContext {
	return crypto.EncryptionContext{
		ProjectID: projectID, ResourceType: "idea",
		ResourceID: id, ResourceVersion: uint64(version), //nolint:gosec // a small positive counter
	}
}

func (s *Service) seal(ctx context.Context, tx pgx.Tx, projectID, id uuid.UUID, version int, b ideaBlob) (envelope, error) {
	plaintext, err := json.Marshal(b)
	if err != nil {
		return envelope{}, fmt.Errorf("ideas: marshal: %w", err)
	}
	var e envelope
	e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion, err = s.crypto.Encrypt(ctx, tx, encryptionContext(projectID, id, version), plaintext)
	if err != nil {
		return envelope{}, fmt.Errorf("ideas: encrypt: %w", err)
	}
	return e, nil
}

func (s *Service) open(ctx context.Context, tx pgx.Tx, i Idea, e envelope) (ideaBlob, error) {
	plaintext, err := s.crypto.Decrypt(ctx, tx, encryptionContext(i.ProjectID, i.ID, i.Version), e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion)
	if err != nil {
		return ideaBlob{}, fmt.Errorf("ideas: decrypt: %w", err)
	}
	var b ideaBlob
	return b, json.Unmarshal(plaintext, &b)
}

const columns = `id, project_id, status, priority, position, version, ciphertext, nonce, wrapped_dek, project_key_version,
	COALESCE(branch, ''), COALESCE(ticket_key, ''), COALESCE(ticket_url, ''),
	created_by_principal, created_by_client, claimed_by_principal, claimed_by_client, claimed_at, claim_expires_at,
	proposed_at, decided_by_principal, decided_at, resulting_plan_id, created_at, updated_at`

// scan reads columns off one row. The blob stays sealed; the caller opens it
// when it needs the content.
func scan(row pgx.Row, i *Idea, e *envelope) error {
	var prio int16
	err := row.Scan(&i.ID, &i.ProjectID, &i.Status, &prio, &i.Position, &i.Version, &e.ciphertext, &e.nonce, &e.wrappedDEK, &e.keyVersion,
		&i.Branch, &i.TicketKey, &i.TicketURL,
		&i.CreatedBy, &i.CreatedByClient, &i.ClaimedBy, &i.ClaimedByClient, &i.ClaimedAt, &i.ClaimExpiresAt,
		&i.ProposedAt, &i.DecidedBy, &i.DecidedAt, &i.ResultingPlanID, &i.CreatedAt, &i.UpdatedAt)
	i.Priority = priorityName(int(prio))
	return err
}

func withContent(i Idea, b ideaBlob) Idea {
	i.Title, i.Body, i.Proposal, i.Rejection = b.Title, b.Body, b.Proposal, b.Rejection
	return i
}

// load reads one idea with its content. forUpdate locks the row, which is
// what serializes every transition on it: two clients claiming the same
// idea queue on the lock and the second sees the first one's claim.
func (s *Service) load(ctx context.Context, tx pgx.Tx, projectID, id uuid.UUID, forUpdate bool) (Idea, ideaBlob, error) {
	q := `SELECT ` + columns + ` FROM ideas WHERE id = $1 AND project_id = $2`
	if forUpdate {
		q += ` FOR UPDATE`
	}
	var i Idea
	var e envelope
	if err := scan(tx.QueryRow(ctx, q, id, projectID), &i, &e); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Idea{}, ideaBlob{}, ErrNotFound
		}
		return Idea{}, ideaBlob{}, err
	}
	b, err := s.open(ctx, tx, i, e)
	if err != nil {
		return Idea{}, ideaBlob{}, err
	}
	return withContent(i, b), b, nil
}

// store writes back every mutable column of i, and re-encrypts the content
// when b is given (bumping the version, which the AAD is bound to).
func (s *Service) store(ctx context.Context, tx pgx.Tx, i *Idea, b *ideaBlob) error {
	var e envelope
	if b != nil {
		i.Version++
		var err error
		if e, err = s.seal(ctx, tx, i.ProjectID, i.ID, i.Version, *b); err != nil {
			return err
		}
		*i = withContent(*i, *b)
	}
	prio, _ := priorityValue(i.Priority)
	return tx.QueryRow(ctx,
		`UPDATE ideas SET status = $2, priority = $3, position = $4,
		        claimed_by_principal = $5, claimed_by_client = $6, claimed_at = $7, claim_expires_at = $8,
		        proposed_at = $9, decided_by_principal = $10, decided_at = $11, resulting_plan_id = $12, version = $13,
		        ciphertext = CASE WHEN $14::bool THEN $15::bytea ELSE ciphertext END,
		        nonce = CASE WHEN $14::bool THEN $16::bytea ELSE nonce END,
		        wrapped_dek = CASE WHEN $14::bool THEN $17::bytea ELSE wrapped_dek END,
		        project_key_version = CASE WHEN $14::bool THEN $18::int ELSE project_key_version END,
		        updated_at = now()
		 WHERE id = $1 RETURNING updated_at`,
		i.ID, i.Status, prio, i.Position,
		i.ClaimedBy, i.ClaimedByClient, i.ClaimedAt, i.ClaimExpiresAt,
		i.ProposedAt, i.DecidedBy, i.DecidedAt, i.ResultingPlanID, i.Version,
		b != nil, e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion,
	).Scan(&i.UpdatedAt)
}

func (s *Service) audit(ctx context.Context, tx pgx.Tx, i Idea, principalID uuid.UUID, clientID *uuid.UUID, action string) error {
	return audit.Write(ctx, tx, audit.Entry{
		ProjectID: &i.ProjectID, PrincipalID: &principalID, ClientID: clientID,
		Action: action, ResourceType: "idea", ResourceID: &i.ID, Result: audit.ResultAllow,
	})
}

// lockOrder serializes everything that assigns an idea position in one
// project, so two ideas added at once do not both take max+1.
func lockOrder(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) error {
	return db.AdvisoryXactLock(ctx, tx, db.LockClassIdeaOrder, projectID.String())
}

func nextPosition(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) (int, error) {
	var pos int
	err := tx.QueryRow(ctx, `SELECT COALESCE(max(position), 0) + 1 FROM ideas WHERE project_id = $1`, projectID).Scan(&pos)
	return pos, err
}

// requirePerson refuses a decision that does not come from a person signed
// in to the interface: a user principal on a browser-session credential.
// Sessions are issued only by an interactive sign-in (docs/adr/0019) and
// are never handed to an agent, while a PAT is exactly what an agent holds,
// whoever it belongs to — so the credential, not the role, is what tells a
// person's click from an agent's call (docs/adr/0026).
func requirePerson(ctx context.Context, tx pgx.Tx, principalID uuid.UUID, clientID *uuid.UUID) error {
	if clientID == nil {
		return ErrNeedsPerson
	}
	var kind, clientType string
	err := tx.QueryRow(ctx,
		`SELECT p.kind, c.client_type FROM client_credentials c JOIN principals p ON p.id = c.principal_id
		 WHERE c.id = $1 AND c.principal_id = $2`, *clientID, principalID,
	).Scan(&kind, &clientType)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNeedsPerson
	}
	if err != nil {
		return fmt.Errorf("ideas: read credential: %w", err)
	}
	if kind != string(identity.PrincipalKindUser) || clientType != identity.SessionClientType {
		return ErrNeedsPerson
	}
	return nil
}

func wrongStatus(i Idea, what string) error {
	return fmt.Errorf("%w: cannot %s an idea that is %s", ErrInvalidTransition, what, i.Status)
}

// ---------------------------------------------------------------- create and read

func (s *Service) Create(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID uuid.UUID, n NewIdea) (Idea, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaWrite); err != nil {
		return Idea{}, err
	}
	n.Title, n.Branch, n.TicketKey, n.TicketURL = strings.TrimSpace(n.Title), strings.TrimSpace(n.Branch), strings.TrimSpace(n.TicketKey), strings.TrimSpace(n.TicketURL)
	if n.Title == "" {
		return Idea{}, invalid("an idea needs a title")
	}
	if _, ok := priorityValue(n.Priority); !ok {
		return Idea{}, invalid("priority %q is not low, normal or high", n.Priority)
	}
	if err := errors.Join(
		checkLen("title", n.Title, MaxTitle), checkLen("body", n.Body, MaxBody),
		checkLen("branch", n.Branch, MaxBranch), checkLen("ticket_key", n.TicketKey, MaxTicketKey), checkTicketURL(n.TicketURL),
	); err != nil {
		return Idea{}, err
	}
	if n.Priority == "" {
		n.Priority = PriorityNormal
	}

	i := Idea{
		ID: uuid.New(), ProjectID: projectID, Status: StatusOpen, Priority: n.Priority, Version: 1,
		Title: n.Title, Body: n.Body, Branch: n.Branch, TicketKey: n.TicketKey, TicketURL: n.TicketURL,
		CreatedBy: principalID, CreatedByClient: clientID,
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := lockOrder(ctx, tx, projectID); err != nil {
			return err
		}
		var err error
		if i.Position, err = nextPosition(ctx, tx, projectID); err != nil {
			return err
		}
		e, err := s.seal(ctx, tx, projectID, i.ID, i.Version, ideaBlob{Title: i.Title, Body: i.Body})
		if err != nil {
			return err
		}
		prio, _ := priorityValue(i.Priority)
		if err := tx.QueryRow(ctx,
			`INSERT INTO ideas (id, project_id, status, priority, position, version, ciphertext, nonce, wrapped_dek, project_key_version,
			                    branch, ticket_key, ticket_url, created_by_principal, created_by_client)
			 VALUES ($1, $2, 'open', $3, $4, 1, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING created_at, updated_at`,
			i.ID, projectID, prio, i.Position, e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion,
			nullIfEmpty(i.Branch), nullIfEmpty(i.TicketKey), nullIfEmpty(i.TicketURL), principalID, clientID,
		).Scan(&i.CreatedAt, &i.UpdatedAt); err != nil {
			return err
		}
		return s.audit(ctx, tx, i, principalID, clientID, "idea.create")
	})
	if err != nil {
		return Idea{}, fmt.Errorf("ideas: create: %w", quota.Translate(err))
	}
	s.plans.NotifyChanged(projectID)
	return i, nil
}

func (s *Service) Get(ctx context.Context, principalID, projectID, ideaID uuid.UUID) (Idea, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaRead); err != nil {
		return Idea{}, err
	}
	var i Idea
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		i, _, err = s.load(ctx, tx, projectID, ideaID, false)
		return err
	})
	if err != nil {
		return Idea{}, fmt.Errorf("ideas: get: %w", err)
	}
	return i, nil
}

// queueOrder reports whether a list over these statuses is read in queue
// order — priority, then manual position, then age — rather than most
// recently changed first. Only the statuses still waiting for an agent have
// a queue; for decided ideas, what changed last is what a reader wants.
func queueOrder(statuses []string) bool {
	if len(statuses) == 0 {
		return false
	}
	for _, st := range statuses {
		if st != StatusOpen && st != StatusClaimed {
			return false
		}
	}
	return true
}

// List returns one page of ideas in the given statuses (all when empty).
// Open and claimed ideas page in queue order with a rank cursor; any other
// selection pages newest-changed first with a time cursor, and a cursor from
// one ordering is refused by the other.
//
// Each idea carries its title and metadata. Body, proposal and rejection
// stay out of the list — Get has them — so a page of fifty ideas is not
// fifty proposals.
func (s *Service) List(ctx context.Context, principalID, projectID uuid.UUID, statuses []string, opts paging.Options) (paging.Page[Idea], error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaRead); err != nil {
		return paging.Page[Idea]{}, err
	}
	for _, st := range statuses {
		if !validStatus(st) {
			return paging.Page[Idea]{}, invalid("status %q is not one of %s", st, strings.Join(Statuses, ", "))
		}
	}
	limit := opts.ClampedLimit()
	queue := queueOrder(statuses)
	var (
		where   string
		orderBy string
		args    = []any{projectID, statuses, limit + 1}
	)
	if queue {
		prio, pos, t, id, has, err := opts.DecodeRank()
		if err != nil {
			return paging.Page[Idea]{}, err
		}
		orderBy = `priority DESC, position, created_at, id`
		where = `NOT $4::bool OR priority < $5 OR (priority = $5 AND (position, created_at, id) > ($6::int, $7::timestamptz, $8::uuid))`
		args = append(args, has, prio, pos, t, id)
	} else {
		t, id, has, err := opts.DecodeTimeUUID()
		if err != nil {
			return paging.Page[Idea]{}, err
		}
		orderBy = `updated_at DESC, id DESC`
		where = `NOT $4::bool OR (updated_at, id) < ($5::timestamptz, $6::uuid)`
		args = append(args, has, t, id)
	}

	out := make([]Idea, 0, limit)
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT `+columns+` FROM ideas
			 WHERE project_id = $1 AND (cardinality($2::text[]) = 0 OR status = ANY($2)) AND (`+where+`)
			 ORDER BY `+orderBy+` LIMIT $3`, args...)
		if err != nil {
			return err
		}
		type sealed struct {
			i Idea
			e envelope
		}
		var list []sealed
		for rows.Next() {
			var r sealed
			if err := scan(rows, &r.i, &r.e); err != nil {
				rows.Close()
				return err
			}
			list = append(list, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// Decrypted after the rows are drained: Decrypt reads the project
		// key on the same transaction, which cannot run a second query
		// while the first one is still streaming.
		for _, r := range list {
			b, err := s.open(ctx, tx, r.i, r.e)
			if err != nil {
				return err
			}
			out = append(out, withContent(r.i, ideaBlob{Title: b.Title}))
		}
		return nil
	})
	if err != nil {
		return paging.Page[Idea]{}, fmt.Errorf("ideas: list: %w", err)
	}

	items, more := paging.More(out, limit)
	page := paging.Page[Idea]{Items: items}
	if more {
		last := items[len(items)-1]
		if queue {
			prio, _ := priorityValue(last.Priority)
			page.NextCursor = paging.EncodeRank(prio, last.Position, last.CreatedAt, last.ID)
		} else {
			page.NextCursor = paging.EncodeTimeUUID(last.UpdatedAt, last.ID)
		}
	}
	return page, nil
}

// People names everyone the given ideas mention — who added, claimed and
// decided each — for a screen that shows names rather than ids. Names only,
// never content, and only for a caller who may read the ideas.
func (s *Service) People(ctx context.Context, principalID, projectID uuid.UUID, list []Idea) (map[uuid.UUID]plans.Person, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaRead); err != nil {
		return nil, err
	}
	ids := map[uuid.UUID]bool{}
	for _, i := range list {
		ids[i.CreatedBy] = true
		for _, p := range []*uuid.UUID{i.ClaimedBy, i.DecidedBy} {
			if p != nil {
				ids[*p] = true
			}
		}
	}
	var out map[uuid.UUID]plans.Person
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		out, err = plans.ResolvePeople(ctx, tx, ids)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("ideas: people: %w", err)
	}
	return out, nil
}

// Counts is how many ideas wait at each live stage, from metadata alone. It
// is what context.bootstrap carries: small, and nothing decrypted.
func (s *Service) Counts(ctx context.Context, principalID, projectID uuid.UUID) (Counts, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaRead); err != nil {
		return Counts{}, err
	}
	var c Counts
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FILTER (WHERE status = 'open'),
			        count(*) FILTER (WHERE status = 'claimed'),
			        count(*) FILTER (WHERE status = 'claimed' AND claim_expires_at <= $2),
			        count(*) FILTER (WHERE status = 'proposed')
			 FROM ideas WHERE project_id = $1 AND status IN ('open', 'claimed', 'proposed')`,
			projectID, s.now(),
		).Scan(&c.Open, &c.Claimed, &c.Lapsed, &c.Proposed)
	})
	if err != nil {
		return Counts{}, fmt.Errorf("ideas: counts: %w", err)
	}
	return c, nil
}

// ---------------------------------------------------------------- claims

// claim makes clientID the holder of i, with a fresh lease.
func (s *Service) claim(i *Idea, principalID uuid.UUID, clientID *uuid.UUID, now time.Time) {
	expires := now.Add(ClaimLease)
	i.Status = StatusClaimed
	i.ClaimedBy, i.ClaimedByClient, i.ClaimedAt, i.ClaimExpiresAt = &principalID, clientID, &now, &expires
}

func clearClaim(i *Idea) {
	i.ClaimedBy, i.ClaimedByClient, i.ClaimedAt, i.ClaimExpiresAt = nil, nil, nil, nil
}

// ClaimNext takes the idea at the head of the queue — the highest-priority
// open idea, or a claimed one whose lease lapsed — for clientID.
//
// If this client already holds a live claim in the project, that idea comes
// back instead, renewed, with resumed=true: an agent whose claim_next reply
// was lost and who calls again must get the idea it already took, not a
// second one. found=false is an empty queue, which is an answer, not an
// error.
//
// FOR UPDATE SKIP LOCKED is what makes two agents calling at once take two
// different ideas: the second one does not wait on the row the first one is
// claiming, it passes over it to the next. A row the first one committed
// meanwhile is re-checked against the WHERE clause under its lock and
// dropped, so a fresh claim is never taken twice.
func (s *Service) ClaimNext(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID uuid.UUID) (i Idea, found, resumed bool, err error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaWrite); err != nil {
		return Idea{}, false, false, err
	}
	if clientID == nil {
		return Idea{}, false, false, ErrNoClient
	}
	now := s.now()
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx,
			`SELECT id FROM ideas
			 WHERE project_id = $1 AND status = 'claimed' AND claimed_by_client = $2 AND claim_expires_at > $3
			 ORDER BY claimed_at, id LIMIT 1 FOR UPDATE`, projectID, *clientID, now,
		).Scan(&id)
		switch {
		case err == nil:
			resumed = true
		case errors.Is(err, pgx.ErrNoRows):
			err = tx.QueryRow(ctx,
				`SELECT id FROM ideas
				 WHERE project_id = $1 AND (status = 'open' OR (status = 'claimed' AND claim_expires_at <= $2))
				 ORDER BY priority DESC, position, created_at, id
				 LIMIT 1 FOR UPDATE SKIP LOCKED`, projectID, now,
			).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
		default:
			return err
		}
		found = true
		// Already locked by the query above; load only reads it.
		if i, _, err = s.load(ctx, tx, projectID, id, false); err != nil {
			return err
		}
		s.claim(&i, principalID, clientID, now)
		if err := s.store(ctx, tx, &i, nil); err != nil {
			return err
		}
		if resumed {
			return nil
		}
		return s.audit(ctx, tx, i, principalID, clientID, "idea.claim")
	})
	if err != nil {
		return Idea{}, false, false, fmt.Errorf("ideas: claim next: %w", err)
	}
	if found {
		s.plans.NotifyChanged(projectID)
	}
	return i, found, resumed, nil
}

// Claim takes one particular idea: an open one, or a claimed one whose
// lease lapsed. Claiming an idea this client already holds renews it, so a
// retried claim is harmless; one another client holds live is
// ErrAlreadyClaimed.
func (s *Service) Claim(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, ideaID uuid.UUID) (Idea, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaWrite); err != nil {
		return Idea{}, err
	}
	if clientID == nil {
		return Idea{}, ErrNoClient
	}
	now := s.now()
	var i Idea
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		if i, _, err = s.load(ctx, tx, projectID, ideaID, true); err != nil {
			return err
		}
		renew := i.Status == StatusClaimed && i.HeldBy(clientID)
		switch {
		case renew, i.Status == StatusOpen, i.Lapsed(now):
		case i.Status == StatusClaimed:
			return ErrAlreadyClaimed
		default:
			return wrongStatus(i, "claim")
		}
		s.claim(&i, principalID, clientID, now)
		if err := s.store(ctx, tx, &i, nil); err != nil {
			return err
		}
		if renew {
			return nil
		}
		return s.audit(ctx, tx, i, principalID, clientID, "idea.claim")
	})
	if err != nil {
		return Idea{}, fmt.Errorf("ideas: claim: %w", err)
	}
	s.plans.NotifyChanged(projectID)
	return i, nil
}

// holder checks that clientID holds i's claim. A lapsed claim still counts
// while nobody else has taken it: the work its holder did is no less valid
// for running long, and refusing it would throw that work away for nothing.
func holder(i Idea, clientID *uuid.UUID, what string) error {
	if i.Status != StatusClaimed {
		return wrongStatus(i, what)
	}
	if !i.HeldBy(clientID) {
		return ErrNotClaimant
	}
	return nil
}

// Renew extends the holder's lease by another ClaimLease from now. Not
// audited: it is a heartbeat, like a plan item tick.
func (s *Service) Renew(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, ideaID uuid.UUID) (Idea, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaWrite); err != nil {
		return Idea{}, err
	}
	now := s.now()
	var i Idea
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		if i, _, err = s.load(ctx, tx, projectID, ideaID, true); err != nil {
			return err
		}
		if err := holder(i, clientID, "renew the claim on"); err != nil {
			return err
		}
		expires := now.Add(ClaimLease)
		i.ClaimExpiresAt = &expires
		return s.store(ctx, tx, &i, nil)
	})
	if err != nil {
		return Idea{}, fmt.Errorf("ideas: renew: %w", err)
	}
	s.plans.NotifyChanged(projectID)
	return i, nil
}

// Release gives a claimed idea back to the queue, in its old place. The
// holder may always release; anyone else only as a person in the
// interface, which is how a team frees an idea held by an agent they know
// is gone without waiting out the lease.
func (s *Service) Release(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, ideaID uuid.UUID) (Idea, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaWrite); err != nil {
		return Idea{}, err
	}
	var i Idea
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		if i, _, err = s.load(ctx, tx, projectID, ideaID, true); err != nil {
			return err
		}
		if err := holder(i, clientID, "release"); errors.Is(err, ErrNotClaimant) {
			if requirePerson(ctx, tx, principalID, clientID) != nil {
				return ErrNotClaimant
			}
		} else if err != nil {
			return err
		}
		i.Status = StatusOpen
		clearClaim(&i)
		if err := s.store(ctx, tx, &i, nil); err != nil {
			return err
		}
		return s.audit(ctx, tx, i, principalID, clientID, "idea.release")
	})
	if err != nil {
		return Idea{}, fmt.Errorf("ideas: release: %w", err)
	}
	s.plans.NotifyChanged(projectID)
	return i, nil
}

// ---------------------------------------------------------------- proposals and decisions

// Propose answers a claimed idea with the holder's proposed solution, and
// optionally the draft plan that would carry it out. The idea, the proposal
// and the plan are one commit (plans.CreatePlanInTx). A proposal replaces
// any earlier one and clears the reason that earlier one was rejected for:
// both are about a solution that is no longer on the table.
func (s *Service) Propose(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, ideaID uuid.UUID, proposal string, plan *DraftPlan) (Idea, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaWrite); err != nil {
		return Idea{}, err
	}
	if strings.TrimSpace(proposal) == "" {
		return Idea{}, invalid("a proposal needs text")
	}
	if err := checkLen("proposal", proposal, MaxProposal); err != nil {
		return Idea{}, err
	}
	now := s.now()
	var i Idea
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var b ideaBlob
		var err error
		if i, b, err = s.load(ctx, tx, projectID, ideaID, true); err != nil {
			return err
		}
		if err := holder(i, clientID, "propose for"); err != nil {
			return err
		}
		i.ResultingPlanID = nil
		if plan != nil {
			title := plan.Title
			if strings.TrimSpace(title) == "" {
				title = i.Title
			}
			items := make([]plans.NewItem, len(plan.Items))
			for k, it := range plan.Items {
				items[k] = plans.NewItem{Text: it.Text, Kind: it.Kind}
			}
			p, err := s.plans.CreatePlanInTx(ctx, tx, principalID, clientID, projectID, plan.PhaseID, plans.PlanDraft, title, plan.Body, items)
			if err != nil {
				return translatePlanError(err)
			}
			i.ResultingPlanID = &p.ID
		}
		b.Proposal, b.Rejection = proposal, ""
		i.Status, i.ProposedAt, i.ClaimExpiresAt = StatusProposed, &now, nil
		if err := s.store(ctx, tx, &i, &b); err != nil {
			return err
		}
		return s.audit(ctx, tx, i, principalID, clientID, "idea.propose")
	})
	if err != nil {
		return Idea{}, fmt.Errorf("ideas: propose: %w", quota.Translate(err))
	}
	s.plans.NotifyChanged(projectID)
	return i, nil
}

// translatePlanError restates a refusal from the plan a proposal tried to
// create as this package's, so an adapter maps it without knowing that a
// proposal writes a plan.
func translatePlanError(err error) error {
	switch {
	case errors.Is(err, plans.ErrForbidden):
		return fmt.Errorf("%w: creating the draft plan: %w", ErrForbidden, err)
	case errors.Is(err, plans.ErrInvalid):
		return fmt.Errorf("%w: the draft plan: %s", ErrInvalid, strings.TrimPrefix(err.Error(), plans.ErrInvalid.Error()+": "))
	}
	return err
}

// Accept is a person's yes to a proposal. Its draft plan, if it still is a
// draft, becomes active: that is the moment the work is decided on, and
// the board shows it from then.
func (s *Service) Accept(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, ideaID uuid.UUID) (Idea, error) {
	return s.decide(ctx, principalID, clientID, projectID, ideaID, StatusAccepted, "", false)
}

// Reject is a person's no to a proposal, with the reason. reopen puts the
// idea back in the queue at its old place, so the next planning agent takes
// it again and reads the rejected proposal and the reason before it
// proposes; without reopen the idea is rejected for good. Either way its
// draft plan is abandoned.
func (s *Service) Reject(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, ideaID uuid.UUID, reason string, reopen bool) (Idea, error) {
	if strings.TrimSpace(reason) == "" {
		return Idea{}, invalid("a rejection needs a reason: it is what the next proposal is written against")
	}
	if err := checkLen("reason", reason, MaxRejection); err != nil {
		return Idea{}, err
	}
	return s.decide(ctx, principalID, clientID, projectID, ideaID, StatusRejected, reason, reopen)
}

func (s *Service) decide(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, ideaID uuid.UUID, verdict, reason string, reopen bool) (Idea, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaWrite); err != nil {
		return Idea{}, err
	}
	now := s.now()
	var i Idea
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := requirePerson(ctx, tx, principalID, clientID); err != nil {
			return err
		}
		var b ideaBlob
		var err error
		if i, b, err = s.load(ctx, tx, projectID, ideaID, true); err != nil {
			return err
		}
		what := map[string]string{StatusAccepted: "accept", StatusRejected: "reject"}[verdict]
		if i.Status != StatusProposed {
			return wrongStatus(i, what)
		}
		if i.ResultingPlanID != nil {
			to := plans.PlanActive
			if verdict == StatusRejected {
				to = plans.PlanAbandoned
			}
			if _, err := s.plans.SetStatusInTx(ctx, tx, principalID, projectID, *i.ResultingPlanID, []string{plans.PlanDraft}, to); err != nil {
				return translatePlanError(err)
			}
		}
		i.Status, i.DecidedBy, i.DecidedAt = verdict, &principalID, &now
		var rewrite *ideaBlob
		if verdict == StatusRejected {
			b.Rejection = reason
			rewrite = &b
			if reopen {
				i.Status = StatusOpen
				clearClaim(&i)
			}
		}
		if err := s.store(ctx, tx, &i, rewrite); err != nil {
			return err
		}
		return s.audit(ctx, tx, i, principalID, clientID, "idea."+what)
	})
	if err != nil {
		return Idea{}, fmt.Errorf("ideas: decide: %w", err)
	}
	s.plans.NotifyChanged(projectID)
	return i, nil
}

// Withdraw takes an idea off the stack before it is decided: it is no longer
// wanted. A draft plan its proposal came with is abandoned. Any writer may
// withdraw; the interface offers it, the MCP tools do not, because an agent
// deciding an idea is not worth doing is a proposal, not a withdrawal.
func (s *Service) Withdraw(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, ideaID uuid.UUID) (Idea, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaWrite); err != nil {
		return Idea{}, err
	}
	now := s.now()
	var i Idea
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		if i, _, err = s.load(ctx, tx, projectID, ideaID, true); err != nil {
			return err
		}
		if !slices.Contains([]string{StatusOpen, StatusClaimed, StatusProposed}, i.Status) {
			return wrongStatus(i, "withdraw")
		}
		if i.ResultingPlanID != nil {
			if _, err := s.plans.SetStatusInTx(ctx, tx, principalID, projectID, *i.ResultingPlanID, []string{plans.PlanDraft}, plans.PlanAbandoned); err != nil {
				return translatePlanError(err)
			}
		}
		i.Status, i.ClaimExpiresAt, i.DecidedBy, i.DecidedAt = StatusWithdrawn, nil, &principalID, &now
		if err := s.store(ctx, tx, &i, nil); err != nil {
			return err
		}
		return s.audit(ctx, tx, i, principalID, clientID, "idea.withdraw")
	})
	if err != nil {
		return Idea{}, fmt.Errorf("ideas: withdraw: %w", err)
	}
	s.plans.NotifyChanged(projectID)
	return i, nil
}

// ---------------------------------------------------------------- order

// Update reprioritises or moves an open idea. A new priority sends the idea
// to the end of that priority's line; a position moves it within its line
// (1 is first), renumbering the line densely under the project's order
// lock, as plans do (docs/adr/0022, "Reordering"). Neither is audited: the
// order of a queue is not a security event.
func (s *Service) Update(ctx context.Context, principalID, projectID, ideaID uuid.UUID, u Update) (Idea, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapIdeaWrite); err != nil {
		return Idea{}, err
	}
	if u.Priority != nil {
		if _, ok := priorityValue(*u.Priority); !ok || *u.Priority == "" {
			return Idea{}, invalid("priority %q is not low, normal or high", *u.Priority)
		}
	}
	if u.Position != nil && *u.Position < 1 {
		return Idea{}, invalid("position is 1-based")
	}
	var i Idea
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		// The order lock before the row lock, always, as Create takes them.
		if err := lockOrder(ctx, tx, projectID); err != nil {
			return err
		}
		var err error
		if i, _, err = s.load(ctx, tx, projectID, ideaID, true); err != nil {
			return err
		}
		if i.Status != StatusOpen {
			return wrongStatus(i, "reorder")
		}
		if u.Priority != nil && *u.Priority != i.Priority {
			i.Priority = *u.Priority
			if i.Position, err = nextPosition(ctx, tx, projectID); err != nil {
				return err
			}
			if err := s.store(ctx, tx, &i, nil); err != nil {
				return err
			}
		}
		if u.Position != nil {
			if i.Position, err = s.move(ctx, tx, i, *u.Position); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Idea{}, fmt.Errorf("ideas: update: %w", err)
	}
	s.plans.NotifyChanged(projectID)
	return i, nil
}

// move puts an open idea at a 1-based place among the open ideas of its
// priority and renumbers that line 1..n. The caller holds lockOrder.
func (s *Service) move(ctx context.Context, tx pgx.Tx, i Idea, to int) (int, error) {
	prio, _ := priorityValue(i.Priority)
	rows, err := tx.Query(ctx,
		`SELECT id FROM ideas WHERE project_id = $1 AND status = 'open' AND priority = $2
		 ORDER BY position, created_at, id`, i.ProjectID, prio)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return 0, err
	}
	from := slices.Index(ids, i.ID)
	if from < 0 {
		return 0, ErrNotFound
	}
	ids = slices.Delete(ids, from, from+1)
	at := min(to, len(ids)+1) - 1
	ids = slices.Insert(ids, at, i.ID)
	// Rows already in place are not touched, so moving an idea one step
	// rewrites two rows, not the line.
	_, err = tx.Exec(ctx,
		`UPDATE ideas t SET position = o.n::int
		 FROM unnest($1::uuid[]) WITH ORDINALITY AS o(id, n)
		 WHERE t.id = o.id AND t.position <> o.n`, ids)
	return at + 1, err
}

// ProjectCounts is one project's waiting ideas, for a cross-project read.
type ProjectCounts struct {
	ProjectID uuid.UUID
	Path      string
	Name      string
	Counts
}

// CountsAll is Counts for every project whose role may read ideas, leaving
// out projects with nothing waiting. One query: the dashboard asks it on
// every load, and a person in thirty projects must not pay thirty round trips.
func (s *Service) CountsAll(ctx context.Context, principalID uuid.UUID) ([]ProjectCounts, error) {
	out := []ProjectCounts{}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		memberships, err := projects.NewStore().ListForPrincipal(ctx, tx, principalID)
		if err != nil {
			return err
		}
		refs := map[uuid.UUID]ProjectCounts{}
		var ids []uuid.UUID
		for _, ms := range memberships {
			if !auth.RoleCan(ms.Role, auth.CapIdeaRead) {
				continue
			}
			ids = append(ids, ms.Project.ID)
			refs[ms.Project.ID] = ProjectCounts{ProjectID: ms.Project.ID, Path: ms.NamespaceSlug + "/" + ms.Project.Slug, Name: ms.Project.Name}
		}
		if len(ids) == 0 {
			return nil
		}
		rows, err := tx.Query(ctx,
			`SELECT project_id,
			        count(*) FILTER (WHERE status = 'open'),
			        count(*) FILTER (WHERE status = 'claimed'),
			        count(*) FILTER (WHERE status = 'claimed' AND claim_expires_at <= $2),
			        count(*) FILTER (WHERE status = 'proposed')
			 FROM ideas WHERE project_id = ANY($1) AND status IN ('open', 'claimed', 'proposed')
			 GROUP BY project_id`, ids, s.now())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var c Counts
			if err := rows.Scan(&id, &c.Open, &c.Claimed, &c.Lapsed, &c.Proposed); err != nil {
				return err
			}
			r := refs[id]
			r.Counts = c
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("ideas: counts all: %w", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Proposed != out[j].Proposed {
			return out[i].Proposed > out[j].Proposed
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}
