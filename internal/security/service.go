package security

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/paging"
)

// Service is the read side, and it is installation-admin-only
// (docs/adr/0017).
//
// Not project-scoped, for two reasons that both matter. A refusal is
// frequently about a project the caller has no access to — scoping the
// record by the very membership that was missing would hide exactly the
// rows worth reading. And the timeline is cross-project by nature: one
// credential probing five projects is a pattern no per-project view can
// show.
//
// The trade is that installation admins can see that principal P was
// refused capability C on project X. That is metadata about a refusal, never
// content: migrations/000019 admits no free-form payload, and Recorder
// writes only fixed reason strings.
type Service struct {
	pool               *db.Pool
	installationAdmins *installation.Store
}

func NewService(pool *db.Pool, installationAdmins *installation.Store) *Service {
	return &Service{pool: pool, installationAdmins: installationAdmins}
}

// List returns one page of events, newest first. userID (not principalID)
// is what installation-admin rights are keyed on — a ServicePrincipal can
// never hold them (docs/adr/0017), so an agent cannot read the record of
// what it was refused.
func (s *Service) List(ctx context.Context, principalID, userID uuid.UUID, opts paging.Options) (paging.Page[Event], error) {
	afterID, hasCursor, err := opts.DecodeSeq()
	if err != nil {
		return paging.Page[Event]{}, err
	}
	limit := opts.ClampedLimit()

	out := make([]Event, 0, limit)
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		isAdmin, err := s.installationAdmins.IsAdmin(ctx, tx, userID)
		if err != nil {
			return fmt.Errorf("security: check installation admin: %w", err)
		}
		if !isAdmin {
			return ErrForbidden
		}

		// Keyset on id alone: it is a bigserial, so it already orders the
		// way ts does without ts's ambiguity when two events land in the
		// same microsecond. DecodeSeq gives "after this position" for an
		// ascending list, so a newest-first page continues *below* the
		// cursor — hence "< $2" with the cursor carrying the last id
		// returned.
		rows, err := tx.Query(ctx,
			`SELECT id, ts, kind, principal_id, credential_id, project_id,
			        COALESCE(capability, ''), reason, COALESCE(request_id, '')
			 FROM security_events
			 WHERE (NOT $1::bool OR id < $2::bigint)
			 ORDER BY id DESC LIMIT $3`,
			hasCursor, afterID, limit+1)
		if err != nil {
			return fmt.Errorf("security: list: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var e Event
			if err := rows.Scan(&e.ID, &e.TS, &e.Kind, &e.PrincipalID, &e.CredentialID, &e.ProjectID,
				&e.Capability, &e.Reason, &e.RequestID); err != nil {
				return fmt.Errorf("security: scan: %w", err)
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return paging.Page[Event]{}, err
	}

	items, hasMore := paging.More(out, limit)
	page := paging.Page[Event]{Items: items}
	if hasMore {
		page.NextCursor = paging.EncodeSeq(items[len(items)-1].ID)
	}
	return page, nil
}
