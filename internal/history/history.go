// Package history defines Conversation, ExternalSession, and HistoryObject
// lineage — provider session IDs are opaque metadata, never canonical
// identity, see docs/architecture-cookbook.md §10.
package history

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("history: not found")
	ErrForbidden = errors.New("history: forbidden")
)

// Conversation is Context Commander's own stable identity for a logical
// thread of work — never a provider session ID (§10.1).
type Conversation struct {
	ID                 uuid.UUID
	ProjectID          uuid.UUID
	CreatedByPrincipal uuid.UUID
	CreatedAt          time.Time
}

// ExternalSession records one provider-side session as an edge into a
// Conversation. Relation widens §10.2's list to match §10.4's fuller
// vocabulary — see migrations/000011_history.up.sql's CHECK constraint.
type ExternalSession struct {
	ID                      uuid.UUID
	ProjectID               uuid.UUID
	ConversationID          uuid.UUID
	Provider                string
	Client                  string
	ProviderSessionID       string
	ParentExternalSessionID *uuid.UUID
	Relation                string
	StartedAt               time.Time
	EndedAt                 *time.Time
}

// HistoryObject is one immutable, append-only record of conversation
// content. A compacted/summarized representation is a NEW HistoryObject
// with DerivedFrom pointing at its source — never an edit of the original
// (§10.3). Body is CONTENT PAYLOAD (ADR 0011), returned decrypted by
// Service.ListHistoryObjects, never stored in this struct as ciphertext.
type HistoryObject struct {
	ID                uuid.UUID
	ProjectID         uuid.UUID
	ConversationID    uuid.UUID
	ExternalSessionID uuid.UUID
	Type              string
	DerivedFrom       *uuid.UUID
	ContentHash       string
	Body              []byte
	CreatedAt         time.Time
}

// LineageEdge is one edge in a conversation's lineage graph — deliberately
// not assumed to be a tree (§10.4): a HistoryObject can have at most one
// DerivedFrom parent in this schema, but the combined edge set across
// history_objects.derived_from and external_sessions.relation can still
// branch and merge in ways a single tree cannot represent.
type LineageEdge struct {
	FromID uuid.UUID
	ToID   uuid.UUID
	Kind   string
}
