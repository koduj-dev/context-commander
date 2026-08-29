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
}

// Body is CONTENT PAYLOAD (docs/adr/0011) — marshaled to JSON and encrypted
// as one blob via crypto.Provider, ResourceType "handoff". Referenced
// artifacts/tasks are informational IDs, not enforced foreign keys — the
// cookbook does not specify FK integrity for them, and a handoff outliving
// the artifact/task it names is a normal, expected occurrence.
type Body struct {
	Summary             string      `json:"summary"`
	CompletedWork       string      `json:"completed_work"`
	NextActions         string      `json:"next_actions"`
	ReferencedArtifacts []uuid.UUID `json:"referenced_artifacts,omitempty"`
	ReferencedTasks     []uuid.UUID `json:"referenced_tasks,omitempty"`
}
