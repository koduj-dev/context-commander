package messages_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/messages"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type member struct {
	principalID uuid.UUID
	agentA      uuid.UUID // a PAT, as an agent holds
	agentB      uuid.UUID // a second PAT of the same principal: another agent session
	sessionID   uuid.UUID // a browser session, as a person signed in holds
}

type notified struct {
	mu       sync.Mutex
	projects []uuid.UUID
}

func (n *notified) Notify(p uuid.UUID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.projects = append(n.projects, p)
}

func (n *notified) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.projects)
}

type fixture struct {
	t         *testing.T
	ctx       context.Context
	ownerPool *db.Pool
	appPool   *db.Pool
	svc       *messages.Service
	notified  *notified
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
	n := &notified{}
	f := &fixture{t: t, ctx: ctx, ownerPool: ownerPool, appPool: appPool, svc: messages.NewService(appPool, cp, authz, n), notified: n}
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

// join adds a user to a project with a role, two agent PATs and a browser
// session.
func (f *fixture) join(projectID uuid.UUID, name string, role projects.Role) member {
	f.t.Helper()
	m := member{principalID: uuid.New(), agentA: uuid.New(), agentB: uuid.New(), sessionID: uuid.New()}
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		user := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`, user, user.String()+"@example.test", name); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, m.principalID, user); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO client_credentials (id, principal_id, client_type, token_hash)
			 VALUES ($1, $2, 'claude-code', 'unused'), ($3, $2, 'claude-code', 'unused'), ($4, $2, 'session', 'unused')`,
			m.agentA, m.principalID, m.agentB, m.sessionID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`, projectID, m.principalID, role)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed member: %v", err)
	}
	return m
}

func (f *fixture) send(from member, n messages.NewMessage) messages.Message {
	f.t.Helper()
	m, err := f.svc.Send(f.ctx, from.principalID, &from.sessionID, f.projectID, n)
	if err != nil {
		f.t.Fatalf("send: %v", err)
	}
	return m
}

func (f *fixture) inbox(principalID, clientID uuid.UUID, opts messages.InboxOptions) messages.InboxPage {
	f.t.Helper()
	page, err := f.svc.Inbox(f.ctx, principalID, &clientID, opts)
	if err != nil {
		f.t.Fatalf("inbox: %v", err)
	}
	return page
}

func bodies(page messages.InboxPage) string {
	var out []string
	for _, m := range page.Items {
		out = append(out, m.Body)
	}
	return strings.Join(out, ",")
}

func (f *fixture) unread(principalID, clientID uuid.UUID) int {
	f.t.Helper()
	u, err := f.svc.Unread(f.ctx, principalID, clientID)
	if err != nil {
		f.t.Fatalf("unread: %v", err)
	}
	return u.Count
}

// Only a person in the interface sends, and only with plan.write.
func TestOnlyAPersonWhoCanWriteSends(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	reader := f.join(f.projectID, "Reader", projects.RoleReader)

	_, err := f.svc.Send(f.ctx, alice.principalID, &alice.agentA, f.projectID, messages.NewMessage{Body: "hi"})
	if !errors.Is(err, messages.ErrNeedsPerson) {
		t.Errorf("send from an agent's PAT: %v, want ErrNeedsPerson", err)
	}
	_, err = f.svc.Send(f.ctx, alice.principalID, nil, f.projectID, messages.NewMessage{Body: "hi"})
	if !errors.Is(err, messages.ErrNeedsPerson) {
		t.Errorf("send with no credential: %v, want ErrNeedsPerson", err)
	}
	_, err = f.svc.Send(f.ctx, reader.principalID, &reader.sessionID, f.projectID, messages.NewMessage{Body: "hi"})
	if !errors.Is(err, messages.ErrForbidden) {
		t.Errorf("send as a reader: %v, want ErrForbidden", err)
	}
	if f.notified.count() != 0 {
		t.Errorf("a refused send notified the board")
	}
	m := f.send(alice, messages.NewMessage{Body: "  look at the cache first  "})
	if m.Body != "look at the cache first" || m.Audience() != messages.AudienceProject {
		t.Errorf("sent %+v", m)
	}
	if f.notified.count() != 1 {
		t.Errorf("a send notified %d times, want 1", f.notified.count())
	}

	var audited int
	if err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `SELECT count(*) FROM audit_log WHERE action = 'message.send' AND resource_id = $1`, m.ID).Scan(&audited)
	}); err != nil {
		t.Fatal(err)
	}
	if audited != 1 {
		t.Errorf("message.send audited %d times, want 1", audited)
	}
}

func TestSendValidatesBodyRecipientAndExpiry(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	bob := f.join(f.projectID, "Bob", projects.RoleContributor)
	outsider := f.join(f.newProject("other"), "Outsider", projects.RoleContributor)

	cases := map[string]messages.NewMessage{
		"empty body":                   {Body: "   "},
		"body over the limit":          {Body: strings.Repeat("x", messages.MaxBody+1)},
		"expiry too short":             {Body: "x", TTL: time.Second},
		"expiry too long":              {Body: "x", TTL: messages.MaxTTL + time.Hour},
		"a browser session":            {Body: "x", RecipientClient: &bob.sessionID},
		"a non-member principal":       {Body: "x", RecipientPrincipal: &outsider.principalID},
		"a non-member's session":       {Body: "x", RecipientClient: &outsider.agentA},
		"a session of someone else":    {Body: "x", RecipientClient: &bob.agentA, RecipientPrincipal: &alice.principalID},
		"a credential that is not one": {Body: "x", RecipientClient: new(uuid.UUID)},
	}
	for name, n := range cases {
		if _, err := f.svc.Send(f.ctx, alice.principalID, &alice.sessionID, f.projectID, n); !errors.Is(err, messages.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	m := f.send(alice, messages.NewMessage{Body: strings.Repeat("x", messages.MaxBody), RecipientClient: &bob.agentA})
	if m.RecipientPrincipal == nil || *m.RecipientPrincipal != bob.principalID {
		t.Errorf("a session message's principal is %v, want Bob", m.RecipientPrincipal)
	}
	if got := m.ExpiresAt.Sub(m.CreatedAt); got != messages.DefaultTTL {
		t.Errorf("default expiry %s, want %s", got, messages.DefaultTTL)
	}
}

// A message to one session reaches that session and no other — not even
// another session of the same principal.
func TestASessionMessageReachesThatSessionOnly(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	bob := f.join(f.projectID, "Bob", projects.RoleContributor)

	f.send(alice, messages.NewMessage{Body: "stop and run the tests", RecipientClient: &bob.agentA})

	if got := f.unread(bob.principalID, bob.agentA); got != 1 {
		t.Errorf("Bob's addressed session has %d unread, want 1", got)
	}
	if got := bodies(f.inbox(bob.principalID, bob.agentB, messages.InboxOptions{})); got != "" {
		t.Errorf("Bob's other session read %q", got)
	}
	if got := bodies(f.inbox(alice.principalID, alice.agentA, messages.InboxOptions{})); got != "" {
		t.Errorf("Alice's agent read %q", got)
	}

	page := f.inbox(bob.principalID, bob.agentA, messages.InboxOptions{})
	if bodies(page) != "stop and run the tests" || page.People[alice.principalID].Name != "Alice" {
		t.Fatalf("Bob's session read %q, people %v", bodies(page), page.People)
	}
	// Delivered: the notice stops, but the message is offered again until
	// acknowledged.
	if got := f.unread(bob.principalID, bob.agentA); got != 0 {
		t.Errorf("after delivery %d unread, want 0", got)
	}
	again := f.inbox(bob.principalID, bob.agentA, messages.InboxOptions{})
	if len(again.Items) != 1 || len(again.Items[0].Deliveries) != 1 {
		t.Fatalf("an unacknowledged message was not offered again as redelivered: %+v", again.Items)
	}
	n, err := f.svc.Ack(f.ctx, bob.principalID, &bob.agentA, []uuid.UUID{again.Items[0].ID})
	if err != nil || n != 1 {
		t.Fatalf("ack: %d, %v", n, err)
	}
	if n, _ := f.svc.Ack(f.ctx, bob.principalID, &bob.agentA, []uuid.UUID{again.Items[0].ID}); n != 0 {
		t.Errorf("a second ack acknowledged %d", n)
	}
	if got := bodies(f.inbox(bob.principalID, bob.agentA, messages.InboxOptions{})); got != "" {
		t.Errorf("after ack the inbox still holds %q", got)
	}
	// Another session cannot acknowledge what was not its own.
	if n, _ := f.svc.Ack(f.ctx, bob.principalID, &bob.agentB, []uuid.UUID{again.Items[0].ID}); n != 0 {
		t.Errorf("another session acknowledged %d of someone else's", n)
	}

	sent, err := f.svc.Sent(f.ctx, alice.principalID, f.projectID, paging.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sent.Items) != 1 || len(sent.Items[0].Deliveries) != 1 {
		t.Fatalf("sent %+v", sent.Items)
	}
	d := sent.Items[0].Deliveries[0]
	if d.ClientID != bob.agentA || d.AckedAt == nil || d.ClientType != "claude-code" || sent.People[bob.principalID].Name != "Bob" {
		t.Errorf("delivery %+v, people %v", d, sent.People)
	}
}

// A broadcast reaches every session working on the project, each exactly
// once, and not the browser that sent it.
func TestABroadcastReachesEachSessionOnce(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	bob := f.join(f.projectID, "Bob", projects.RoleContributor)
	reader := f.join(f.projectID, "Reader", projects.RoleReader)

	f.send(alice, messages.NewMessage{Body: "freeze the schema"})

	if got := bodies(f.inbox(alice.principalID, alice.sessionID, messages.InboxOptions{})); got != "" {
		t.Errorf("the sending browser read its own broadcast: %q", got)
	}
	for name, c := range map[string][2]uuid.UUID{
		"alice's agent": {alice.principalID, alice.agentA},
		"bob's agent A": {bob.principalID, bob.agentA},
		"bob's agent B": {bob.principalID, bob.agentB},
		"the reader":    {reader.principalID, reader.agentA},
	} {
		page := f.inbox(c[0], c[1], messages.InboxOptions{Ack: true})
		if bodies(page) != "freeze the schema" {
			t.Errorf("%s read %q", name, bodies(page))
		}
		if got := bodies(f.inbox(c[0], c[1], messages.InboxOptions{})); got != "" {
			t.Errorf("%s read the broadcast a second time: %q", name, got)
		}
	}
	sent, err := f.svc.Sent(f.ctx, alice.principalID, f.projectID, paging.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(sent.Items[0].Deliveries); got != 4 {
		t.Errorf("the broadcast has %d deliveries, want 4", got)
	}
}

// A message to a principal reaches each of its sessions and nobody else.
func TestAPrincipalMessageReachesEachOfItsSessions(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	bob := f.join(f.projectID, "Bob", projects.RoleContributor)

	f.send(alice, messages.NewMessage{Body: "for all of Bob's agents", RecipientPrincipal: &bob.principalID})
	for _, c := range []uuid.UUID{bob.agentA, bob.agentB} {
		if got := bodies(f.inbox(bob.principalID, c, messages.InboxOptions{})); got != "for all of Bob's agents" {
			t.Errorf("Bob's session read %q", got)
		}
	}
	if got := bodies(f.inbox(alice.principalID, alice.agentA, messages.InboxOptions{})); got != "" {
		t.Errorf("Alice's agent read %q", got)
	}
}

// RLS, as cc_app: a member of another project reads nothing, and a member
// of the same project cannot read a direct message meant for someone else.
func TestRLSHidesMessagesFromThoseTheyAreNotFor(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	bob := f.join(f.projectID, "Bob", projects.RoleContributor)
	carol := f.join(f.projectID, "Carol", projects.RoleContributor)
	outsider := f.join(f.newProject("other"), "Outsider", projects.RoleOwner)

	direct := f.send(alice, messages.NewMessage{Body: "for Bob", RecipientClient: &bob.agentA})
	broadcast := f.send(alice, messages.NewMessage{Body: "for everyone"})
	if _, err := f.svc.Inbox(f.ctx, bob.principalID, &bob.agentA, messages.InboxOptions{}); err != nil {
		t.Fatal(err)
	}

	visible := func(principalID uuid.UUID) (msgs, deliveries int) {
		t.Helper()
		err := db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			if err := tx.QueryRow(f.ctx, `SELECT count(*) FROM agent_messages WHERE id = ANY($1)`, []uuid.UUID{direct.ID, broadcast.ID}).Scan(&msgs); err != nil {
				return err
			}
			return tx.QueryRow(f.ctx, `SELECT count(*) FROM agent_message_deliveries`).Scan(&deliveries)
		})
		if err != nil {
			t.Fatal(err)
		}
		return msgs, deliveries
	}
	if m, d := visible(outsider.principalID); m != 0 || d != 0 {
		t.Errorf("another project's owner sees %d messages and %d deliveries", m, d)
	}
	if m, d := visible(carol.principalID); m != 1 || d != 0 {
		t.Errorf("a colleague sees %d messages (want only the broadcast) and %d deliveries", m, d)
	}
	if m, d := visible(alice.principalID); m != 2 || d != 2 {
		t.Errorf("the sender sees %d messages and %d deliveries, want 2 and 2 (Bob read both)", m, d)
	}
	if got := bodies(f.inbox(outsider.principalID, outsider.agentA, messages.InboxOptions{})); got != "" {
		t.Errorf("an outsider's inbox read %q", got)
	}
	if _, err := f.svc.Sent(f.ctx, outsider.principalID, f.projectID, paging.Options{}); !errors.Is(err, messages.ErrForbidden) {
		t.Errorf("an outsider listing sent messages: %v, want ErrForbidden", err)
	}
	if _, err := f.svc.Inbox(f.ctx, outsider.principalID, &outsider.agentA, messages.InboxOptions{ProjectID: &f.projectID}); !errors.Is(err, messages.ErrForbidden) {
		t.Errorf("an outsider's inbox in the project: %v, want ErrForbidden", err)
	}
	// Nobody inserts a message in another's name, even bypassing the service.
	err := db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: carol.principalID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`INSERT INTO agent_messages (project_id, sender_principal, ciphertext, nonce, wrapped_dek, project_key_version, expires_at)
			 SELECT project_id, $1, ciphertext, nonce, wrapped_dek, project_key_version, expires_at FROM agent_messages WHERE id = $2`,
			alice.principalID, broadcast.ID)
		return err
	})
	if err == nil {
		t.Error("a member inserted a message as someone else")
	}
}

// The inbox pages forward by cursor, oldest first.
func TestTheInboxPagesByCursor(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	bob := f.join(f.projectID, "Bob", projects.RoleContributor)
	for _, b := range []string{"1", "2", "3", "4", "5"} {
		f.send(alice, messages.NewMessage{Body: b, RecipientClient: &bob.agentA})
	}
	var got []string
	cursor := ""
	for range 5 {
		page := f.inbox(bob.principalID, bob.agentA, messages.InboxOptions{Options: paging.Options{Limit: 2, Cursor: cursor}})
		got = append(got, bodies(page))
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if strings.Join(got, "|") != "1,2|3,4|5" {
		t.Errorf("pages %q", got)
	}
	if _, err := f.svc.Inbox(f.ctx, bob.principalID, &bob.agentA, messages.InboxOptions{Options: paging.Options{Cursor: "garbage"}}); !errors.Is(err, paging.ErrInvalidCursor) {
		t.Errorf("a bad cursor: %v", err)
	}
	if _, err := f.svc.Inbox(f.ctx, bob.principalID, nil, messages.InboxOptions{}); !errors.Is(err, messages.ErrNoClient) {
		t.Errorf("no client: %v", err)
	}
}

// An expired message is no longer offered, and no longer counted.
func TestAnExpiredMessageIsNotOffered(t *testing.T) {
	f := newFixture(t)
	alice := f.join(f.projectID, "Alice", projects.RoleContributor)
	bob := f.join(f.projectID, "Bob", projects.RoleContributor)
	m := f.send(alice, messages.NewMessage{Body: "stale", RecipientClient: &bob.agentA})
	if err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `UPDATE agent_messages SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE id = $1`, m.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.unread(bob.principalID, bob.agentA); got != 0 {
		t.Errorf("an expired message counts as unread: %d", got)
	}
	if got := bodies(f.inbox(bob.principalID, bob.agentA, messages.InboxOptions{})); got != "" {
		t.Errorf("an expired message was offered: %q", got)
	}
}
