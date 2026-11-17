package skills

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/organizations"
)

// Trust levels a skill version reports (docs/adr/0040).
const (
	TrustUnverified = "unverified"
	TrustSigned     = "signed"
	TrustApproved   = "approved"
)

// trustExpr is a version's effective trust, for every query that reports
// one: signed when its signature was verified at publish, approved when a
// person approved it since, unverified otherwise.
const trustExpr = `CASE WHEN v.trust_status = 'signed' THEN 'signed'
	WHEN EXISTS (SELECT 1 FROM skill_version_approvals ap WHERE ap.skill_version_id = v.id) THEN 'approved'
	ELSE 'unverified' END`

var (
	ErrBadSignature   = errors.New("skills: the signature does not verify against any active signer key")
	ErrBadSigner      = errors.New("skills: a signer needs a name and a 32-byte Ed25519 public key, base64")
	ErrSignerExists   = errors.New("skills: a signer with that name or key already exists")
	ErrNeedsPerson    = errors.New("skills: only a person signed in to the interface can approve a skill version")
	ErrSelfApproval   = errors.New("skills: a skill version cannot be approved by the person who published it")
	ErrAlreadyTrusted = errors.New("skills: this skill version is already signed or approved")
	ErrUntrusted      = errors.New("skills: this installation installs only signed or approved skill versions")
)

// SigningMessage is what a publisher signs: the format, the skill's name and
// the content's SHA-256, one per line. The version number is not in it,
// because the server assigns it at publish.
func SigningMessage(name, checksum string) []byte {
	return []byte("context-commander/skill/v1\n" + name + "\n" + checksum)
}

// Signer is a key an installation administrator trusts to sign skills.
type Signer struct {
	ID        uuid.UUID
	Name      string
	PublicKey []byte
	AddedBy   *uuid.UUID
	CreatedAt time.Time
	RevokedAt *time.Time
}

// SetRequireTrust makes Install refuse skill versions that are neither
// signed nor approved, and lets the context pack leave them out.
func (s *Service) SetRequireTrust(on bool) { s.requireTrust = on }

// RequireTrust reports whether only trusted versions may be installed.
func (s *Service) RequireTrust() bool { return s.requireTrust }

func (s *Service) requireAdmin(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if userID == uuid.Nil {
		return ErrForbidden
	}
	ok, err := s.installationAdmins.IsAdmin(ctx, tx, userID)
	if err != nil {
		return fmt.Errorf("skills: check installation admin: %w", err)
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

// AddSigner registers a public key whose signatures make a version
// "signed". Installation administrators only.
func (s *Service) AddSigner(ctx context.Context, principalID, userID uuid.UUID, name, publicKeyB64 string) (Signer, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyB64))
	name = strings.TrimSpace(name)
	if err != nil || len(key) != ed25519.PublicKeySize || name == "" || len(name) > 100 {
		return Signer{}, ErrBadSigner
	}
	sg := Signer{ID: uuid.New(), Name: name, PublicKey: key, AddedBy: &principalID}
	err = db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := s.requireAdmin(ctx, tx, userID); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`INSERT INTO skill_signers (id, name, public_key, added_by) VALUES ($1, $2, $3, $4) RETURNING created_at`,
			sg.ID, sg.Name, sg.PublicKey, principalID).Scan(&sg.CreatedAt)
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Signer{}, ErrSignerExists
	}
	if err != nil {
		return Signer{}, fmt.Errorf("skills: add signer: %w", err)
	}
	return sg, nil
}

// Signers lists every registered key, revoked ones included.
func (s *Service) Signers(ctx context.Context, principalID uuid.UUID) ([]Signer, error) {
	var out []Signer
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, name, public_key, added_by, created_at, revoked_at FROM skill_signers ORDER BY created_at`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Signer, error) {
			var sg Signer
			err := r.Scan(&sg.ID, &sg.Name, &sg.PublicKey, &sg.AddedBy, &sg.CreatedAt, &sg.RevokedAt)
			return sg, err
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("skills: list signers: %w", err)
	}
	return out, nil
}

// RevokeSigner stops a key from signing new versions. Versions it signed
// before stay signed: their signature was valid when it was checked.
func (s *Service) RevokeSigner(ctx context.Context, principalID, userID, signerID uuid.UUID) error {
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := s.requireAdmin(ctx, tx, userID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE skill_signers SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, signerID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil && !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("skills: revoke signer: %w", err)
	}
	return err
}

// verifySignature returns the active signer whose key verifies signature
// over (name, checksum), or ErrBadSignature.
func verifySignature(ctx context.Context, tx pgx.Tx, name, checksum string, signature []byte) (uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT id, public_key FROM skill_signers WHERE revoked_at IS NULL`)
	if err != nil {
		return uuid.Nil, err
	}
	type key struct {
		id  uuid.UUID
		pub []byte
	}
	keys, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (key, error) {
		var k key
		err := r.Scan(&k.id, &k.pub)
		return k, err
	})
	if err != nil {
		return uuid.Nil, err
	}
	msg := SigningMessage(name, checksum)
	for _, k := range keys {
		if len(k.pub) == ed25519.PublicKeySize && ed25519.Verify(k.pub, msg, signature) {
			return k.id, nil
		}
	}
	return uuid.Nil, ErrBadSignature
}

// Approve marks a skill version as reviewed and trusted. It needs a person
// signed in to the interface (an agent cannot vouch for a skill), who is
// not the version's publisher, and who may approve for the skill's scope:
// an installation administrator for installation and organization skills,
// an organization manager for organization skills, a project ADMIN or
// OWNER for project skills.
func (s *Service) Approve(ctx context.Context, principalID, userID uuid.UUID, clientID *uuid.UUID, versionID uuid.UUID, note string) error {
	if len(note) > 2000 {
		note = note[:2000]
	}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		if err := requireSession(ctx, tx, principalID, clientID); err != nil {
			return err
		}
		var scope Scope
		var scopeRef, publishedBy *uuid.UUID
		var status string
		err := tx.QueryRow(ctx,
			`SELECT sk.scope, sk.scope_ref, v.published_by, `+trustExpr+`
			 FROM skill_versions v JOIN skills sk ON sk.id = v.skill_id WHERE v.id = $1`, versionID,
		).Scan(&scope, &scopeRef, &publishedBy, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != TrustUnverified {
			return ErrAlreadyTrusted
		}
		if publishedBy != nil && *publishedBy == principalID {
			return ErrSelfApproval
		}
		if err := s.checkApprover(ctx, tx, principalID, userID, scope, scopeRef); err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO skill_version_approvals (skill_version_id, approved_by, note) VALUES ($1, $2, NULLIF($3, ''))`,
			versionID, principalID, note)
		return err
	})
	switch {
	case err == nil, errors.Is(err, ErrNotFound), errors.Is(err, ErrForbidden), errors.Is(err, ErrNeedsPerson),
		errors.Is(err, ErrSelfApproval), errors.Is(err, ErrAlreadyTrusted):
		return err
	default:
		return fmt.Errorf("skills: approve: %w", err)
	}
}

func (s *Service) checkApprover(ctx context.Context, tx pgx.Tx, principalID, userID uuid.UUID, scope Scope, scopeRef *uuid.UUID) error {
	isAdmin := false
	if userID != uuid.Nil {
		var err error
		if isAdmin, err = s.installationAdmins.IsAdmin(ctx, tx, userID); err != nil {
			return err
		}
	}
	switch scope {
	case ScopeInstallation:
		if isAdmin {
			return nil
		}
	case ScopeOrganization:
		if isAdmin {
			return nil
		}
		if scopeRef != nil && userID != uuid.Nil {
			if m, err := s.orgs.GetMembership(ctx, tx, *scopeRef, userID); err == nil &&
				organizations.RoleCan(m.Role, organizations.CapOrgManageMembers) {
				return nil
			}
		}
	case ScopeProject:
		if scopeRef != nil {
			if m, err := s.projects.GetMembership(ctx, tx, *scopeRef, principalID); err == nil &&
				auth.RoleCan(m.Role, auth.CapSkillOverride) {
				return nil
			}
		}
	}
	return ErrForbidden
}

// versionTrusted reports whether a version is signed or approved.
func versionTrusted(ctx context.Context, tx pgx.Tx, versionID uuid.UUID) (bool, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT `+trustExpr+` FROM skill_versions v WHERE v.id = $1`, versionID).Scan(&status)
	return status != TrustUnverified, err
}

// requireSession: a user principal on a browser session credential.
func requireSession(ctx context.Context, tx pgx.Tx, principalID uuid.UUID, clientID *uuid.UUID) error {
	if clientID == nil {
		return ErrNeedsPerson
	}
	var kind, clientType string
	err := tx.QueryRow(ctx,
		`SELECT p.kind, c.client_type FROM client_credentials c JOIN principals p ON p.id = c.principal_id
		 WHERE c.id = $1 AND c.principal_id = $2`, *clientID, principalID,
	).Scan(&kind, &clientType)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNeedsPerson
	}
	if err != nil {
		return err
	}
	if kind != string(identity.PrincipalKindUser) || clientType != identity.SessionClientType {
		return ErrNeedsPerson
	}
	return nil
}
