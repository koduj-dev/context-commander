// Package contextpack assembles the starting context for an agent joining a
// project — NEXT.md Phase 1's "aggregated context.bootstrap".
//
// It is a READ MODEL, not a source of truth. Every field below is fetched
// through the same domain services (and therefore the same capability
// checks, docs/adr/0014) any adapter would call one at a time; this package
// owns only the composition, the ordering, and the size budget. Nothing
// here writes, and no fact lives here that does not live in a domain
// package first.
//
// Why it exists: before it, an agent's first useful step required knowing
// the internal domain model — resolve the project, list handoffs, guess
// which one is current, get it, list effective skills, turn each binding's
// UUID into content, list artifacts, read the relevant ones, and list
// events from the beginning of time. That is six round trips and several
// judgement calls before the agent has learned anything about the work. The
// pack answers "what do I need to know to continue here?" in one call, and
// the per-domain tools stay available for everything past that first step.
package contextpack

import (
	"time"

	"github.com/google/uuid"
)

// DefaultBudget is how many bytes of content (artifact bodies plus skill
// packages) one pack carries when the caller names no budget of its own.
//
// 256 KiB is chosen against the consumer, not the store: a pack is loaded
// into an agent's context window, where this is roughly 60k tokens of
// text — large enough for a handoff plus the instructions and working files
// around it, small enough that a client with a modest window is not handed
// something it must immediately throw away. Callers that know their own
// window ask for more.
const DefaultBudget = 256 * 1024

// MaxBudget bounds what a caller can ask for. Content is assembled in
// memory, so the budget is also this server's per-request allocation — an
// unbounded one lets a single caller decide how much memory the process
// uses.
const MaxBudget = 8 * 1024 * 1024

// DefaultEventLimit is how many recent events a pack reports. Events are
// metadata-only (docs/adr/0011) and cheap, but they are also the least
// information-dense part of the pack — the cursor matters more than the
// rows, since it is what lets the agent follow along from here.
const DefaultEventLimit = 20

// ConventionalArtifactPrefix is the "convention" half of NEXT.md's
// "selected artifacts by manifest or convention". Until the declarative
// project manifest exists (Phase 2), artifacts under this logical-path
// prefix are what a project says every arriving agent should read.
//
// A prefix, not a fixed filename list: the project decides what belongs in
// it, and adding a second must-read file is a write to the project rather
// than a change to this server.
const ConventionalArtifactPrefix = "context/"

// MaxConventionalArtifacts bounds how many artifacts the convention half of
// the selection contributes. It counts only artifacts under
// ConventionalArtifactPrefix — a project with more must-read files than this
// has outgrown "everything under this prefix is required reading" and wants
// the declarative manifest (Phase 2), not a larger pack. Hitting it is
// reported in BudgetReport.Truncated, never silently absorbed.
const MaxConventionalArtifacts = 100

// Options tunes one assembly. The zero value is the supported default:
// convention-selected artifacts, effective skills, DefaultBudget,
// DefaultEventLimit.
type Options struct {
	// Budget caps the total bytes of artifact and skill content included.
	// <= 0 means DefaultBudget; anything above MaxBudget is clamped.
	Budget int

	// ArtifactPaths replaces the convention entirely when non-empty: the
	// caller names exactly the logical paths it wants. Paths that don't
	// exist are reported in Truncated rather than failing the pack — a
	// client's list of "the files I usually need" outliving one of them is
	// normal, and losing the whole pack over it is not a useful response.
	ArtifactPaths []string

	// SkipSkills / SkipArtifacts drop a whole section for a caller that
	// already has it (an agent resuming inside the same session wants the
	// handoff and the events, not another copy of the skills it loaded an
	// hour ago).
	SkipSkills    bool
	SkipArtifacts bool

	// EventLimit is how many recent events to include. <= 0 means
	// DefaultEventLimit; clamped to paging.MaxLimit.
	EventLimit int
}

// Pack is the assembled starting context.
type Pack struct {
	Project ProjectRef `json:"project"`

	// Role is the caller's project role, and Capabilities the actions it
	// implies. An agent that knows it holds READER will propose a handoff
	// instead of attempting a write and reporting a permission error as if
	// it were a bug.
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`

	// Handoff is the work to continue: the newest handoff still open or
	// claimed, with its body. Nil when the project has none — a legitimate
	// state (nothing has been handed over yet), not an error.
	Handoff *HandoffSection `json:"handoff,omitempty"`

	Artifacts []ArtifactSection `json:"artifacts"`
	Skills    []SkillSection    `json:"skills"`
	Events    EventsSection     `json:"events"`

	Budget BudgetReport `json:"budget"`

	// AssembledAt is when this view was taken. A pack is a snapshot, not a
	// subscription; the timestamp plus Events.NextCursor is what tells a
	// later reader how stale it is and where to catch up from.
	AssembledAt time.Time `json:"assembled_at"`
}

type ProjectRef struct {
	ID   uuid.UUID `json:"id"`
	Path string    `json:"path"`
	Name string    `json:"name"`
}

// HandoffSection is the handoff's metadata and its decrypted body. Status
// is load-bearing for the arriving agent: "open" means take it (claim it
// first), "claimed" means someone else already did and this is context, not
// an assignment.
type HandoffSection struct {
	ID        uuid.UUID  `json:"id"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	ClaimedAt *time.Time `json:"claimed_at,omitempty"`

	// Who holds it. Status alone cannot answer "is this mine?": a claim
	// whose response was lost leaves the claimer seeing "claimed" on its
	// retry with no way to tell itself from a competitor, and two clients
	// can share a principal, so the client id is the half that actually
	// distinguishes them.
	ClaimedByPrincipal  *uuid.UUID  `json:"claimed_by_principal,omitempty"`
	ClaimedByClient     *uuid.UUID  `json:"claimed_by_client,omitempty"`
	Summary             string      `json:"summary"`
	CompletedWork       string      `json:"completed_work"`
	NextActions         string      `json:"next_actions"`
	ReferencedArtifacts []uuid.UUID `json:"referenced_artifacts,omitempty"`
}

type ArtifactSection struct {
	LogicalPath string    `json:"logical_path"`
	Version     int       `json:"version"`
	ContentHash string    `json:"content_hash"`
	SizeBytes   int64     `json:"size_bytes"`
	UpdatedAt   time.Time `json:"updated_at"`
	Content     string    `json:"content"`
}

// SkillSection carries a skill's actual instructions, not just the binding
// UUID that ListEffective returns. Scope and Pinned travel with it because
// "this instruction is mandated installation-wide" and "this project chose
// it" are different things for an agent deciding how to weigh them.
type SkillSection struct {
	BindingID      uuid.UUID `json:"binding_id"`
	SkillID        uuid.UUID `json:"skill_id"`
	SkillVersionID uuid.UUID `json:"skill_version_id"`
	Name           string    `json:"name"`
	Version        int       `json:"version"`
	Scope          string    `json:"scope"`
	TrustStatus    string    `json:"trust_status"`
	Content        string    `json:"content"`
}

// EventsSection is the tail of the project's event stream plus the cursor
// that continues it. The cursor is the point of the section: it is what
// turns a one-shot snapshot into something an agent can stay current with,
// by passing it to event.list (the same paging contract, cookbook §22).
type EventsSection struct {
	Recent []EventRef `json:"recent"`
	// NextCursor continues *forward* from the newest event included here.
	// Empty only when the project has no events at all.
	NextCursor string `json:"next_cursor,omitempty"`
}

type EventRef struct {
	Type         string     `json:"type"`
	ResourceType string     `json:"resource_type"`
	ResourceID   *uuid.UUID `json:"resource_id,omitempty"`
}

// BudgetReport states what the pack cost and, crucially, what it left out.
// NEXT.md asks for "an explicit size budget with a statement of what was
// truncated" — an agent that cannot tell "there is nothing else" from "there
// was more and you didn't get it" will confidently act on a partial picture.
type BudgetReport struct {
	LimitBytes int              `json:"limit_bytes"`
	UsedBytes  int              `json:"used_bytes"`
	Truncated  []TruncationNote `json:"truncated"`
}

// TruncationNote names one thing the pack does not contain and why, in
// terms the caller can act on: every note identifies something fetchable
// through a per-domain tool.
//
// Items are omitted whole, never cut mid-content: half an artifact looks
// exactly like a complete one to a reader, and a skill package truncated
// mid-instruction is worse than an absent one.
type TruncationNote struct {
	Kind      string `json:"kind"`                 // "artifact" | "skill" | "handoff" | "events"
	Ref       string `json:"ref"`                  // logical path, skill name, or section name
	SizeBytes int64  `json:"size_bytes,omitempty"` // what it would have cost, when known
	Reason    string `json:"reason"`
}
