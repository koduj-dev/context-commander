package http

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
)

// resolveUserID looks up the User principalID belongs to — needed by
// skills.Service.Install/Override and gates.AdminService.Bind/Unbind, whose
// organization-scope authorization checks organization_members by user, not
// by principal (docs/adr/0006). Returns uuid.Nil for a ServicePrincipal (no
// UserID), which simply fails any subsequent organization-membership lookup
// as ErrForbidden — a service principal cannot be an organization admin in
// v1, not a bug to work around here.
func resolveUserID(ctx context.Context, pool *db.Pool, principalID uuid.UUID) (uuid.UUID, error) {
	var userID uuid.UUID
	err := db.WithPrincipal(ctx, pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		var nullableUserID *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT user_id FROM principals WHERE id = $1`, principalID).Scan(&nullableUserID); err != nil {
			return err
		}
		if nullableUserID != nil {
			userID = *nullableUserID
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve user for principal %s: %w", principalID, err)
	}
	return userID, nil
}
