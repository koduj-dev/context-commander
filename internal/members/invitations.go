package members

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// Invitations (docs/adr/0030, migrations/000041): an address invited to a
// project with a role, accepted by the server the next time someone with
// that address signs in through the browser.

const (
	// DefaultInvitationTTL is how long an invitation stays acceptable.
	DefaultInvitationTTL = 14 * 24 * time.Hour
	// MaxInvitationTTL bounds what a caller may ask for.
	MaxInvitationTTL = 90 * 24 * time.Hour
)

// ErrInvalidEmail is an address that cannot be invited.
var ErrInvalidEmail = errors.New("members: an invitation needs an email address like someone@example.com")

// ErrInvitationNotFound is an invitation id that is not live in the project.
var ErrInvitationNotFound = errors.New("members: no such pending invitation in this project")

// Invitation is one pending (or decided) invitation.
type Invitation struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	Email      string
	Role       projects.Role
	InvitedBy  uuid.UUID
	CreatedAt  time.Time
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	AcceptedBy *uuid.UUID
	RevokedAt  *time.Time
}

// normalizeEmail lowercases and checks an address enough to be matched
// against what a directory or an administrator records. It is not RFC 5322:
// the address only has to equal one a provider has already verified.
func normalizeEmail(raw string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(raw))
	at := strings.LastIndexByte(e, '@')
	if len(e) < 3 || len(e) > 320 || at <= 0 || at == len(e)-1 ||
		!strings.Contains(e[at+1:], ".") || strings.ContainsAny(e, " \t\r\n<>()[],;:\"") {
		return "", ErrInvalidEmail
	}
	return e, nil
}

// Invite records an invitation. The caller needs the same capabilities as
// adding the member directly, OWNER included: an invitation is an Add that
// waits for its person. Inviting an address that already holds a live
// invitation to this project replaces it, so the newest role and expiry win.
//
// Whether the address already has an account is deliberately not consulted:
// answering "invited" for one address and "added" for another would tell
// anyone who may invite which addresses exist here.
func (s *Service) Invite(ctx context.Context, principalID, projectID uuid.UUID, email string, role projects.Role, ttl time.Duration) (Invitation, error) {
	switch role {
	case projects.RoleOwner, projects.RoleAdmin, projects.RoleContributor, projects.RoleReader:
	default:
		return Invitation{}, ErrInvalidRole
	}
	addr, err := normalizeEmail(email)
	if err != nil {
		return Invitation{}, err
	}
	if ttl <= 0 {
		ttl = DefaultInvitationTTL
	}
	if ttl > MaxInvitationTTL {
		ttl = MaxInvitationTTL
	}
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapMemberManage); err != nil {
		return Invitation{}, err
	}
	if role == projects.RoleOwner {
		if err := s.checkCapability(ctx, principalID, projectID, auth.CapMemberGrantOwner); err != nil {
			return Invitation{}, err
		}
	}

	inv := Invitation{ProjectID: projectID, Email: addr, Role: role, InvitedBy: principalID}
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE project_invitations SET revoked_at = now()
			 WHERE project_id = $1 AND email = $2 AND accepted_at IS NULL AND revoked_at IS NULL`,
			projectID, addr); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO project_invitations (project_id, email, role, invited_by_principal, expires_at)
			 VALUES ($1, $2, $3, $4, now() + $5::interval)
			 RETURNING id, created_at, expires_at`,
			projectID, addr, role, principalID, fmt.Sprintf("%d seconds", int64(ttl/time.Second)),
		).Scan(&inv.ID, &inv.CreatedAt, &inv.ExpiresAt); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID,
			Action: "member.invite", ResourceType: "project_invitation", ResourceID: &inv.ID,
			Result: audit.ResultAllow,
		})
	})
	if err != nil {
		return Invitation{}, fmt.Errorf("members: invite: %w", err)
	}
	return inv, nil
}

// Invitations lists the project's live invitations, newest first. Gated on
// CapMemberManage, not CapMemberRead: an invitation names an address that
// is not a member yet, and the people who may invite are the people who
// need the list.
func (s *Service) Invitations(ctx context.Context, principalID, projectID uuid.UUID) ([]Invitation, error) {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapMemberManage); err != nil {
		return nil, err
	}
	out := []Invitation{}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id, project_id, email, role, invited_by_principal, created_at, expires_at
			 FROM project_invitations
			 WHERE project_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()
			 ORDER BY created_at DESC, id DESC
			 LIMIT 200`, projectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var i Invitation
			if err := rows.Scan(&i.ID, &i.ProjectID, &i.Email, &i.Role, &i.InvitedBy, &i.CreatedAt, &i.ExpiresAt); err != nil {
				return err
			}
			out = append(out, i)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("members: invitations: %w", err)
	}
	return out, nil
}

// RevokeInvitation withdraws a live invitation.
func (s *Service) RevokeInvitation(ctx context.Context, principalID, projectID, invitationID uuid.UUID) error {
	if err := s.checkCapability(ctx, principalID, projectID, auth.CapMemberManage); err != nil {
		return err
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE project_invitations SET revoked_at = now()
			 WHERE id = $1 AND project_id = $2 AND accepted_at IS NULL AND revoked_at IS NULL`,
			invitationID, projectID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrInvitationNotFound
		}
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &projectID, PrincipalID: &principalID,
			Action: "member.invite.revoke", ResourceType: "project_invitation", ResourceID: &invitationID,
			Result: audit.ResultAllow,
		})
	})
	if err != nil {
		return fmt.Errorf("members: revoke invitation: %w", err)
	}
	return nil
}

// AcceptInvitations makes principalID a member of every project that holds
// a live invitation for its user's address, and returns those projects. The
// address is read from the database, never passed in. Called after every
// browser sign-in; a service principal has no address and joins nothing.
func AcceptInvitations(ctx context.Context, pool *db.Pool, principalID uuid.UUID) ([]uuid.UUID, error) {
	var joined []uuid.UUID
	err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT cc_accept_invitations($1)`, principalID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			joined = append(joined, id)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("members: accept invitations: %w", err)
	}
	return joined, nil
}

// HasPendingInvitation reports whether an address holds a live invitation
// anywhere, for the sign-in callback: an invitation admits its address and
// lets a first sign-in create the account (docs/adr/0030).
func HasPendingInvitation(ctx context.Context, pool *db.Pool, email string) (bool, error) {
	var ok bool
	err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT cc_has_pending_invitation($1)`, email).Scan(&ok)
	})
	if err != nil {
		return false, fmt.Errorf("members: pending invitation: %w", err)
	}
	return ok, nil
}
