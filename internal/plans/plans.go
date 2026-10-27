// Package plans is where a project's work is laid out and followed: phases,
// the plans agents and people make inside them, the items those plans are
// made of, and where every client currently is
// (docs/adr/0022-plans-phases-and-presence.md).
//
// The text is CONTENT PAYLOAD (docs/adr/0011). A plan's own text and all of
// its items' text are one encrypted blob on the plan row, so the operation
// agents do most — changing an item's status — touches a plaintext metadata
// row and re-encrypts nothing.
package plans

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("plans: not found")
	ErrForbidden = errors.New("plans: forbidden")
	// ErrConflict is a plan text rewrite against a version that is no longer
	// current. Only rewriting the plan's own title and text is versioned;
	// item edits and appends are merged on the server (docs/adr/0022).
	ErrConflict = errors.New("plans: version conflict: the plan was changed since your expected_version")
	// ErrInvalid is a request that names a status, kind or size this
	// package does not accept. The wrapped message says which.
	ErrInvalid = errors.New("plans: invalid request")
	// ErrNoClient is a presence report from a caller without a client
	// credential. Presence is per client, so there is nothing to attach it to.
	ErrNoClient = errors.New("plans: presence needs a client credential")
)

// Statuses. Strings rather than an enum type so adapters pass them through
// unchanged; Valid* say which are accepted.
const (
	PhasePlanned = "planned"
	PhaseActive  = "active"
	PhaseDone    = "done"

	PlanDraft     = "draft"
	PlanActive    = "active"
	PlanDone      = "done"
	PlanAbandoned = "abandoned"

	ItemOpen       = "open"
	ItemInProgress = "in_progress"
	ItemDone       = "done"
	ItemBlocked    = "blocked"
	ItemSkipped    = "skipped"

	KindTask  = "task"
	KindCheck = "check"
)

func validPhaseStatus(s string) bool { return s == PhasePlanned || s == PhaseActive || s == PhaseDone }
func validPlanStatus(s string) bool {
	return s == PlanDraft || s == PlanActive || s == PlanDone || s == PlanAbandoned
}
func validItemStatus(s string) bool {
	return s == ItemOpen || s == ItemInProgress || s == ItemDone || s == ItemBlocked || s == ItemSkipped
}
func validKind(s string) bool { return s == KindTask || s == KindCheck }

// Limits. A plan is something a person reads, not a dump; these are far
// above any real plan and exist so one runaway agent cannot write a
// megabyte per tick.
const (
	MaxTitle        = 300
	MaxPlanBody     = 64 * 1024
	MaxItemText     = 4000
	MaxItemNote     = 4000
	MaxItemsPerPlan = 500
	MaxActivity     = 300
	// MaxTokensPerReport caps one report's token deltas, so a client that
	// sends a cumulative count by mistake adds one large number, not a
	// billion every heartbeat.
	MaxTokensPerReport = 50_000_000

	// HistoryPerItem is how many status changes an item keeps.
	HistoryPerItem = 20
	// IdleAfter is when presence stops counting as working: a report later
	// than this after the previous one adds no time, and the interface shows
	// the client as idle.
	IdleAfter = 15 * time.Minute
)

// Person is a principal as the board shows it: a name, never content.
type Person struct {
	PrincipalID uuid.UUID
	Kind        string // "user" or "service"
	Name        string
}

type Phase struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Position  int
	Status    string
	Version   int
	Title     string
	Goal      string
	CreatedBy uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Plan struct {
	ID              uuid.UUID
	ProjectID       uuid.UUID
	PhaseID         *uuid.UUID
	Status          string
	Version         int
	Title           string
	Body            string
	CreatedBy       uuid.UUID
	CreatedByClient *uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Items           []Item
}

type Item struct {
	ID          uuid.UUID
	PlanID      uuid.UUID
	Position    int
	Kind        string
	Status      string
	Text        string
	Note        string
	Assignee    *uuid.UUID
	UpdatedBy   *uuid.UUID
	UpdatedAt   time.Time
	CompletedAt *time.Time
	// Effort is the item's total across everyone who worked on it.
	Effort Effort
}

// Effort is time covered by heartbeats, and tokens as clients reported them.
// Lower bounds both: an idle client adds no time, and a client that cannot
// see its token counts adds none.
type Effort struct {
	Seconds   int64
	TokensIn  int64
	TokensOut int64
}

func (e *Effort) add(o Effort) {
	e.Seconds += o.Seconds
	e.TokensIn += o.TokensIn
	e.TokensOut += o.TokensOut
}

type Transition struct {
	From        string
	To          string
	PrincipalID uuid.UUID
	ClientID    *uuid.UUID
	At          time.Time
}

// Presence is where one client is.
type Presence struct {
	ClientID    uuid.UUID
	ClientType  string
	PrincipalID uuid.UUID
	ProjectID   uuid.UUID
	// ProjectPath is "namespace/slug", filled where presence spans projects.
	ProjectPath string
	PlanID      *uuid.UUID
	ItemID      *uuid.UUID
	// PlanTitle and ItemText are resolved for the reader, so a screen does
	// not have to fetch every plan to say where someone is.
	PlanTitle string
	ItemText  string
	Activity  string
	Since     time.Time
	LastBeat  time.Time
}

// Idle reports whether the client has gone quiet.
func (p Presence) Idle(now time.Time) bool { return now.Sub(p.LastBeat) > IdleAfter }

// NewItem is one item to add.
type NewItem struct {
	Text string
	Kind string // KindTask when empty
}

// ItemUpdate changes an item. Nil fields are left as they are.
type ItemUpdate struct {
	Status *string
	Text   *string
	Note   *string
	// Assignee: set AssigneeSet with Assignee nil to unassign.
	AssigneeSet bool
	Assignee    *uuid.UUID
}

// PlanUpdate changes a plan. Title and Body are a rewrite of the plan's own
// text and need ExpectedVersion; Status and the phase do not.
type PlanUpdate struct {
	Title           *string
	Body            *string
	ExpectedVersion int
	Status          *string
	PhaseSet        bool
	PhaseID         *uuid.UUID
}

type PhaseUpdate struct {
	Title  *string
	Goal   *string
	Status *string
}

// Report is one presence heartbeat. ItemID and PlanID say where the client
// is now (both nil: in the project, on nothing in particular). Token deltas
// are since the client's previous report.
type Report struct {
	PlanID    *uuid.UUID
	ItemID    *uuid.UUID
	Activity  string
	TokensIn  int64
	TokensOut int64
}

// Counts is how many items are in each status.
type Counts struct {
	Open, InProgress, Done, Blocked, Skipped int
}

func (c *Counts) add(status string) {
	switch status {
	case ItemOpen:
		c.Open++
	case ItemInProgress:
		c.InProgress++
	case ItemDone:
		c.Done++
	case ItemBlocked:
		c.Blocked++
	case ItemSkipped:
		c.Skipped++
	}
}

// Total is every item, skipped ones included.
func (c Counts) Total() int { return c.Open + c.InProgress + c.Done + c.Blocked + c.Skipped }

// PhaseSummary is a phase with its plans' progress.
type PhaseSummary struct {
	Phase
	Counts Counts
	Effort Effort
}

// PlanSummary is a plan without its items, with their progress.
type PlanSummary struct {
	ID        uuid.UUID
	PhaseID   *uuid.UUID
	Status    string
	Title     string
	CreatedBy uuid.UUID
	UpdatedAt time.Time
	Counts    Counts
	Effort    Effort
}

// ActiveItem is an item someone is on.
type ActiveItem struct {
	PlanID    uuid.UUID
	PlanTitle string
	Item      Item
}

// Board is the state of a project's work in one read.
type Board struct {
	Phases     []PhaseSummary
	Plans      []PlanSummary
	InProgress []ActiveItem
	Blocked    []ActiveItem
	Presence   []Presence
	People     map[uuid.UUID]Person
}

// SearchHit is a plan or one of its items that matched.
type SearchHit struct {
	PlanID     uuid.UUID
	PlanTitle  string
	PlanStatus string
	ItemID     *uuid.UUID
	Text       string
	Status     string
}
