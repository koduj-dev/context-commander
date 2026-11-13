// Package messages is the agent inbox: a person on a project sends an agent
// something while it works, and the agent picks it up at its next tool call
// or client hook (docs/adr/0032-agent-inbox.md).
//
// MCP is pull-based, so nothing is pushed to an agent. A message waits in
// agent_messages until a session asks for its inbox; every MCP tool result
// carries a one-line notice while something is waiting, and a Claude Code
// hook can poll the inbox after each tool call and hand the text straight to
// the model.
//
// The body is CONTENT PAYLOAD (docs/adr/0011), encrypted with the project
// key. Who sent it, who it is for, and when it was delivered and
// acknowledged are OPERATIONAL METADATA.
package messages

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("messages: not found")
	ErrForbidden = errors.New("messages: forbidden")
	// ErrInvalid is a request with a body, recipient or expiry this package
	// does not accept. The wrapped message says which.
	ErrInvalid = errors.New("messages: invalid request")
	// ErrNeedsPerson is a send from anything but a person signed in to the
	// interface. Messages are input from people to agents, and an agent
	// would read one as its user speaking; a token that can send could
	// make one agent instruct another in a person's name.
	ErrNeedsPerson = errors.New("messages: only a person signed in to the interface can send a message to an agent")
	// ErrNoClient is an inbox read without a client credential: the inbox
	// is per session, and there is no session to read it for.
	ErrNoClient = errors.New("messages: the inbox is per client credential, and this request has none")
)

// Limits.
const (
	// MaxBody is the body's size in bytes. A message is a thought, not a
	// document; a longer text belongs in an artifact the message points to.
	MaxBody = 8 * 1024
	// DefaultTTL is how long a message is offered when the sender names no
	// expiry. A thought sent mid-work is about that work; a day later it is
	// more likely stale than useful to an agent that has just started.
	DefaultTTL = 24 * time.Hour
	MinTTL     = time.Minute
	MaxTTL     = 30 * 24 * time.Hour
	// DefaultInboxLimit and MaxInboxLimit bound one inbox read. An agent
	// reads its inbox into its context, so a page is small.
	DefaultInboxLimit = 20
	MaxInboxLimit     = 100
	// UnreadCacheTTL is how long a session's unread count is reused. The
	// notice rides on every MCP tool call, and an agent can make several a
	// second; a few seconds' delay before it learns of a message costs
	// nothing a person would notice.
	UnreadCacheTTL = 5 * time.Second
)

// Audience says who a message is for.
const (
	// AudienceSession is one session: one client credential.
	AudienceSession = "session"
	// AudiencePrincipal is every session of one principal in the project.
	AudiencePrincipal = "principal"
	// AudienceProject is everyone working on the project.
	AudienceProject = "project"
)

// Message is one message, decrypted.
type Message struct {
	ID          uuid.UUID
	Seq         int64
	ProjectID   uuid.UUID
	ProjectPath string // "namespace/slug"
	Sender      uuid.UUID
	// SenderClient is nil once the sender's credential is gone.
	SenderClient *uuid.UUID
	// RecipientPrincipal is nil for a message to everyone on the project.
	// RecipientClient is set for a message to one session, and then
	// RecipientPrincipal is that session's principal.
	RecipientPrincipal *uuid.UUID
	RecipientClient    *uuid.UUID
	Body               string
	CreatedAt          time.Time
	ExpiresAt          time.Time
	// Deliveries are the sessions that received the message, with when.
	// Filled for the sender's list; an inbox read fills only its own.
	Deliveries []Delivery
}

// Audience is AudienceSession, AudiencePrincipal or AudienceProject.
func (m Message) Audience() string {
	switch {
	case m.RecipientClient != nil:
		return AudienceSession
	case m.RecipientPrincipal != nil:
		return AudiencePrincipal
	}
	return AudienceProject
}

// Delivery is one session's receipt of a message.
type Delivery struct {
	ClientID    uuid.UUID
	ClientType  string
	PrincipalID uuid.UUID
	DeliveredAt time.Time
	// AckedAt is when the session said it read the message; nil until then.
	AckedAt *time.Time
}

// NewMessage is what Send takes. Leave both recipients nil to address
// everyone working on the project. RecipientClient alone is enough for one
// session: its principal is looked up.
type NewMessage struct {
	Body               string
	RecipientPrincipal *uuid.UUID
	RecipientClient    *uuid.UUID
	// TTL is how long the message is offered; zero is DefaultTTL.
	TTL time.Duration
}

// Unread is what the notice on an MCP tool result says: how many messages
// are waiting for the session, and who sent them.
type Unread struct {
	Count   int
	Senders []string
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}
