package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
)

// ErrForbidden is returned when AuthzService.Can denies the requested
// capability — see internal/artifacts.ErrForbidden for the same reasoning.
var ErrForbidden = errors.New("events: forbidden")

// Service exposes read access to the outbox to adapters — see
// docs/architecture-cookbook.md §22's event.list. There is deliberately no
// Service.Publish: publishing must happen inside the same transaction as
// the domain mutation it describes (docs/adr/0013-transactional-outbox-and-storage-lifecycle.md),
// so Publish stays the package-level function other domains' own
// transactions call directly, not a standalone adapter-facing operation.
type Service struct {
	pool  *db.Pool
	authz *auth.AuthzService
}

func NewService(pool *db.Pool, authz *auth.AuthzService) *Service {
	return &Service{pool: pool, authz: authz}
}

func (s *Service) checkCapability(ctx context.Context, principalID, projectID uuid.UUID, cap auth.Capability) error {
	allowed, err := s.authz.Can(ctx, principalID, projectID, cap)
	if err != nil {
		return fmt.Errorf("events: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// maxListLimit bounds a single List call the same way
// internal/audit.QueryOptions.Limit does — a caller-supplied limit above
// this (or <= 0) falls back to defaultListLimit instead of running an
// effectively unbounded query.
const (
	defaultListLimit = 100
	maxListLimit     = 500
)

// List returns up to limit events for projectID with seq > afterSeq, after
// checking principalID may read events on that project.
func (s *Service) List(ctx context.Context, principalID, projectID uuid.UUID, afterSeq int64, limit int) ([]Event, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapEventRead); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > maxListLimit {
		limit = defaultListLimit
	}

	var out []Event
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		out, err = ListSince(ctx, tx, projectID, afterSeq, limit)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
