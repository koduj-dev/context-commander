package memories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/quota"
)

// Service is the authorization chokepoint for memories (docs/adr/0014):
// every method checks a capability before touching data. Memories reuse the
// plan capabilities — every role reads, every role that can write the
// project writes — because they are the same kind of shared working record
// and a team that may tick a plan may say what it learned doing it.
type Service struct {
	pool   *db.Pool
	crypto *crypto.Provider
	authz  *auth.AuthzService
	// ranker orders search candidates (search.go). Replaceable so a
	// semantic ranker can be plugged in without changing any caller.
	ranker Ranker
}

func NewService(pool *db.Pool, cryptoProvider *crypto.Provider, authz *auth.AuthzService) *Service {
	return &Service{pool: pool, crypto: cryptoProvider, authz: authz, ranker: TextRanker{}}
}

// SetRanker replaces how search ranks candidates (search.go).
func (s *Service) SetRanker(r Ranker) { s.ranker = r }

func (s *Service) checkCapability(ctx context.Context, principalID, projectID uuid.UUID, cap auth.Capability) error {
	allowed, err := s.authz.Can(ctx, principalID, projectID, cap)
	if err != nil {
		return fmt.Errorf("memories: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// ---------------------------------------------------------------- validation

func checkLen(field, v string, max int) error {
	if utf8.RuneCountInString(v) > max {
		return invalid("%s is longer than %d characters", field, max)
	}
	return nil
}

// checkLabel bounds a one-line plaintext label (branch, ticket key): no
// control characters, so nothing stored here can break a log line or a
// table cell, and no whitespace where field says it has none.
func checkLabel(field, v string, max int, spaces bool) error {
	if err := checkLen(field, v, max); err != nil {
		return err
	}
	for _, c := range v {
		if unicode.IsControl(c) || (!spaces && unicode.IsSpace(c)) {
			return invalid("%s contains whitespace or a control character", field)
		}
	}
	return nil
}

// checkTicketURL accepts an absolute http or https URL with a host and no
// credentials. The interface renders it as a link, so a javascript: or
// data: URL here would be a script someone else clicks; and a tracker URL
// with a password in it would be a secret stored as plaintext metadata.
func checkTicketURL(v string) error {
	if v == "" {
		return nil
	}
	if err := checkLabel("ticket_url", v, MaxTicketURL, false); err != nil {
		return err
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return invalid("ticket_url must be an absolute http or https URL")
	}
	if u.User != nil {
		return invalid("ticket_url must not carry credentials")
	}
	return nil
}

// normalizeTags lowercases, trims and de-duplicates tags, keeping their
// order. Lowercased because a tag is a filter, and "Billing" and "billing"
// being two filters is never what anyone meant.
func normalizeTags(in []string) ([]string, error) {
	out := []string{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if err := checkLabel("tag", t, MaxTag, false); err != nil {
			return nil, err
		}
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	if len(out) > MaxTags {
		return nil, invalid("a memory has at most %d tags", MaxTags)
	}
	return out, nil
}

// normalize trims and validates everything a memory says, in place.
func normalize(m *Memory) error {
	m.Title = strings.TrimSpace(m.Title)
	m.Branch = strings.TrimSpace(m.Branch)
	m.TicketKey = strings.TrimSpace(m.TicketKey)
	m.TicketURL = strings.TrimSpace(m.TicketURL)
	if m.Title == "" {
		return invalid("a memory needs a title")
	}
	if !validKind(m.Kind) {
		return invalid("kind %q is not one of %s", m.Kind, strings.Join(Kinds, ", "))
	}
	tags, err := normalizeTags(m.Tags)
	if err != nil {
		return err
	}
	m.Tags = tags
	return errors.Join(
		checkLen("title", m.Title, MaxTitle),
		checkLen("body", m.Body, MaxBody),
		checkLabel("branch", m.Branch, MaxBranch, false),
		checkLabel("ticket_key", m.TicketKey, MaxTicketKey, false),
		checkTicketURL(m.TicketURL),
	)
}

// checkLinks verifies that what a memory points at is in its project: an
// assignee who is a member, a plan and step of this project's, and a step
// that belongs to the plan named with it. The foreign keys alone would
// accept another project's plan id, and RLS would only hide it.
func checkLinks(ctx context.Context, tx pgx.Tx, m *Memory) error {
	if m.Assignee != nil {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM project_members WHERE project_id = $1 AND principal_id = $2)`,
			m.ProjectID, *m.Assignee).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return invalid("the assignee is not a member of this project")
		}
	}
	if m.PlanItemID != nil {
		var planID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT plan_id FROM plan_items WHERE id = $1 AND project_id = $2`, *m.PlanItemID, m.ProjectID).Scan(&planID)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid("plan item %s is not in this project", *m.PlanItemID)
		}
		if err != nil {
			return err
		}
		if m.PlanID == nil {
			m.PlanID = &planID
		} else if *m.PlanID != planID {
			return invalid("plan item %s is not in plan %s", *m.PlanItemID, *m.PlanID)
		}
	}
	if m.PlanID != nil {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM plans WHERE id = $1 AND project_id = $2)`, *m.PlanID, m.ProjectID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return invalid("plan %s is not in this project", *m.PlanID)
		}
	}
	return nil
}

// ---------------------------------------------------------------- blobs

// blob is what a memory says: everything a reader would call its content.
type blob struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

type envelope struct {
	ciphertext, nonce, wrappedDEK []byte
	keyVersion                    int
}

func encryptionContext(projectID, id uuid.UUID, version int) crypto.EncryptionContext {
	return crypto.EncryptionContext{
		ProjectID: projectID, ResourceType: "memory",
		ResourceID: id, ResourceVersion: uint64(version), //nolint:gosec // a small positive counter
	}
}

func (s *Service) seal(ctx context.Context, tx pgx.Tx, m Memory) (envelope, error) {
	plaintext, err := json.Marshal(blob{Title: m.Title, Body: m.Body})
	if err != nil {
		return envelope{}, fmt.Errorf("memories: marshal: %w", err)
	}
	var e envelope
	e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion, err = s.crypto.Encrypt(ctx, tx, encryptionContext(m.ProjectID, m.ID, m.Version), plaintext)
	if err != nil {
		return envelope{}, fmt.Errorf("memories: encrypt: %w", err)
	}
	return e, nil
}

func (s *Service) open(ctx context.Context, tx pgx.Tx, r *row) error {
	plaintext, err := s.crypto.Decrypt(ctx, tx, encryptionContext(r.ProjectID, r.ID, r.Version),
		r.e.ciphertext, r.e.nonce, r.e.wrappedDEK, r.e.keyVersion)
	if err != nil {
		return fmt.Errorf("memories: decrypt: %w", err)
	}
	var b blob
	if err := json.Unmarshal(plaintext, &b); err != nil {
		return err
	}
	r.Title, r.Body = b.Title, b.Body
	return nil
}

// row is a memory as stored, before its blob is opened.
type row struct {
	Memory
	e envelope
}

const columns = `id, project_id, kind, status, version, COALESCE(branch, ''), COALESCE(ticket_key, ''), COALESCE(ticket_url, ''), tags,
	assignee_principal, plan_id, plan_item_id, superseded_by, ciphertext, nonce, wrapped_dek, project_key_version,
	created_by_principal, created_by_client, created_at, updated_at`

func scan(r pgx.Row, m *row) error {
	return r.Scan(&m.ID, &m.ProjectID, &m.Kind, &m.Status, &m.Version, &m.Branch, &m.TicketKey, &m.TicketURL, &m.Tags,
		&m.Assignee, &m.PlanID, &m.PlanItemID, &m.SupersededBy, &m.e.ciphertext, &m.e.nonce, &m.e.wrappedDEK, &m.e.keyVersion,
		&m.CreatedBy, &m.CreatedByClient, &m.CreatedAt, &m.UpdatedAt)
}

// load reads one memory, decrypted. forUpdate locks the row, which is what
// serializes edits against the version check.
func (s *Service) load(ctx context.Context, tx pgx.Tx, projectID, id uuid.UUID, forUpdate bool) (Memory, error) {
	q := `SELECT ` + columns + ` FROM memories WHERE id = $1 AND project_id = $2`
	if forUpdate {
		q += ` FOR UPDATE`
	}
	var r row
	if err := scan(tx.QueryRow(ctx, q, id, projectID), &r); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, ErrNotFound
		}
		return Memory{}, err
	}
	if err := s.open(ctx, tx, &r); err != nil {
		return Memory{}, err
	}
	return r.Memory, nil
}

// nullable turns "" into NULL, so an absent label is absent in SQL too and
// "branch IS NULL" means what it says.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---------------------------------------------------------------- writes

// Create records a memory. Audited: a memory is shared project knowledge,
// and who put it there is worth answering later.
func (s *Service) Create(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID uuid.UUID, n NewMemory) (Memory, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Memory{}, err
	}
	if n.Kind == "" {
		n.Kind = KindNote
	}
	m := Memory{
		ID: uuid.New(), ProjectID: projectID, Kind: n.Kind, Status: StatusActive, Version: 1,
		Title: n.Title, Body: n.Body, Branch: n.Branch, TicketKey: n.TicketKey, TicketURL: n.TicketURL, Tags: n.Tags,
		Assignee: n.Assignee, PlanID: n.PlanID, PlanItemID: n.PlanItemID, CreatedBy: principalID, CreatedByClient: clientID,
	}
	if err := normalize(&m); err != nil {
		return Memory{}, err
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := checkLinks(ctx, tx, &m); err != nil {
			return err
		}
		e, err := s.seal(ctx, tx, m)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO memories (id, project_id, kind, status, version, branch, ticket_key, ticket_url, tags,
			   assignee_principal, plan_id, plan_item_id, ciphertext, nonce, wrapped_dek, project_key_version,
			   created_by_principal, created_by_client)
			 VALUES ($1, $2, $3, $4, 1, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
			 RETURNING created_at, updated_at`,
			m.ID, projectID, m.Kind, m.Status, nullable(m.Branch), nullable(m.TicketKey), nullable(m.TicketURL), m.Tags,
			m.Assignee, m.PlanID, m.PlanItemID, e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion, principalID, clientID,
		).Scan(&m.CreatedAt, &m.UpdatedAt); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID, ClientID: clientID,
			Action: "memory.create", ResourceType: "memory", ResourceID: &m.ID, Result: audit.ResultAllow,
		})
	})
	if err != nil {
		return Memory{}, fmt.Errorf("memories: create: %w", quota.Translate(err))
	}
	return m, nil
}

func (s *Service) Get(ctx context.Context, principalID, projectID, id uuid.UUID) (Memory, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return Memory{}, err
	}
	var m Memory
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		m, err = s.load(ctx, tx, projectID, id, false)
		return err
	})
	if err != nil {
		return Memory{}, fmt.Errorf("memories: get: %w", err)
	}
	return m, nil
}

// Update edits a memory's text or metadata against the version the caller
// read. Not audited and no outbox event, like a plan's text edit: an edit is
// not a security event, and the row's version and updated_at already say
// that it happened. No history of the text is kept (docs/adr/0025).
func (s *Service) Update(ctx context.Context, principalID, projectID, id uuid.UUID, u Update) (Memory, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Memory{}, err
	}
	var m Memory
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		if m, err = s.load(ctx, tx, projectID, id, true); err != nil {
			return err
		}
		if u.ExpectedVersion != m.Version {
			return ErrConflict
		}
		set := func(dst *string, src *string) {
			if src != nil {
				*dst = *src
			}
		}
		set(&m.Kind, u.Kind)
		set(&m.Title, u.Title)
		set(&m.Body, u.Body)
		set(&m.Branch, u.Branch)
		set(&m.TicketKey, u.TicketKey)
		set(&m.TicketURL, u.TicketURL)
		if u.Tags != nil {
			m.Tags = *u.Tags
		}
		if u.AssigneeSet {
			m.Assignee = u.Assignee
		}
		if err := normalize(&m); err != nil {
			return err
		}
		if u.AssigneeSet {
			if err := checkLinks(ctx, tx, &Memory{ProjectID: projectID, Assignee: m.Assignee}); err != nil {
				return err
			}
		}
		// Every edit re-seals, even one that touches only metadata: the
		// version is the ciphertext's AAD, so it cannot move without it.
		m.Version++
		e, err := s.seal(ctx, tx, m)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`UPDATE memories SET kind = $2, version = $3, branch = $4, ticket_key = $5, ticket_url = $6, tags = $7,
			   assignee_principal = $8, ciphertext = $9, nonce = $10, wrapped_dek = $11, project_key_version = $12, updated_at = now()
			 WHERE id = $1 RETURNING updated_at`,
			m.ID, m.Kind, m.Version, nullable(m.Branch), nullable(m.TicketKey), nullable(m.TicketURL), m.Tags,
			m.Assignee, e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion,
		).Scan(&m.UpdatedAt)
	})
	if err != nil {
		return Memory{}, fmt.Errorf("memories: update: %w", quota.Translate(err))
	}
	return m, nil
}

// SetStatus archives or reactivates a memory. Superseding goes through
// Supersede, which names the replacement. No version is needed, like a
// plan's status: it rewrites no text, and setting a status twice is the
// same as setting it once.
func (s *Service) SetStatus(ctx context.Context, principalID, projectID, id uuid.UUID, status string) (Memory, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Memory{}, err
	}
	if status != StatusActive && status != StatusArchived {
		return Memory{}, invalid("status %q is not active or archived; supersede a memory by naming its replacement", status)
	}
	return s.setStatus(ctx, principalID, projectID, id, status, nil)
}

// Supersede marks a memory as replaced by another one in the same project.
// The old memory stays readable — "we used to think X" is worth keeping —
// but drops out of the default list, search and bootstrap.
func (s *Service) Supersede(ctx context.Context, principalID, projectID, id, by uuid.UUID) (Memory, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Memory{}, err
	}
	if id == by {
		return Memory{}, invalid("a memory cannot supersede itself")
	}
	return s.setStatus(ctx, principalID, projectID, id, StatusSuperseded, &by)
}

func (s *Service) setStatus(ctx context.Context, principalID, projectID, id uuid.UUID, status string, by *uuid.UUID) (Memory, error) {
	var m Memory
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if by != nil {
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM memories WHERE id = $1 AND project_id = $2)`, *by, projectID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return invalid("memory %s is not in this project", *by)
			}
		}
		var err error
		if m, err = s.load(ctx, tx, projectID, id, true); err != nil {
			return err
		}
		m.Status, m.SupersededBy = status, by
		return tx.QueryRow(ctx,
			`UPDATE memories SET status = $2, superseded_by = $3, updated_at = now() WHERE id = $1 RETURNING updated_at`,
			m.ID, m.Status, m.SupersededBy,
		).Scan(&m.UpdatedAt)
	})
	if err != nil {
		return Memory{}, fmt.Errorf("memories: set status: %w", err)
	}
	return m, nil
}

// Delete removes a memory outright, as plans are deleted: the row and its
// ciphertext go, the bytes return to the quota, and a memory it superseded
// keeps pointing at nothing (ON DELETE SET NULL). Archive is the soft way.
func (s *Service) Delete(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID, id uuid.UUID) error {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return err
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM memories WHERE id = $1 AND project_id = $2`, id, projectID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID, ClientID: clientID,
			Action: "memory.delete", ResourceType: "memory", ResourceID: &id, Result: audit.ResultAllow,
		})
	})
	if err != nil {
		return fmt.Errorf("memories: delete: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------- reads

// validFilter refuses a filter naming a kind or status that does not exist,
// so a typo is an error rather than an empty answer that looks like
// "nothing was ever recorded".
func validFilter(f Filter) error {
	if f.Kind != "" && !validKind(f.Kind) {
		return invalid("kind %q is not one of %s", f.Kind, strings.Join(Kinds, ", "))
	}
	if f.Status != "" && f.Status != StatusAny && !validStatus(f.Status) {
		return invalid("status %q is not one of active, superseded, archived, any", f.Status)
	}
	return nil
}

// where builds the metadata half of a list or search. Every value travels
// as a parameter; the clause text is fixed in this function.
func where(projectID uuid.UUID, f Filter) (string, []any) {
	status := f.Status
	if status == "" {
		status = StatusActive
	}
	if status == StatusAny {
		status = ""
	}
	args := []any{projectID, f.Kind, f.Branch, f.TicketKey, strings.ToLower(strings.TrimSpace(f.Tag)), f.Assignee, f.Author, status,
		f.CreatedAfter, f.CreatedBefore}
	return `project_id = $1
		AND ($2 = '' OR kind = $2)
		AND ($3 = '' OR branch = $3)
		AND ($4 = '' OR ticket_key = $4)
		AND ($5 = '' OR tags @> ARRAY[$5]::text[])
		AND ($6::uuid IS NULL OR assignee_principal = $6)
		AND ($7::uuid IS NULL OR created_by_principal = $7)
		AND ($8 = '' OR status = $8)
		AND ($9::timestamptz IS NULL OR created_at > $9)
		AND ($10::timestamptz IS NULL OR created_at < $10)`, args
}

// query reads up to limit memories matching the filter, newest first,
// starting after the cursor position when one is given, decrypted.
func (s *Service) query(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, f Filter, after *cursor, limit int) ([]Memory, error) {
	clause, args := where(projectID, f)
	q := `SELECT ` + columns + ` FROM memories WHERE ` + clause
	if after != nil {
		args = append(args, after.t, after.id)
		q += fmt.Sprintf(` AND (created_at, id) < ($%d, $%d)`, len(args)-1, len(args))
	}
	args = append(args, limit)
	q += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args))

	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var list []row
	for rows.Next() {
		var r row
		if err := scan(rows, &r); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Decrypted only after the rows are closed: pgx refuses a second query
	// on the transaction while one is open, and Decrypt needs one.
	out := make([]Memory, 0, len(list))
	for i := range list {
		if err := s.open(ctx, tx, &list[i]); err != nil {
			return nil, err
		}
		out = append(out, list[i].Memory)
	}
	return out, nil
}

// List pages through a project's memories, newest first, narrowed by
// metadata. The filtering is SQL over plaintext columns; only the page that
// comes back is decrypted.
func (s *Service) List(ctx context.Context, principalID, projectID uuid.UUID, f Filter, opts paging.Options) (paging.Page[Memory], error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return paging.Page[Memory]{}, err
	}
	if err := validFilter(f); err != nil {
		return paging.Page[Memory]{}, err
	}
	t, id, ok, err := opts.DecodeTimeUUID()
	if err != nil {
		return paging.Page[Memory]{}, err
	}
	var after *cursor
	if ok {
		after = &cursor{t: t, id: id}
	}
	limit := opts.ClampedLimit()
	var list []Memory
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		list, err = s.query(ctx, tx, projectID, f, after, limit+1)
		return err
	})
	if err != nil {
		return paging.Page[Memory]{}, fmt.Errorf("memories: list: %w", err)
	}
	items, more := paging.More(list, limit)
	page := paging.Page[Memory]{Items: items}
	if more {
		last := items[len(items)-1]
		page.NextCursor = paging.EncodeTimeUUID(last.CreatedAt, last.ID)
	}
	return page, nil
}

// People names the principals a screen shows next to memories: authors and
// assignees. Only names, read under the caller's RLS context.
func (s *Service) People(ctx context.Context, principalID uuid.UUID, list []Memory) (map[uuid.UUID]Person, error) {
	ids := map[uuid.UUID]bool{}
	for _, m := range list {
		ids[m.CreatedBy] = true
		if m.Assignee != nil {
			ids[*m.Assignee] = true
		}
	}
	var out map[uuid.UUID]Person
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		out, err = resolvePeople(ctx, tx, ids)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("memories: people: %w", err)
	}
	return out, nil
}

func resolvePeople(ctx context.Context, tx pgx.Tx, ids map[uuid.UUID]bool) (map[uuid.UUID]Person, error) {
	out := map[uuid.UUID]Person{}
	if len(ids) == 0 {
		return out, nil
	}
	list := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	rows, err := tx.Query(ctx,
		`SELECT p.id, p.kind, COALESCE(NULLIF(u.display_name, ''), u.email, sp.name, '')
		 FROM principals p
		 LEFT JOIN users u ON u.id = p.user_id
		 LEFT JOIN service_principals sp ON sp.id = p.service_principal_id
		 WHERE p.id = ANY($1)`, list)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.PrincipalID, &p.Kind, &p.Name); err != nil {
			return nil, err
		}
		out[p.PrincipalID] = p
	}
	return out, rows.Err()
}
