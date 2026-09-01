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

	// ErrInvalidReference is returned when a supplied conversation_id,
	// external_session_id, parent_external_session_id, or derived_from
	// does not belong to this project (and, where applicable, this
	// conversation) — the database enforces this via composite foreign
	// keys (migrations/000015); this error gives that violation a stable,
	// domain-level shape instead of an opaque constraint-violation error.
	ErrInvalidReference = errors.New("history: referenced object does not belong to this project/conversation")

	// ErrTooLarge is returned by Service.AppendHistoryObject when plaintext
	// exceeds MaxContentSize — history had no domain-level content limit at
	// all before this, unlike internal/artifacts.MaxSize.
	ErrTooLarge = errors.New("history: content exceeds the 10 MB MVP limit")
)

// MaxContentSize is the MVP per-HistoryObject size limit — smaller than
// internal/artifacts.MaxSize (50 MB) since a single conversation turn/session
// transcript is expected to be much smaller than an arbitrary artifact, and
// ListHistoryObjects decrypts every object of a conversation into memory at
// once (see its own MaxListLimit cap for the row-count half of this).
const MaxContentSize = 10 * 1024 * 1024 // 10 MB

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
