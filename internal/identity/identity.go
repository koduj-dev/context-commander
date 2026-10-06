// Package identity defines Principal (User | ServicePrincipal) and
// ClientCredential — see docs/adr/0004-principal-model.md and
// docs/adr/0009-pat-format-and-bootstrap.md.
package identity

import (
	"time"

	"github.com/google/uuid"
)

type PrincipalKind string

const (
	PrincipalKindUser    PrincipalKind = "user"
	PrincipalKindService PrincipalKind = "service"
)

// Principal is a User or a ServicePrincipal, never both — see
// docs/adr/0004-principal-model.md. It is what AuthzService.Can() and RLS's
// app.current_principal_id are keyed on, not a bare user ID, so a
// ServicePrincipal (CI agent, worker) is a real, first-class project member.
type Principal struct {
	ID                 uuid.UUID
	Kind               PrincipalKind
	UserID             *uuid.UUID
	ServicePrincipalID *uuid.UUID
	CreatedAt          time.Time
}

type User struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	CreatedAt   time.Time
}

type ServicePrincipal struct {
	ID          uuid.UUID
	Name        string
	Description string
	CreatedAt   time.Time
	// CreatedBy is the user who registered this agent, or nil for one
	// registered before the column existed (migrations/000026). It is
	// provenance and grants nothing: an agent's access comes from its own
	// project memberships (docs/adr/0004), never from this user's.
	CreatedBy *uuid.UUID
}

// ClientCredential is a PAT — see docs/adr/0009-pat-format-and-bootstrap.md
// for the cc_<id>_<secret> token format and why only TokenHash (never the
// raw secret) is stored.
type ClientCredential struct {
	ID          uuid.UUID
	PrincipalID uuid.UUID
	ClientType  string
	TokenHash   string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
}

// Active reports whether the credential can currently authenticate a
// request: not revoked, and not expired (a nil ExpiresAt never expires —
// see docs/adr/0009, a PAT without an expiry is a deliberate, supported
// choice, not an oversight).
func (c ClientCredential) Active(now time.Time) bool {
	if c.RevokedAt != nil {
		return false
	}
	if c.ExpiresAt != nil && !now.Before(*c.ExpiresAt) {
		return false
	}
	return true
}
