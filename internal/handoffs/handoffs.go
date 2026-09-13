// Package handoffs defines Handoff — structured hand-off state between
// agents, see docs/architecture-cookbook.md section 9: "Agent A can leave
// enough structured state for Agent B to continue without sharing
// vendor-specific conversation history."
package handoffs

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("handoffs: not found")
	ErrForbidden = errors.New("handoffs: forbidden")

	// ErrInvalidTransition is returned by Service.Claim/Close when the
	// handoff is not in a status the requested transition starts from —
	// claiming a closed handoff, or closing one twice. A client mistake
	// about state, not a server fault, so adapters map it to 409 rather
	// than the default 500: the caller's view of the lifecycle is stale
	// and re-reading the handoff is the fix.
	ErrInvalidTransition = errors.New("handoffs: invalid status transition")

	// ErrAlreadyClaimed is the one transition failure worth distinguishing
	// by name. Two agents racing to continue the same work is the expected
	// collision in a cross-client handoff, and the loser needs to be told
	// specifically that someone else took it — not the generic "wrong
	// status", which reads as a bug in the caller rather than as the
	// coordination outcome it is.
	ErrAlreadyClaimed = errors.New("handoffs: already claimed by another client")
)

// The handoff lifecycle: created open, claimed by whichever client takes
// responsibility for continuing the work, closed when that work is done.
// StatusClaimed is deliberately not skippable — "someone is already on
// this" and "this is finished" are different facts, and an agent deciding
// whether to pick work up needs to tell them apart (migrations/000018).
const (
	StatusOpen    = "open"
	StatusClaimed = "claimed"
	StatusClosed  = "closed"
)

// Handoff is the plaintext-safe half of a handoff record — everything a
// caller can see without decrypting Body (docs/adr/0011: status/id/
// timestamps are INDEXABLE/OPERATIONAL METADATA, safe to stay plaintext).
type Handoff struct {
	ID                 uuid.UUID
	ProjectID          uuid.UUID
	CreatedByPrincipal uuid.UUID
	CreatedByClient    *uuid.UUID
	SourceSessionID    *uuid.UUID
	Status             string
	CreatedAt          time.Time

	// Lifecycle attribution — who moved this handoff out of 'open' and
	// when. All nil while Status is StatusOpen; Claimed* is set from
	// StatusClaimed onwards and deliberately survives the close, so a
	// closed handoff still records which client actually did the work.
	ClaimedAt          *time.Time
	ClaimedByPrincipal *uuid.UUID
	ClaimedByClient    *uuid.UUID
	ClosedAt           *time.Time
	ClosedByPrincipal  *uuid.UUID
	ClosedByClient     *uuid.UUID
}

// Body is CONTENT PAYLOAD (docs/adr/0011) — marshaled to JSON and encrypted
// as one blob via crypto.Provider, ResourceType "handoff". Referenced
// artifacts are informational IDs, not enforced foreign keys — the cookbook
// does not specify FK integrity for them, and a handoff outliving the
// artifact it names is a normal, expected occurrence.
//
// There is deliberately no ReferencedTasks: v1 has no Task domain, and the
// field was a placeholder from an older draft that had leaked into the
// domain, both adapters and the context pack without anything ever
// populating it. Removed before the API stabilised rather than carried
// forever. Rows written before the removal keep the key in their encrypted
// JSON; encoding/json drops unknown fields on read, so no migration is
// needed and no data is lost that anyone was using.
type Body struct {
	Summary             string      `json:"summary"`
	CompletedWork       string      `json:"completed_work"`
	NextActions         string      `json:"next_actions"`
	ReferencedArtifacts []uuid.UUID `json:"referenced_artifacts,omitempty"`
}
