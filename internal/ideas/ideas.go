// Package ideas is the project's idea stack (docs/adr/0026-idea-stack.md):
// tasks and thoughts people and agents drop in, which a planning agent takes
// one at a time, analyses in the project's context and answers with a
// proposed solution, and which a person then accepts or rejects.
//
// Context Commander runs no agent. The stack is durable state an agent
// outside the server pulls from; the claim is what keeps two planning agents
// from taking the same idea, and its lease is what keeps a crashed one from
// holding it forever.
//
// Title, body, proposal and rejection reason are CONTENT PAYLOAD
// (docs/adr/0011), one encrypted blob per idea. Everything the queue is
// ordered and counted by is plaintext metadata.
package ideas

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("ideas: not found")
	ErrForbidden = errors.New("ideas: forbidden")
	// ErrInvalid is a request with a missing title, an unknown status or
	// priority, or a field over its bound. The wrapped message says which.
	ErrInvalid = errors.New("ideas: invalid request")
	// ErrInvalidTransition is an operation the idea's status does not allow
	// — proposing on an idea nobody claimed, accepting one that was never
	// proposed. The caller's view is stale; re-reading the idea is the fix.
	ErrInvalidTransition = errors.New("ideas: invalid status transition")
	// ErrAlreadyClaimed is the expected collision between two planning
	// agents, named so the loser can tell it from a bug: another client
	// holds a live claim on this idea.
	ErrAlreadyClaimed = errors.New("ideas: already claimed by another client")
	// ErrNotClaimant is a renew, release or propose from a client that does
	// not hold the claim — typically because its lease lapsed and another
	// agent took the idea since.
	ErrNotClaimant = errors.New("ideas: this client does not hold the idea's claim")
	// ErrNoClient is a claim from a caller without a client credential. A
	// claim belongs to a client, as a handoff claim does.
	ErrNoClient = errors.New("ideas: claiming needs a client credential")
	// ErrNeedsPerson is an accept or reject that did not come from a
	// person signed in to the interface. Deciding on a proposal is a human
	// decision, and a token an agent holds is not a human (docs/adr/0026).
	ErrNeedsPerson = errors.New("ideas: accepting or rejecting a proposal is a person's decision; sign in to the interface to make it")
)

// Statuses. Strings rather than an enum type so adapters pass them through
// unchanged.
//
//	open ──claim──▶ claimed ──propose──▶ proposed ──accept──▶ accepted
//	  ▲               │  ▲                  │
//	  └───release─────┘  └ lease lapses:    ├──reject──▶ rejected
//	  ▲                    claimable again  └──reject, reopen──▶ open
//	  └──────────────────────────────────────────────────────────┘
//	open | claimed | proposed ──withdraw──▶ withdrawn
const (
	StatusOpen      = "open"
	StatusClaimed   = "claimed"
	StatusProposed  = "proposed"
	StatusAccepted  = "accepted"
	StatusRejected  = "rejected"
	StatusWithdrawn = "withdrawn"
)

// Statuses lists every status, in lifecycle order.
var Statuses = []string{StatusOpen, StatusClaimed, StatusProposed, StatusAccepted, StatusRejected, StatusWithdrawn}

func validStatus(s string) bool {
	for _, v := range Statuses {
		if v == s {
			return true
		}
	}
	return false
}

// Priorities, stored as 0..2 so the queue orders by them in SQL.
const (
	PriorityLow    = "low"
	PriorityNormal = "normal"
	PriorityHigh   = "high"
)

func priorityValue(p string) (int, bool) {
	switch p {
	case PriorityLow:
		return 0, true
	case PriorityNormal, "":
		return 1, true
	case PriorityHigh:
		return 2, true
	}
	return 0, false
}

func priorityName(v int) string {
	switch v {
	case 0:
		return PriorityLow
	case 2:
		return PriorityHigh
	}
	return PriorityNormal
}

// Limits. An idea is a thought, and a proposal a page or two of analysis;
// these sit far above either and exist so a runaway agent cannot write a
// megabyte per call.
const (
	MaxTitle     = 300
	MaxBody      = 16 * 1024
	MaxProposal  = 64 * 1024
	MaxRejection = 4000
	MaxBranch    = 255
	MaxTicketKey = 64
	MaxTicketURL = 2048

	// ClaimLease is how long a claim holds without a renewal. Two hours is
	// longer than a planning agent needs to read the context and write a
	// proposal, and short enough that an idea held by an agent that crashed
	// at lunch is back in the queue by the afternoon.
	ClaimLease = 2 * time.Hour
)

// Idea is one entry on the stack, metadata and content together.
type Idea struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Status    string
	Priority  string
	// Position orders open ideas of the same priority; lower is sooner.
	Position int
	Version  int

	// Content payload, decrypted.
	Title     string
	Body      string
	Proposal  string
	Rejection string

	Branch    string
	TicketKey string
	TicketURL string

	CreatedBy       uuid.UUID
	CreatedByClient *uuid.UUID
	// ClaimedBy* say who took the idea to analyse; they survive the
	// proposal, so a decided idea still records which agent proposed.
	ClaimedBy       *uuid.UUID
	ClaimedByClient *uuid.UUID
	ClaimedAt       *time.Time
	// ClaimExpiresAt is set only while the idea is claimed.
	ClaimExpiresAt  *time.Time
	ProposedAt      *time.Time
	DecidedBy       *uuid.UUID
	DecidedAt       *time.Time
	ResultingPlanID *uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Lapsed reports whether the idea is claimed but its lease has run out: it
// still shows who held it, and the next claim may take it.
func (i Idea) Lapsed(now time.Time) bool {
	return i.Status == StatusClaimed && i.ClaimExpiresAt != nil && !now.Before(*i.ClaimExpiresAt)
}

// HeldBy reports whether clientID holds the idea's claim, lapsed or not.
func (i Idea) HeldBy(clientID *uuid.UUID) bool {
	return clientID != nil && i.ClaimedByClient != nil && *i.ClaimedByClient == *clientID
}

// NewIdea is what Create takes.
type NewIdea struct {
	Title     string
	Body      string
	Priority  string
	Branch    string
	TicketKey string
	TicketURL string
}

// DraftPlan is the plan a proposal may come with. It is created as a draft
// plan (internal/plans) and becomes active when the proposal is accepted.
type DraftPlan struct {
	PhaseID *uuid.UUID
	Title   string
	Body    string
	Items   []PlanItem
}

// PlanItem is one step of a DraftPlan: kind is task (default) or check.
type PlanItem struct {
	Text string
	Kind string
}

// Update reorders or reprioritises an open idea. Nil leaves a field alone.
type Update struct {
	Priority *string
	// Position is a 1-based place among open ideas of the same priority:
	// 1 is first, past the end is last (as plans reorder, docs/adr/0022).
	Position *int
}

// Counts is how many ideas are waiting at each live stage. Lapsed is the
// part of Claimed whose lease ran out, which claim_next will hand out again.
type Counts struct {
	Open     int
	Claimed  int
	Lapsed   int
	Proposed int
}
