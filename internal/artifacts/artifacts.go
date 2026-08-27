// Package artifacts defines Artifact, the immutable ArtifactVersion, and
// the mutable ArtifactVersionEnvelope — see
// docs/adr/0010-immutable-artifact-version.md — and Service, which
// orchestrates writes/reads across internal/crypto, internal/storage,
// internal/audit, and internal/events following the failure-safe lifecycle
// in docs/adr/0013-transactional-outbox-and-storage-lifecycle.md.
package artifacts

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// MaxSize is the MVP artifact size limit — decided (not left open), see
// docs/adr/0005-key-hierarchy-and-artifact-size-limit.md §addendum 2.
// internal/storage.ObjectStore stays stream-compatible; internal/crypto
// operates on []byte in v1. A chunked/streamed AEAD format gets designed
// against real usage if this limit ever actually matters, not speculatively
// before the first artifact exists.
const MaxSize = 50 * 1024 * 1024 // 50 MB

var (
	ErrNotFound = errors.New("artifacts: not found")
	ErrConflict = errors.New("artifacts: version conflict")
	ErrTooLarge = errors.New("artifacts: content exceeds the 50 MB MVP limit")
	ErrDeleted  = errors.New("artifacts: artifact is deleted")
)

type Artifact struct {
	ID                 uuid.UUID
	ProjectID          uuid.UUID
	LogicalPath        string
	ContentType        string
	CurrentVersion     int
	CreatedByPrincipal uuid.UUID
	CreatedByClient    *uuid.UUID
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          *time.Time
}

// Version is the immutable half of an artifact version — see
// docs/adr/0010. It never carries crypto wrapping state; that lives in the
// separate, mutable envelope (internal, not exported — callers only ever
// see decrypted content via Service.Read, never raw envelope fields).
type Version struct {
	ID                 uuid.UUID
	ProjectID          uuid.UUID
	ArtifactID         uuid.UUID
	Version            int
	ContentHash        string
	StorageKey         string
	SizeBytes          int64
	ParentVersion      *int
	CreatedByPrincipal uuid.UUID
	CreatedByClient    *uuid.UUID
	CreatedAt          time.Time
}
