package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/koduj-dev/context-commander/internal/db"
)

// Refusals of DeleteUser. ErrLastOwner and ErrLastAdmin carry the server's
// explanation, naming what blocks the deletion.
var (
	ErrDeleteForbidden    = errors.New("identity: only the account's owner or an installation administrator can delete it")
	ErrDeleteNeedsPerson  = errors.New("identity: only a person signed in to the interface can delete an account")
	ErrDeleteConfirmation = errors.New("identity: confirm by typing the account's email address")
	ErrAccountGone        = errors.New("identity: no such account, or it is already deleted")
	ErrLastOwner          = errors.New("identity: the account is the last OWNER of something")
	ErrLastAdmin          = errors.New("identity: the account is the installation's last administrator")
)

// DeleteUser deletes targetUserID's account (docs/adr/0042). The caller is
// either that person or an installation administrator, signed in to the
// interface (a token cannot delete an account), confirming with the
// account's email address. The person's email and name are replaced by a
// tombstone; their memberships, administrator role, password, directory
// links, sessions and tokens go; pending invitations to their address are
// revoked. What they wrote stays with its projects, attributed to "Deleted
// user". Refused while they are the last OWNER of a project or organization,
// or the last administrator.
func DeleteUser(ctx context.Context, pool *db.Pool, principalID uuid.UUID, clientID *uuid.UUID, targetUserID uuid.UUID, confirm string) error {
	err := db.WithPrincipal(ctx, pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := requireBrowserSession(ctx, tx, principalID, clientID); err != nil {
			return err
		}
		var email string
		err := tx.QueryRow(ctx, `SELECT email FROM users WHERE id = $1 AND deleted_at IS NULL`, targetUserID).Scan(&email)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAccountGone
		}
		if err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(confirm), email) {
			return ErrDeleteConfirmation
		}
		_, err = tx.Exec(ctx, `SELECT cc_delete_user($1)`, targetUserID)
		return err
	})
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "CCD01":
			return ErrDeleteForbidden
		case "CCD03":
			return ErrAccountGone
		case "CCD04":
			return fmt.Errorf("%w: %s", ErrLastOwner, pgErr.Message)
		case "CCD05":
			return fmt.Errorf("%w: %s", ErrLastAdmin, pgErr.Message)
		}
	}
	for _, known := range []error{ErrDeleteNeedsPerson, ErrDeleteConfirmation, ErrAccountGone} {
		if errors.Is(err, known) {
			return err
		}
	}
	return fmt.Errorf("identity: delete user: %w", err)
}

// requireBrowserSession: a user principal on a browser session credential.
func requireBrowserSession(ctx context.Context, tx pgx.Tx, principalID uuid.UUID, clientID *uuid.UUID) error {
	if clientID == nil {
		return ErrDeleteNeedsPerson
	}
	var kind, clientType string
	err := tx.QueryRow(ctx,
		`SELECT p.kind, c.client_type FROM client_credentials c JOIN principals p ON p.id = c.principal_id
		 WHERE c.id = $1 AND c.principal_id = $2`, *clientID, principalID,
	).Scan(&kind, &clientType)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDeleteNeedsPerson
	}
	if err != nil {
		return err
	}
	if kind != string(PrincipalKindUser) || clientType != SessionClientType {
		return ErrDeleteNeedsPerson
	}
	return nil
}
