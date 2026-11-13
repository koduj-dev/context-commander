package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/plans"
	"github.com/koduj-dev/context-commander/internal/quota"
)

// Notifier is told that something a project's board shows changed: a
// message was sent, delivered or acknowledged. plans.Service is one; its
// board stream then tells open screens to re-read (docs/adr/0022).
type Notifier interface {
	Notify(projectID uuid.UUID)
}

// Service is the authorization chokepoint for messages (docs/adr/0014).
//
// Sending checks CapPlanWrite in the project, and that the caller is a
// person in a browser session. Reading an inbox needs no project
// capability beyond membership, which RLS enforces: an inbox is only ever
// the caller's own messages, read under the caller's RLS context, and every
// role holds plan.read. The sender's list checks CapPlanRead.
type Service struct {
	pool     *db.Pool
	crypto   *crypto.Provider
	authz    *auth.AuthzService
	notifier Notifier
	now      func() time.Time

	mu     sync.Mutex
	unread map[uuid.UUID]unreadEntry
}

type unreadEntry struct {
	u  Unread
	at time.Time
}

// maxUnreadCache bounds the unread cache. Past it the cache is dropped
// whole: a fresh count is one indexed query, and a map that only grows is a
// slow leak.
const maxUnreadCache = 10000

func NewService(pool *db.Pool, cryptoProvider *crypto.Provider, authz *auth.AuthzService, notifier Notifier) *Service {
	return &Service{
		pool: pool, crypto: cryptoProvider, authz: authz, notifier: notifier,
		now: func() time.Time { return time.Now().UTC() }, unread: map[uuid.UUID]unreadEntry{},
	}
}

func (s *Service) checkCapability(ctx context.Context, principalID, projectID uuid.UUID, cap auth.Capability) error {
	allowed, err := s.authz.Can(ctx, principalID, projectID, cap)
	if err != nil {
		return fmt.Errorf("messages: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (s *Service) notify(projectID uuid.UUID) {
	if s.notifier != nil {
		s.notifier.Notify(projectID)
	}
}

// forget drops cached unread counts: one session's, or all of them.
func (s *Service) forget(clientID *uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if clientID == nil {
		s.unread = map[uuid.UUID]unreadEntry{}
		return
	}
	delete(s.unread, *clientID)
}

// ---------------------------------------------------------------- blobs

type blob struct {
	Body string `json:"body"`
}

func encryptionContext(projectID, id uuid.UUID) crypto.EncryptionContext {
	// A message is never rewritten, so its version is always 1.
	return crypto.EncryptionContext{ProjectID: projectID, ResourceType: "agent_message", ResourceID: id, ResourceVersion: 1}
}

type envelope struct {
	ciphertext, nonce, wrappedDEK []byte
	keyVersion                    int
}

func (s *Service) open(ctx context.Context, tx pgx.Tx, m *Message, e envelope) error {
	plaintext, err := s.crypto.Decrypt(ctx, tx, encryptionContext(m.ProjectID, m.ID), e.ciphertext, e.nonce, e.wrappedDEK, e.keyVersion)
	if err != nil {
		return fmt.Errorf("messages: decrypt: %w", err)
	}
	var b blob
	if err := json.Unmarshal(plaintext, &b); err != nil {
		return err
	}
	m.Body = b.Body
	return nil
}

// requirePerson is the same rule internal/ideas applies to accepting a
// proposal: a user principal, on a browser session credential.
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
		return fmt.Errorf("messages: read credential: %w", err)
	}
	if kind != string(identity.PrincipalKindUser) || clientType != identity.SessionClientType {
		return ErrNeedsPerson
	}
	return nil
}

// resolveRecipient checks that a message's recipient is on the project and
// fills in the principal of a session recipient.
func resolveRecipient(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, n *NewMessage) error {
	if n.RecipientClient != nil {
		var owner uuid.UUID
		var clientType string
		var revoked *time.Time
		err := tx.QueryRow(ctx, `SELECT principal_id, client_type, revoked_at FROM client_credentials WHERE id = $1`, *n.RecipientClient).
			Scan(&owner, &clientType, &revoked)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && revoked != nil) {
			return invalid("session %s does not exist or was revoked", *n.RecipientClient)
		}
		if err != nil {
			return err
		}
		if clientType == identity.SessionClientType {
			return invalid("session %s is a browser, and messages are for agents", *n.RecipientClient)
		}
		if n.RecipientPrincipal != nil && *n.RecipientPrincipal != owner {
			return invalid("session %s does not belong to principal %s", *n.RecipientClient, *n.RecipientPrincipal)
		}
		n.RecipientPrincipal = &owner
	}
	if n.RecipientPrincipal != nil {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM project_members WHERE project_id = $1 AND principal_id = $2)`,
			projectID, *n.RecipientPrincipal).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return invalid("the recipient is not a member of this project")
		}
	}
	return nil
}

// ---------------------------------------------------------------- send

// Send leaves a message for an agent. Audited (message.send): a message is
// read by an agent as its user speaking, and who said what to it is worth
// answering later. The audit row names the message, not its text.
func (s *Service) Send(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, projectID uuid.UUID, n NewMessage) (Message, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanWrite); err != nil {
		return Message{}, err
	}
	n.Body = strings.TrimSpace(n.Body)
	if n.Body == "" {
		return Message{}, invalid("a message needs a body")
	}
	if len(n.Body) > MaxBody {
		return Message{}, invalid("the body is longer than %d bytes", MaxBody)
	}
	if n.TTL == 0 {
		n.TTL = DefaultTTL
	}
	if n.TTL < MinTTL || n.TTL > MaxTTL {
		return Message{}, invalid("a message expires between %s and %d days after it is sent", MinTTL, int(MaxTTL/(24*time.Hour)))
	}

	now := s.now()
	m := Message{
		ID: uuid.New(), ProjectID: projectID, Sender: principalID, SenderClient: clientID,
		Body: n.Body, CreatedAt: now, ExpiresAt: now.Add(n.TTL),
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := requirePerson(ctx, tx, principalID, clientID); err != nil {
			return err
		}
		if err := resolveRecipient(ctx, tx, projectID, &n); err != nil {
			return err
		}
		m.RecipientPrincipal, m.RecipientClient = n.RecipientPrincipal, n.RecipientClient
		plaintext, err := json.Marshal(blob{Body: m.Body})
		if err != nil {
			return err
		}
		ct, nonce, dek, kv, err := s.crypto.Encrypt(ctx, tx, encryptionContext(projectID, m.ID), plaintext)
		if err != nil {
			return fmt.Errorf("messages: encrypt: %w", err)
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO agent_messages (id, project_id, sender_principal, sender_client, recipient_principal, recipient_client,
			   ciphertext, nonce, wrapped_dek, project_key_version, created_at, expires_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			 RETURNING seq`,
			m.ID, projectID, principalID, clientID, m.RecipientPrincipal, m.RecipientClient,
			ct, nonce, dek, kv, m.CreatedAt, m.ExpiresAt,
		).Scan(&m.Seq); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT cc_prune_agent_messages()`); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID, ClientID: clientID,
			Action: "message.send", ResourceType: "agent_message", ResourceID: &m.ID, Result: audit.ResultAllow,
		})
	})
	if err != nil {
		return Message{}, fmt.Errorf("messages: send: %w", quota.Translate(err))
	}
	// Every cached count may now be short by one; sends are rare enough
	// that dropping them all is cheaper than working out whose.
	s.forget(nil)
	s.notify(projectID)
	return m, nil
}

// ---------------------------------------------------------------- inbox

// addressedTo is the predicate "this message is for session $1 of principal
// $2": sent to the session, to its principal, or to everyone — and not sent
// from this very session, so a person's broadcast does not come back to the
// browser that sent it. RLS adds "in a project the principal is a member
// of". The caller's query names the table m.
const addressedTo = `m.expires_at > now()
	AND m.sender_client IS DISTINCT FROM $1
	AND (m.recipient_client = $1
	     OR (m.recipient_client IS NULL AND m.recipient_principal = $2)
	     OR m.recipient_principal IS NULL)`

// InboxPage is one read of a session's inbox.
type InboxPage struct {
	Items      []Message
	NextCursor string
	// People names the senders.
	People map[uuid.UUID]plans.Person
}

// InboxOptions narrows an inbox read.
type InboxOptions struct {
	paging.Options
	// ProjectID limits the read to one project; nil reads every project
	// the caller is a member of.
	ProjectID *uuid.UUID
	// Ack acknowledges what is returned in the same call.
	Ack bool
}

// Inbox returns the messages waiting for a session that it has not
// acknowledged, oldest first, and records them as delivered to it.
//
// Delivered is not acknowledged: a message is offered again on a read
// without a cursor until the session acks it, so a client that crashed
// between reading and acting loses nothing. The cursor walks forward within
// one read; the notice on MCP tool results counts only what was never
// delivered, so it stops as soon as the messages were handed over.
func (s *Service) Inbox(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, opts InboxOptions) (InboxPage, error) {
	if clientID == nil {
		return InboxPage{}, ErrNoClient
	}
	if opts.ProjectID != nil {
		if err := s.checkCapability(ctx, principalID, *opts.ProjectID, auth.CapPlanRead); err != nil {
			return InboxPage{}, err
		}
	}
	after, _, err := opts.DecodeSeq()
	if err != nil {
		return InboxPage{}, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultInboxLimit
	}
	limit = min(limit, MaxInboxLimit)

	var page InboxPage
	delivered := map[uuid.UUID]bool{}
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT m.id, m.seq, m.project_id, n.slug || '/' || p.slug, m.sender_principal, m.sender_client,
			        m.recipient_principal, m.recipient_client, m.ciphertext, m.nonce, m.wrapped_dek, m.project_key_version,
			        m.created_at, m.expires_at, d.delivered_at, d.acked_at
			 FROM agent_messages m
			 JOIN projects p ON p.id = m.project_id
			 JOIN namespaces n ON n.id = p.namespace_id
			 LEFT JOIN agent_message_deliveries d ON d.message_id = m.id AND d.client_id = $1
			 WHERE `+addressedTo+`
			   AND d.acked_at IS NULL
			   AND m.seq > $3
			   AND ($4::uuid IS NULL OR m.project_id = $4)
			 ORDER BY m.seq
			 LIMIT $5`,
			*clientID, principalID, after, opts.ProjectID, limit+1)
		if err != nil {
			return err
		}
		type item struct {
			m Message
			e envelope
		}
		var list []item
		for rows.Next() {
			var it item
			var deliveredAt, ackedAt *time.Time
			if err := rows.Scan(&it.m.ID, &it.m.Seq, &it.m.ProjectID, &it.m.ProjectPath, &it.m.Sender, &it.m.SenderClient,
				&it.m.RecipientPrincipal, &it.m.RecipientClient, &it.e.ciphertext, &it.e.nonce, &it.e.wrappedDEK, &it.e.keyVersion,
				&it.m.CreatedAt, &it.m.ExpiresAt, &deliveredAt, &ackedAt); err != nil {
				rows.Close()
				return err
			}
			if deliveredAt != nil {
				it.m.Deliveries = []Delivery{{ClientID: *clientID, PrincipalID: principalID, DeliveredAt: *deliveredAt, AckedAt: ackedAt}}
			}
			list = append(list, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		list, more := paging.More(list, limit)
		ids := map[uuid.UUID]bool{}
		for _, it := range list {
			if err := s.open(ctx, tx, &it.m, it.e); err != nil {
				return err
			}
			page.Items = append(page.Items, it.m)
			ids[it.m.Sender] = true
		}
		if more {
			page.NextCursor = paging.EncodeSeq(list[len(list)-1].m.Seq)
		}
		if len(page.Items) == 0 {
			page.People = map[uuid.UUID]plans.Person{}
			return nil
		}
		msgIDs := make([]uuid.UUID, len(page.Items))
		for i, m := range page.Items {
			msgIDs[i] = m.ID
		}
		changed, err := record(ctx, tx, principalID, *clientID, msgIDs, opts.Ack)
		if err != nil {
			return err
		}
		for _, p := range changed {
			delivered[p] = true
		}
		page.People, err = plans.ResolvePeople(ctx, tx, ids)
		return err
	})
	if err != nil {
		return InboxPage{}, fmt.Errorf("messages: inbox: %w", err)
	}
	s.forget(clientID)
	for p := range delivered {
		s.notify(p)
	}
	return page, nil
}

// record writes this session's delivery of the given messages, and with ack
// its acknowledgement, and returns the projects whose rows changed. Only
// messages addressed to the session are touched: an id from elsewhere is
// skipped, never an error, so an ack cannot be used to probe for ids.
func record(ctx context.Context, tx pgx.Tx, principalID, clientID uuid.UUID, ids []uuid.UUID, ack bool) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx,
		`INSERT INTO agent_message_deliveries (message_id, client_id, project_id, recipient_principal, sender_principal, delivered_at, acked_at)
		 SELECT m.id, $1, m.project_id, $2, m.sender_principal, now(), CASE WHEN $4::boolean THEN now() END
		 FROM agent_messages m
		 WHERE m.id = ANY($3) AND `+addressedTo+`
		 ON CONFLICT (message_id, client_id) DO UPDATE
		   SET acked_at = COALESCE(agent_message_deliveries.acked_at, EXCLUDED.acked_at)
		   WHERE agent_message_deliveries.acked_at IS NULL AND EXCLUDED.acked_at IS NOT NULL
		 RETURNING project_id`,
		clientID, principalID, ids, ack)
	if err != nil {
		return nil, err
	}
	projects, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	slices.SortFunc(projects, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(projects), nil
}

// Ack acknowledges messages for a session: it has read them and taken them
// into account. Acknowledging a message twice, or one that is not the
// session's, changes nothing. Returns how many were newly acknowledged.
func (s *Service) Ack(ctx context.Context, principalID uuid.UUID, clientID *uuid.UUID, ids []uuid.UUID) (int, error) {
	if clientID == nil {
		return 0, ErrNoClient
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > MaxInboxLimit {
		return 0, invalid("acknowledge at most %d messages at once", MaxInboxLimit)
	}
	var changed []uuid.UUID
	var n int
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		// Count before recording: RETURNING says which projects changed,
		// and a project can hold several of the acknowledged messages.
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM agent_messages m
			 LEFT JOIN agent_message_deliveries d ON d.message_id = m.id AND d.client_id = $1
			 WHERE m.id = ANY($3) AND d.acked_at IS NULL AND `+addressedTo,
			*clientID, principalID, ids).Scan(&n); err != nil {
			return err
		}
		var err error
		changed, err = record(ctx, tx, principalID, *clientID, ids, true)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("messages: ack: %w", err)
	}
	s.forget(clientID)
	for _, p := range changed {
		s.notify(p)
	}
	return n, nil
}

// Unread is how many messages are waiting for a session that were never
// delivered to it, and who sent them. Cached per session for
// UnreadCacheTTL: it runs on every MCP tool call.
func (s *Service) Unread(ctx context.Context, principalID, clientID uuid.UUID) (Unread, error) {
	now := s.now()
	s.mu.Lock()
	if e, ok := s.unread[clientID]; ok && now.Sub(e.at) < UnreadCacheTTL {
		s.mu.Unlock()
		return e.u, nil
	}
	s.mu.Unlock()

	var u Unread
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var senders []uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT count(*), COALESCE(array_agg(DISTINCT m.sender_principal), '{}')
			 FROM agent_messages m
			 WHERE `+addressedTo+`
			   AND NOT EXISTS (SELECT 1 FROM agent_message_deliveries d WHERE d.message_id = m.id AND d.client_id = $1)`,
			clientID, principalID).Scan(&u.Count, &senders); err != nil {
			return err
		}
		if u.Count == 0 {
			return nil
		}
		ids := map[uuid.UUID]bool{}
		for _, id := range senders {
			ids[id] = true
		}
		people, err := plans.ResolvePeople(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, id := range senders {
			if p, ok := people[id]; ok && p.Name != "" {
				u.Senders = append(u.Senders, p.Name)
			}
		}
		slices.Sort(u.Senders)
		return nil
	})
	if err != nil {
		return Unread{}, fmt.Errorf("messages: unread: %w", err)
	}
	s.mu.Lock()
	if len(s.unread) >= maxUnreadCache {
		s.unread = map[uuid.UUID]unreadEntry{}
	}
	s.unread[clientID] = unreadEntry{u: u, at: now}
	s.mu.Unlock()
	return u, nil
}

// ---------------------------------------------------------------- sent

// SentPage is a page of the caller's sent messages in a project.
type SentPage struct {
	paging.Page[Message]
	People map[uuid.UUID]plans.Person
}

// Sent lists the messages the caller sent in a project, newest first, with
// which sessions received and acknowledged each. Only the caller's own:
// another member's direct message is not theirs to read (RLS says the same).
func (s *Service) Sent(ctx context.Context, principalID, projectID uuid.UUID, opts paging.Options) (SentPage, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapPlanRead); err != nil {
		return SentPage{}, err
	}
	t, id, ok, err := opts.DecodeTimeUUID()
	if err != nil {
		return SentPage{}, err
	}
	limit := opts.ClampedLimit()
	var out SentPage
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT m.id, m.seq, m.project_id, m.sender_principal, m.sender_client, m.recipient_principal, m.recipient_client,
			        m.ciphertext, m.nonce, m.wrapped_dek, m.project_key_version, m.created_at, m.expires_at
			 FROM agent_messages m
			 WHERE m.project_id = $1 AND m.sender_principal = $2
			   AND (NOT $3::boolean OR (m.created_at, m.id) < ($4, $5))
			 ORDER BY m.created_at DESC, m.id DESC
			 LIMIT $6`,
			projectID, principalID, ok, t, id, limit+1)
		if err != nil {
			return err
		}
		type item struct {
			m Message
			e envelope
		}
		var list []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.m.ID, &it.m.Seq, &it.m.ProjectID, &it.m.Sender, &it.m.SenderClient,
				&it.m.RecipientPrincipal, &it.m.RecipientClient, &it.e.ciphertext, &it.e.nonce, &it.e.wrappedDEK, &it.e.keyVersion,
				&it.m.CreatedAt, &it.m.ExpiresAt); err != nil {
				rows.Close()
				return err
			}
			list = append(list, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		list, more := paging.More(list, limit)
		people := map[uuid.UUID]bool{principalID: true}
		index := map[uuid.UUID]int{}
		ids := make([]uuid.UUID, 0, len(list))
		for _, it := range list {
			if err := s.open(ctx, tx, &it.m, it.e); err != nil {
				return err
			}
			if it.m.RecipientPrincipal != nil {
				people[*it.m.RecipientPrincipal] = true
			}
			index[it.m.ID] = len(out.Items)
			ids = append(ids, it.m.ID)
			out.Items = append(out.Items, it.m)
		}
		if more {
			last := list[len(list)-1].m
			out.NextCursor = paging.EncodeTimeUUID(last.CreatedAt, last.ID)
		}
		if len(ids) > 0 {
			drows, err := tx.Query(ctx,
				`SELECT d.message_id, d.client_id, c.client_type, d.recipient_principal, d.delivered_at, d.acked_at
				 FROM agent_message_deliveries d
				 JOIN client_credentials c ON c.id = d.client_id
				 WHERE d.message_id = ANY($1)
				 ORDER BY d.delivered_at`, ids)
			if err != nil {
				return err
			}
			for drows.Next() {
				var mid uuid.UUID
				var d Delivery
				if err := drows.Scan(&mid, &d.ClientID, &d.ClientType, &d.PrincipalID, &d.DeliveredAt, &d.AckedAt); err != nil {
					drows.Close()
					return err
				}
				people[d.PrincipalID] = true
				out.Items[index[mid]].Deliveries = append(out.Items[index[mid]].Deliveries, d)
			}
			drows.Close()
			if err := drows.Err(); err != nil {
				return err
			}
		}
		out.People, err = plans.ResolvePeople(ctx, tx, people)
		return err
	})
	if err != nil {
		return SentPage{}, fmt.Errorf("messages: sent: %w", err)
	}
	return out, nil
}
