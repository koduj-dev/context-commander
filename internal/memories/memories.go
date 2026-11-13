// Package memories is what agents and people learned about a project and
// want whoever comes next to know: decisions, findings, gotchas and status
// notes, each tagged with where the work happened — the git branch, the
// ticket in the team's tracker, who solved it (docs/adr/0025-memories.md).
//
// The title and body are CONTENT PAYLOAD (docs/adr/0011), encrypted on the
// row. The metadata a team filters by is OPERATIONAL METADATA and stays
// plaintext, which is what lets "everything on branch X about ticket Y" be
// answered by SQL without decrypting the project. Text search decrypts, like
// plan search, and goes through a Ranker so a later semantic ranker can
// replace the substring match without touching the callers.
package memories

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("memories: not found")
	ErrForbidden = errors.New("memories: forbidden")
	// ErrConflict is a text rewrite against a version that is no longer
	// current: someone else edited the memory since it was read.
	ErrConflict = errors.New("memories: version conflict: the memory was changed since your expected_version")
	// ErrInvalid is a request with a kind, status, size or metadata value
	// this package does not accept. The wrapped message says which.
	ErrInvalid = errors.New("memories: invalid request")
	// ErrSemanticOff is a semantic search or a related read on a server
	// with no embedding provider configured. It wraps ErrInvalid, so every
	// adapter already answers it as a bad request.
	ErrSemanticOff = fmt.Errorf("%w: semantic search is not configured on this server (an operator sets --embeddings-provider); use mode text", ErrInvalid)
)

// Kinds. Strings rather than an enum type so adapters pass them through
// unchanged; the migration's CHECK constraint holds the same list.
const (
	KindDecision = "decision"
	KindFinding  = "finding"
	KindGotcha   = "gotcha"
	KindNote     = "note"
	KindStatus   = "status"
	KindOther    = "other"
)

// Kinds lists every kind, in the order a screen offers them.
var Kinds = []string{KindDecision, KindFinding, KindGotcha, KindNote, KindStatus, KindOther}

// Statuses. Active is what an arriving agent should believe; superseded
// points at the memory that replaced it; archived is kept but no longer
// current.
const (
	StatusActive     = "active"
	StatusSuperseded = "superseded"
	StatusArchived   = "archived"
	// StatusAny is a list or search filter, never a stored status: every
	// status. The empty filter means active only, because a superseded
	// decision handed to an agent as if it stood is worse than no answer.
	StatusAny = "any"
)

func validKind(s string) bool {
	for _, k := range Kinds {
		if s == k {
			return true
		}
	}
	return false
}

func validStatus(s string) bool {
	return s == StatusActive || s == StatusSuperseded || s == StatusArchived
}

// Limits. A memory is something a person reads in one sitting; these are far
// above a real one and exist so one runaway agent cannot write a megabyte a
// call.
const (
	MaxTitle     = 300
	MaxBody      = 32 * 1024
	MaxBranch    = 255
	MaxTicketKey = 64
	MaxTicketURL = 2048
	MaxTags      = 20
	MaxTag       = 50
)

// Memory is one memory, decrypted.
type Memory struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Kind      string
	Status    string
	Version   int
	Title     string
	Body      string
	// Branch, TicketKey and TicketURL are "" when not given.
	Branch    string
	TicketKey string
	TicketURL string
	Tags      []string
	// Assignee is who solved or owns what the memory is about, which is not
	// necessarily who wrote it down: an agent records what its user did.
	Assignee        *uuid.UUID
	PlanID          *uuid.UUID
	PlanItemID      *uuid.UUID
	SupersededBy    *uuid.UUID
	CreatedBy       uuid.UUID
	CreatedByClient *uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewMemory is what Create takes. Kind defaults to note.
type NewMemory struct {
	Kind       string
	Title      string
	Body       string
	Branch     string
	TicketKey  string
	TicketURL  string
	Tags       []string
	Assignee   *uuid.UUID
	PlanID     *uuid.UUID
	PlanItemID *uuid.UUID
}

// Update changes a memory. Nil fields are left as they are. Title and Body
// are a rewrite of the encrypted text and need ExpectedVersion; so does
// every metadata change, because metadata and text are one memory and a
// writer acting on a stale read should hear about it either way.
type Update struct {
	ExpectedVersion int
	Kind            *string
	Title           *string
	Body            *string
	// Branch, TicketKey and TicketURL: a pointer to "" clears the field.
	Branch    *string
	TicketKey *string
	TicketURL *string
	Tags      *[]string
	// AssigneeSet with Assignee nil clears the assignee.
	AssigneeSet bool
	Assignee    *uuid.UUID
}

// Filter narrows a list or a search by metadata. The zero value is every
// active memory in the project.
type Filter struct {
	Kind      string
	Branch    string
	TicketKey string
	Tag       string
	Assignee  *uuid.UUID
	Author    *uuid.UUID
	// Status is a stored status, StatusAny, or "" for active only.
	Status        string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
}

// Person is a principal as a screen shows it: a name, never content.
type Person struct {
	PrincipalID uuid.UUID
	Kind        string // "user" or "service"
	Name        string
}

// Match says where a search hit its query.
const (
	MatchTitle = "title"
	MatchBody  = "body"
	// MatchSemantic is a hit found by meaning, not by the words of the
	// query (docs/adr/0028).
	MatchSemantic = "semantic"
)

// Search modes (docs/adr/0028). Text is the substring match; semantic ranks
// by embedding similarity; hybrid fuses both rankings. The empty mode is
// the service's default: hybrid when semantic search is configured, text
// otherwise.
const (
	ModeText     = "text"
	ModeSemantic = "semantic"
	ModeHybrid   = "hybrid"
)

// Hit is one memory a search found. Body is left out of a hit — a hundred
// bodies is a lot to hand an agent that wanted one — and Snippet carries the
// part around the match instead; memory.get has the rest.
type Hit struct {
	Memory  Memory
	Match   string
	Snippet string
	// Score is the cosine similarity for a semantic hit and the fused rank
	// score for a hybrid one; zero for a text hit, whose order is its rank.
	Score float64
}

// SearchResult is a search's answer. Projects names the projects the hits
// came from when the search spanned several; Truncated says a bound cut the
// answer, so a caller can tell "nothing else" from "narrow it down".
type SearchResult struct {
	Hits      []Hit
	Projects  []ProjectRef
	People    map[uuid.UUID]Person
	Truncated bool
	// Mode is the mode that answered, after the default was applied.
	Mode string
	// Degraded says a hybrid search answered by text alone because the
	// embedding provider failed: the hits are right, but nothing was found
	// by meaning.
	Degraded bool
}

// ProjectRef names a project a hit came from.
type ProjectRef struct {
	ID   uuid.UUID
	Path string // "namespace/slug"
	Name string
}
