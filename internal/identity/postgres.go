package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Repository methods take a pgx.Tx directly rather than an abstract querier
// interface — a deliberate, documented simplification for now (Postgres is
// the only backend through Phase 1-3), not an oversight. See
// docs/adr/0007-metadata-store-abstraction-sqlite.md §"Consequences": the
// interface boundary that matters is at the *package* level (callers depend
// on identity.Store, not on pgx types, throughout internal/auth etc.); a
// second backend, when it exists, is free to change this signature based on
// what a real second implementation actually needs, rather than guessing
// now. Callers obtain a tx via internal/db.WithPrincipal / WithoutPrincipal
// — nothing in this package opens its own transaction (docs/adr/0003).

var ErrNotFound = errors.New("identity: not found")

// Store is the Postgres-backed identity repository.
type Store struct{}

func NewStore() *Store { return &Store{} }

func (s *Store) CreateUser(ctx context.Context, tx pgx.Tx, email, displayName string) (User, error) {
	var u User
	err := tx.QueryRow(ctx,
		`INSERT INTO users (email, display_name) VALUES ($1, $2)
		 RETURNING id, email, display_name, created_at`,
		email, displayName,
	).Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt)
	if err != nil {
		return User{}, fmt.Errorf("identity: create user: %w", err)
	}
	return u, nil
}

// GetUser looks up a User by id — needed anywhere a Principal must be shown
// as a human (email/display name), since Principal itself only carries the
// UserID pointer.
func (s *Store) GetUser(ctx context.Context, tx pgx.Tx, id uuid.UUID) (User, error) {
	var u User
	err := tx.QueryRow(ctx,
		`SELECT id, email, display_name, created_at FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("identity: get user: %w", err)
	}
	return u, nil
}

// GetUsersForPrincipals batch-resolves Principal ids to their Users, keyed
// by *principal* id (not user id) — the shape the audit log view needs to
// label a page of audit_log rows (which carry principal_id) with actor
// emails in one query instead of one per row. A ServicePrincipal id, or one
// with no matching row, is simply absent from the result map.
func (s *Store) GetUsersForPrincipals(ctx context.Context, tx pgx.Tx, principalIDs []uuid.UUID) (map[uuid.UUID]User, error) {
	out := make(map[uuid.UUID]User, len(principalIDs))
	if len(principalIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx,
		`SELECT p.id, u.id, u.email, u.display_name, u.created_at
		 FROM principals p JOIN users u ON u.id = p.user_id
		 WHERE p.id = ANY($1)`, principalIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("identity: get users for principals: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var principalID uuid.UUID
		var u User
		if err := rows.Scan(&principalID, &u.ID, &u.Email, &u.DisplayName, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("identity: get users for principals: scan: %w", err)
		}
		out[principalID] = u
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: get users for principals: %w", err)
	}
	return out, nil
}

func (s *Store) CreateServicePrincipal(ctx context.Context, tx pgx.Tx, name, description string) (ServicePrincipal, error) {
	var sp ServicePrincipal
	err := tx.QueryRow(ctx,
		`INSERT INTO service_principals (name, description) VALUES ($1, $2)
		 RETURNING id, name, description, created_at`,
		name, description,
	).Scan(&sp.ID, &sp.Name, &sp.Description, &sp.CreatedAt)
	if err != nil {
		return ServicePrincipal{}, fmt.Errorf("identity: create service principal: %w", err)
	}
	return sp, nil
}

// GetServicePrincipal looks up a ServicePrincipal by id — the service-kind
// counterpart to GetUser, used wherever a Principal must be shown as
// whatever it actually is (getMe in adapters/http).
func (s *Store) GetServicePrincipal(ctx context.Context, tx pgx.Tx, id uuid.UUID) (ServicePrincipal, error) {
	var sp ServicePrincipal
	err := tx.QueryRow(ctx,
		`SELECT id, name, description, created_at FROM service_principals WHERE id = $1`, id,
	).Scan(&sp.ID, &sp.Name, &sp.Description, &sp.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ServicePrincipal{}, ErrNotFound
	}
	if err != nil {
		return ServicePrincipal{}, fmt.Errorf("identity: get service principal: %w", err)
	}
	return sp, nil
}

func (s *Store) CreatePrincipalForUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (Principal, error) {
	var p Principal
	err := tx.QueryRow(ctx,
		`INSERT INTO principals (kind, user_id) VALUES ('user', $1)
		 RETURNING id, kind, user_id, service_principal_id, created_at`,
		userID,
	).Scan(&p.ID, &p.Kind, &p.UserID, &p.ServicePrincipalID, &p.CreatedAt)
	if err != nil {
		return Principal{}, fmt.Errorf("identity: create principal for user: %w", err)
	}
	return p, nil
}

func (s *Store) CreatePrincipalForService(ctx context.Context, tx pgx.Tx, servicePrincipalID uuid.UUID) (Principal, error) {
	var p Principal
	err := tx.QueryRow(ctx,
		`INSERT INTO principals (kind, service_principal_id) VALUES ('service', $1)
		 RETURNING id, kind, user_id, service_principal_id, created_at`,
		servicePrincipalID,
	).Scan(&p.ID, &p.Kind, &p.UserID, &p.ServicePrincipalID, &p.CreatedAt)
	if err != nil {
		return Principal{}, fmt.Errorf("identity: create principal for service: %w", err)
	}
	return p, nil
}

func (s *Store) GetPrincipal(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Principal, error) {
	var p Principal
	err := tx.QueryRow(ctx,
		`SELECT id, kind, user_id, service_principal_id, created_at FROM principals WHERE id = $1`,
		id,
	).Scan(&p.ID, &p.Kind, &p.UserID, &p.ServicePrincipalID, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrNotFound
	}
	if err != nil {
		return Principal{}, fmt.Errorf("identity: get principal: %w", err)
	}
	return p, nil
}

// IssueCredential inserts a new ClientCredential row and returns the
// caller-facing token exactly once (it is never recoverable afterwards —
// only the hash is stored). expiresAt may be nil for a non-expiring PAT.
func (s *Store) IssueCredential(
	ctx context.Context, tx pgx.Tx,
	principalID uuid.UUID, clientType string, expiresAt *time.Time,
) (token string, cred ClientCredential, err error) {
	// The credential row is inserted first (without a token hash) purely to
	// obtain its id — the token format embeds that id, and the hash can only
	// be computed once the id is known. This keeps GenerateToken (which
	// needs the id) decoupled from persistence.
	var id uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO client_credentials (principal_id, client_type, token_hash, expires_at)
		 VALUES ($1, $2, '', $3) RETURNING id`,
		principalID, clientType, expiresAt,
	).Scan(&id)
	if err != nil {
		return "", ClientCredential{}, fmt.Errorf("identity: issue credential: %w", err)
	}

	token, tokenHash, err := GenerateToken(id)
	if err != nil {
		return "", ClientCredential{}, err
	}

	err = tx.QueryRow(ctx,
		`UPDATE client_credentials SET token_hash = $1 WHERE id = $2
		 RETURNING id, principal_id, client_type, token_hash, created_at, last_used_at, expires_at, revoked_at`,
		tokenHash, id,
	).Scan(&cred.ID, &cred.PrincipalID, &cred.ClientType, &cred.TokenHash,
		&cred.CreatedAt, &cred.LastUsedAt, &cred.ExpiresAt, &cred.RevokedAt)
	if err != nil {
		return "", ClientCredential{}, fmt.Errorf("identity: store credential hash: %w", err)
	}

	return token, cred, nil
}

// GetCredential is the O(1) lookup half of PAT verification — see
// docs/adr/0009: callers ParseToken() first, then look up by the resulting
// id, never scan/compare against every stored hash.
func (s *Store) GetCredential(ctx context.Context, tx pgx.Tx, id uuid.UUID) (ClientCredential, error) {
	var cred ClientCredential
	err := tx.QueryRow(ctx,
		`SELECT id, principal_id, client_type, token_hash, created_at, last_used_at, expires_at, revoked_at
		 FROM client_credentials WHERE id = $1`,
		id,
	).Scan(&cred.ID, &cred.PrincipalID, &cred.ClientType, &cred.TokenHash,
		&cred.CreatedAt, &cred.LastUsedAt, &cred.ExpiresAt, &cred.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ClientCredential{}, ErrNotFound
	}
	if err != nil {
		return ClientCredential{}, fmt.Errorf("identity: get credential: %w", err)
	}
	return cred, nil
}

func (s *Store) TouchCredentialLastUsed(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE client_credentials SET last_used_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("identity: touch credential: %w", err)
	}
	return nil
}
