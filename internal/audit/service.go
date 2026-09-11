package audit

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
var ErrForbidden = errors.New("audit: forbidden")

// Service exposes read access to the audit trail to adapters.
// auth.CapAuditRead is ADMIN/OWNER-only (see internal/auth/capability.go)
// — the audit trail is more sensitive than most of what it describes.
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
		return fmt.Errorf("audit: authorization check: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// Query returns one page of audit_log rows for projectID, after checking
// principalID holds CapAuditRead on that project.
func (s *Service) Query(ctx context.Context, principalID, projectID uuid.UUID, opts QueryOptions) (paging.Page[Record], error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapAuditRead); err != nil {
		return paging.Page[Record]{}, err
	}

	var out paging.Page[Record]
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var err error
		out, err = Query(ctx, tx, projectID, opts)
		return err
	})
	if err != nil {
		return paging.Page[Record]{}, err
	}
	return out, nil
}
