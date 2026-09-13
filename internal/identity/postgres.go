package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/koduj-dev/context-commander/internal/paging"
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

// ErrAlreadyExists is a unique-constraint violation on an identity someone
// asked to create — a second user with the same email, a second service
// principal with the same name.
//
// It is a distinct error because the caller made a legitimate request about
// a real conflict, and the two answers call for different things: "internal
// error" tells an operator their installation is broken, while "that email
// already has an account" tells them what to do next. Without this, the
// only signal was a 500.
var ErrAlreadyExists = errors.New("identity: already exists")

// isUniqueViolation reports whether err is Postgres 23505. Checked on the
// SQLSTATE rather than on the message, which is localized and changes
// between versions.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

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
	if isUniqueViolation(err) {
		return User{}, fmt.Errorf("%w: a user with this email", ErrAlreadyExists)
	}
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

// GetUserByEmail looks up a User by email — used wherever an operator
// names a user by email rather than id (e.g. `ccd installation
// grant-admin`, mirroring `ccd token create --user <email>`'s existing
// convention).
func (s *Store) GetUserByEmail(ctx context.Context, tx pgx.Tx, email string) (User, error) {
	var u User
	err := tx.QueryRow(ctx,
		`SELECT id, email, display_name, created_at FROM users WHERE email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("identity: get user by email: %w", err)
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
	if isUniqueViolation(err) {
		return ServicePrincipal{}, fmt.Errorf("%w: a service principal with this name", ErrAlreadyExists)
	}
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

// ListCredentialsForPrincipal lists every PAT (including revoked/expired
// ones — the caller decides what to show) belonging to principalID. This
// package has no authz layer of its own (see ADR-0009's consequences): a
// caller wanting "list only *my* tokens" must pass its own principal id
// here, never one read from a request body.
func (s *Store) ListCredentialsForPrincipal(ctx context.Context, tx pgx.Tx, principalID uuid.UUID) ([]ClientCredential, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, principal_id, client_type, token_hash, created_at, last_used_at, expires_at, revoked_at
		 FROM client_credentials WHERE principal_id = $1 ORDER BY created_at DESC`,
		principalID,
	)
	if err != nil {
		return nil, fmt.Errorf("identity: list credentials: %w", err)
	}
	defer rows.Close()

	var out []ClientCredential
	for rows.Next() {
		var c ClientCredential
		if err := rows.Scan(&c.ID, &c.PrincipalID, &c.ClientType, &c.TokenHash,
			&c.CreatedAt, &c.LastUsedAt, &c.ExpiresAt, &c.RevokedAt); err != nil {
			return nil, fmt.Errorf("identity: list credentials: scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: list credentials: %w", err)
	}
	return out, nil
}

// RevokeCredential revokes a PAT — but only if it belongs to principalID.
// The ownership check is in the WHERE clause, not a separate fetch+compare,
// so "doesn't exist" and "exists but isn't yours" are indistinguishable
// from the caller's side (ErrNotFound either way) — same reasoning as
// project.resolve for a non-member (adapters/mcp/tools_project.go): don't
// hand out more information than "you can't have this." Revoking an
// already-revoked credential is also ErrNotFound (0 rows match
// revoked_at IS NULL), which is fine — the end state is identical.
func (s *Store) RevokeCredential(ctx context.Context, tx pgx.Tx, id, principalID uuid.UUID) error {
	tag, err := tx.Exec(ctx,
		`UPDATE client_credentials SET revoked_at = now()
		 WHERE id = $1 AND principal_id = $2 AND revoked_at IS NULL`,
		id, principalID,
	)
	if err != nil {
		return fmt.Errorf("identity: revoke credential: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeCredentialByID revokes a PAT by id alone, with no ownership check —
// the cross-principal counterpart to RevokeCredential, for an installation
// admin managing a principal that is not themselves
// (internal/installation.Service.RevokeCredentialFor is the only caller;
// it does its own CapCredentialManage check before reaching here).
func (s *Store) RevokeCredentialByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	tag, err := tx.Exec(ctx,
		`UPDATE client_credentials SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("identity: revoke credential by id: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DirectoryUser is a user together with the Principal that acts as them.
//
// The two ids are not interchangeable and confusing them is the classic
// mistake here: an organization membership is keyed by user, a project
// membership by principal (docs/adr/0006). An interface that offers a person
// to pick therefore needs both, and principals_user_unique guarantees there
// is exactly one principal to carry.
type DirectoryUser struct {
	User
	PrincipalID uuid.UUID
}

// DirectoryAgent is the same pairing for a ServicePrincipal.
type DirectoryAgent struct {
	ServicePrincipal
	PrincipalID uuid.UUID
}

// ListUsers returns one page of users, oldest first.
//
// It exists because nothing else did: every screen that needs to name a
// person could only ask the caller to paste a UUID, which is both the
// clumsiest thing in the product and a reliable source of mistakes. Reading
// the directory is an installation-admin operation — see
// installation.Service.ListUsers, which is the only caller.
func (s *Store) ListUsers(ctx context.Context, tx pgx.Tx, opts paging.Options) (paging.Page[DirectoryUser], error) {
	cursorTime, cursorID, hasCursor, err := opts.DecodeTimeUUID()
	if err != nil {
		return paging.Page[DirectoryUser]{}, err
	}
	limit := opts.ClampedLimit()

	// JOIN, not LEFT JOIN: every user gets a principal at creation and
	// principals_user_unique keeps it to one, so a user without one is a
	// broken row rather than a case to render around.
	rows, err := tx.Query(ctx,
		`SELECT u.id, u.email, u.display_name, u.created_at, p.id
		 FROM users u
		 JOIN principals p ON p.user_id = u.id
		 WHERE (NOT $1::bool OR (u.created_at, u.id) > ($2::timestamptz, $3::uuid))
		 ORDER BY u.created_at ASC, u.id ASC LIMIT $4`,
		hasCursor, cursorTime, cursorID, limit+1,
	)
	if err != nil {
		return paging.Page[DirectoryUser]{}, fmt.Errorf("identity: list users: %w", err)
	}
	defer rows.Close()

	out := make([]DirectoryUser, 0, limit)
	for rows.Next() {
		var u DirectoryUser
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt, &u.PrincipalID); err != nil {
			return paging.Page[DirectoryUser]{}, fmt.Errorf("identity: scan user: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return paging.Page[DirectoryUser]{}, fmt.Errorf("identity: list users: %w", err)
	}

	page, hasMore := paging.More(out, limit)
	next := ""
	if hasMore {
		last := page[len(page)-1]
		next = paging.EncodeTimeUUID(last.CreatedAt, last.ID)
	}
	return paging.Page[DirectoryUser]{Items: page, NextCursor: next}, nil
}

// ListServicePrincipals is ListUsers' counterpart for agents. Same ordering,
// same reasoning: an agent is named by a UUID everywhere until something can
// list them.
func (s *Store) ListServicePrincipals(ctx context.Context, tx pgx.Tx, opts paging.Options) (paging.Page[DirectoryAgent], error) {
	cursorTime, cursorID, hasCursor, err := opts.DecodeTimeUUID()
	if err != nil {
		return paging.Page[DirectoryAgent]{}, err
	}
	limit := opts.ClampedLimit()

	rows, err := tx.Query(ctx,
		`SELECT sp.id, sp.name, COALESCE(sp.description, ''), sp.created_at, p.id
		 FROM service_principals sp
		 JOIN principals p ON p.service_principal_id = sp.id
		 WHERE (NOT $1::bool OR (sp.created_at, sp.id) > ($2::timestamptz, $3::uuid))
		 ORDER BY sp.created_at ASC, sp.id ASC LIMIT $4`,
		hasCursor, cursorTime, cursorID, limit+1,
	)
	if err != nil {
		return paging.Page[DirectoryAgent]{}, fmt.Errorf("identity: list service principals: %w", err)
	}
	defer rows.Close()

	out := make([]DirectoryAgent, 0, limit)
	for rows.Next() {
		var a DirectoryAgent
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.CreatedAt, &a.PrincipalID); err != nil {
			return paging.Page[DirectoryAgent]{}, fmt.Errorf("identity: scan service principal: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return paging.Page[DirectoryAgent]{}, fmt.Errorf("identity: list service principals: %w", err)
	}

	page, hasMore := paging.More(out, limit)
	next := ""
	if hasMore {
		last := page[len(page)-1]
		next = paging.EncodeTimeUUID(last.CreatedAt, last.ID)
	}
	return paging.Page[DirectoryAgent]{Items: page, NextCursor: next}, nil
}
