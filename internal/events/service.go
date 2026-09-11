package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/paging"
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

// List returns one page of projectID's events, oldest first, after checking
// principalID may read events on that project.
//
// domain_events.seq is a bigserial that only ever increases, so it is a
// complete keyset position on its own — no (timestamp, id) tiebreaker like
// the UUID-keyed lists need. The cursor is still the same opaque token as
// everywhere else, so a client pages events exactly the way it pages
// anything: echo back next_cursor.
//
// afterSeq stays available underneath, via ListSince, for the outbox worker
// that legitimately resumes from a seq it persisted itself — that is a
// different contract from a client paging a list, and collapsing the two
// would force the worker to synthesize cursors.
func (s *Service) List(ctx context.Context, principalID, projectID uuid.UUID, opts paging.Options) (paging.Page[Event], error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapEventRead); err != nil {
		return paging.Page[Event]{}, err
	}
	afterSeq, _, err := opts.DecodeSeq()
	if err != nil {
		return paging.Page[Event]{}, err
	}
	limit := opts.ClampedLimit()

	var out []Event
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		out, err = ListSince(ctx, tx, projectID, afterSeq, limit+1)
		return err
	})
	if err != nil {
		return paging.Page[Event]{}, err
	}

	items, hasMore := paging.More(out, limit)
	if items == nil {
		items = []Event{}
	}
	page := paging.Page[Event]{Items: items}
	if hasMore {
		page.NextCursor = paging.EncodeSeq(items[len(items)-1].Seq)
	}
	return page, nil
}
