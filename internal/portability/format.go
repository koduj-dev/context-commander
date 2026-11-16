// Package portability exports a project to an archive and imports one into
// a new project (docs/adr/0035). An archive carries the project's content
// in plaintext: artifacts with every version, memories, phases and plans,
// ideas, handoffs and conversation history. It carries no secrets, no keys,
// no members, no audit trail, no events and no usage, and the server never
// keeps a copy.
//
// It owns no storage. Export reads through the domain services, so every
// read is authorized and decrypted by the package that owns the data; import
// writes through the domain packages' import functions, which encrypt under
// the new project's key.
package portability

import (
	"time"
)

// Format names an archive; Version is its layout. A reader refuses an
// archive with a format or version it does not know.
const (
	Format  = "context-commander/project"
	Version = 1
)

// Files inside an archive. Manifest is written last, and an archive without
// one, or with Complete false, is refused: an export that failed half way
// must never import as if it were whole.
const (
	fileManifest  = "manifest.json"
	filePeople    = "people.json"
	fileArtifacts = "artifacts.json"
	fileMemories  = "memories.json"
	filePlans     = "plans.json"
	fileIdeas     = "ideas.json"
	fileHandoffs  = "handoffs.json"
	fileHistory   = "history.json"
	blobDir       = "blobs/"
)

// Excluded is what an archive never carries, stated in its manifest so a
// reader does not have to infer it from what is missing.
var Excluded = []string{"secrets", "skills and policies", "members and invitations", "audit trail", "events", "usage and presence", "messages"}

// Manifest describes an archive.
type Manifest struct {
	Format     string         `json:"format"`
	Version    int            `json:"version"`
	ExportedAt time.Time      `json:"exported_at"`
	Project    ProjectRef     `json:"project"`
	Counts     map[string]int `json:"counts"`
	Excluded   []string       `json:"excluded"`
	Complete   bool           `json:"complete"`
}

// ProjectRef is the project an archive came from.
type ProjectRef struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// Person is a principal an archive's records name, so an import can map it
// to someone in the target installation by email.
type Person struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
}

// Artifact is one artifact and every version of it, oldest first. Content
// lives under blobs/, named by the version's content hash.
type Artifact struct {
	Path        string            `json:"path"`
	ContentType string            `json:"content_type"`
	Versions    []ArtifactVersion `json:"versions"`
}

type ArtifactVersion struct {
	Version     int       `json:"version"`
	ContentHash string    `json:"content_hash"`
	SizeBytes   int64     `json:"size_bytes"`
	Blob        string    `json:"blob"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

type Memory struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Status       string    `json:"status"`
	Title        string    `json:"title"`
	Body         string    `json:"body,omitempty"`
	Branch       string    `json:"branch,omitempty"`
	TicketKey    string    `json:"ticket_key,omitempty"`
	TicketURL    string    `json:"ticket_url,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
	Assignee     string    `json:"assignee,omitempty"`
	PlanID       string    `json:"plan_id,omitempty"`
	PlanItemID   string    `json:"plan_item_id,omitempty"`
	SupersededBy string    `json:"superseded_by,omitempty"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Plans struct {
	Phases []Phase `json:"phases"`
	Plans  []Plan  `json:"plans"`
}

type Phase struct {
	ID        string    `json:"id"`
	Position  int       `json:"position"`
	Status    string    `json:"status"`
	Title     string    `json:"title"`
	Goal      string    `json:"goal,omitempty"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

type Plan struct {
	ID        string    `json:"id"`
	PhaseID   string    `json:"phase_id,omitempty"`
	Status    string    `json:"status"`
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	Items     []Item    `json:"items"`
}

type Item struct {
	ID          string     `json:"id"`
	Position    int        `json:"position"`
	Kind        string     `json:"kind"`
	Status      string     `json:"status"`
	Text        string     `json:"text"`
	Note        string     `json:"note,omitempty"`
	Assignee    string     `json:"assignee,omitempty"`
	UpdatedBy   string     `json:"updated_by,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type Idea struct {
	ID              string     `json:"id"`
	Status          string     `json:"status"`
	Priority        string     `json:"priority"`
	Position        int        `json:"position"`
	Title           string     `json:"title"`
	Body            string     `json:"body,omitempty"`
	Proposal        string     `json:"proposal,omitempty"`
	Rejection       string     `json:"rejection,omitempty"`
	Branch          string     `json:"branch,omitempty"`
	TicketKey       string     `json:"ticket_key,omitempty"`
	TicketURL       string     `json:"ticket_url,omitempty"`
	CreatedBy       string     `json:"created_by"`
	ClaimedBy       string     `json:"claimed_by,omitempty"`
	ProposedAt      *time.Time `json:"proposed_at,omitempty"`
	DecidedBy       string     `json:"decided_by,omitempty"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
	ResultingPlanID string     `json:"resulting_plan_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type Handoff struct {
	ID                  string     `json:"id"`
	Status              string     `json:"status"`
	Summary             string     `json:"summary"`
	CompletedWork       string     `json:"completed_work,omitempty"`
	NextActions         string     `json:"next_actions,omitempty"`
	ReferencedArtifacts []string   `json:"referenced_artifacts,omitempty"` // logical paths
	CreatedBy           string     `json:"created_by"`
	CreatedAt           time.Time  `json:"created_at"`
	ClaimedBy           string     `json:"claimed_by,omitempty"`
	ClaimedAt           *time.Time `json:"claimed_at,omitempty"`
	ClosedBy            string     `json:"closed_by,omitempty"`
	ClosedAt            *time.Time `json:"closed_at,omitempty"`
}

type History struct {
	Conversations []Conversation `json:"conversations"`
}

type Conversation struct {
	ID        string            `json:"id"`
	CreatedBy string            `json:"created_by"`
	CreatedAt time.Time         `json:"created_at"`
	Sessions  []ExternalSession `json:"sessions"`
	Objects   []HistoryObject   `json:"objects"`
	Lineage   []LineageEdge     `json:"lineage,omitempty"`
}

type ExternalSession struct {
	ID                string     `json:"id"`
	Provider          string     `json:"provider"`
	Client            string     `json:"client"`
	ProviderSessionID string     `json:"provider_session_id"`
	ParentID          string     `json:"parent_id,omitempty"`
	Relation          string     `json:"relation"`
	StartedAt         time.Time  `json:"started_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
}

type HistoryObject struct {
	ID                string    `json:"id"`
	ExternalSessionID string    `json:"external_session_id"`
	Type              string    `json:"type"`
	DerivedFrom       string    `json:"derived_from,omitempty"`
	ContentHash       string    `json:"content_hash"`
	Blob              string    `json:"blob"`
	CreatedAt         time.Time `json:"created_at"`
}

type LineageEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}
