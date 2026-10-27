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
// convention), and by the password sign-in path.
//
// Case-insensitive, matching the functional unique index added in
// migrations/000025: an email address's domain is case-insensitive by RFC and
// its local part is case-sensitive in theory and never in practice, so an
// exact match here would mean someone who capitalises their own address
// cannot sign in. The index guarantees at most one row can match.
func (s *Store) GetUserByEmail(ctx context.Context, tx pgx.Tx, email string) (User, error) {
	var u User
	err := tx.QueryRow(ctx,
		`SELECT id, email, display_name, created_at FROM users WHERE lower(email) = lower($1)`, email,
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

// createdBy is the user who registered this agent — provenance only, and it
// grants nothing (migrations/000026). Nil is accepted: `ccd installation
// create-service-principal` runs as an operator against --database-url with
// no authenticated user to name, and recording a false one would be worse
// than recording none.
func (s *Store) CreateServicePrincipal(ctx context.Context, tx pgx.Tx, name, description string, createdBy *uuid.UUID) (ServicePrincipal, error) {
	var sp ServicePrincipal
	err := tx.QueryRow(ctx,
		`INSERT INTO service_principals (name, description, created_by) VALUES ($1, $2, $3)
		 RETURNING id, name, description, created_at, created_by`,
		name, description, createdBy,
	).Scan(&sp.ID, &sp.Name, &sp.Description, &sp.CreatedAt, &sp.CreatedBy)
	if isUniqueViolation(err) {
		// The only unique key on this table is per registrar
		// (migrations/000027), so the clash is always with an agent the same
		// person registered — and saying so tells them where to look.
		return ServicePrincipal{}, fmt.Errorf("%w: you have already registered an agent with this name", ErrAlreadyExists)
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

// RecordActivity notes that principalID made an authenticated request today
// (UTC), for the weekly counts in cc_installation_trends()
// (migrations/000028). Once per principal per day: a repeat is a no-op.
//
// The application can write this table and never read it back — a principal's
// active days reach nobody except as a count.
func (s *Store) RecordActivity(ctx context.Context, tx pgx.Tx, principalID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO principal_activity_days (day, principal_id)
		 VALUES ((now() AT TIME ZONE 'UTC')::date, $1)
		 ON CONFLICT DO NOTHING`, principalID)
	if err != nil {
		return fmt.Errorf("identity: record activity: %w", err)
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
	// The registrar, already resolved to a person. Empty when nobody is
	// recorded — see ListServicePrincipals' LEFT JOIN. A directory that
	// returned a bare UUID here would be the exact problem the directory was
	// built to remove.
	CreatedByEmail string
	CreatedByName  string
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

	// LEFT JOIN, not JOIN: an agent registered before migrations/000026, or
	// one whose registrar has since been deleted, still has to appear. An
	// inner join here would make agents vanish from the directory for a
	// reason that has nothing to do with them.
	//
	// The email is resolved in this query rather than by the caller, because
	// the alternative is one lookup per row — the shape that stops working
	// long before it stops being tempting.
	rows, err := tx.Query(ctx,
		`SELECT sp.id, sp.name, COALESCE(sp.description, ''), sp.created_at, p.id,
		        sp.created_by, COALESCE(u.email, ''), COALESCE(u.display_name, '')
		 FROM service_principals sp
		 JOIN principals p ON p.service_principal_id = sp.id
		 LEFT JOIN users u ON u.id = sp.created_by
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
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.CreatedAt, &a.PrincipalID,
			&a.CreatedBy, &a.CreatedByEmail, &a.CreatedByName); err != nil {
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

// SessionClientType marks a client_credentials row as a browser session
// rather than a personal access token (docs/adr/0019). The two share the
// table, the token format and every verification and revocation path; this
// value is the only thing that distinguishes them, and exactly two readers
// interpret it — the token screen, which must not list sessions, and the
// session screen, which must not list tokens.
const SessionClientType = "session"

// ListTokensForPrincipal lists personal access tokens: every credential
// EXCEPT browser sessions. A session is not something a person manages by
// hand, and showing one on the token screen would invite them to revoke the
// credential they are currently using, from the screen that needs it.
func (s *Store) ListTokensForPrincipal(ctx context.Context, tx pgx.Tx, principalID uuid.UUID) ([]ClientCredential, error) {
	return s.listCredentials(ctx, tx,
		`WHERE principal_id = $1 AND client_type <> $2`, principalID, SessionClientType)
}

// ListSessionsForPrincipal lists only browser sessions, so a person can see
// where they are signed in and end a session they do not recognise.
func (s *Store) ListSessionsForPrincipal(ctx context.Context, tx pgx.Tx, principalID uuid.UUID) ([]ClientCredential, error) {
	return s.listCredentials(ctx, tx,
		`WHERE principal_id = $1 AND client_type = $2`, principalID, SessionClientType)
}

func (s *Store) listCredentials(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]ClientCredential, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, principal_id, client_type, token_hash, created_at, last_used_at, expires_at, revoked_at
		 FROM client_credentials `+where+` ORDER BY created_at DESC`, args...)
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

// ---------------------------------------------------------------------------
// Local passwords — docs/adr/0019
// ---------------------------------------------------------------------------

// SetPassword stores a PHC hash, replacing any previous one. An upsert
// rather than an insert: setting a password and changing a password are the
// same operation to everything above this layer, and splitting them would
// only create a way to get "already exists" from a screen whose user is
// holding the new password.
func (s *Store) SetPassword(ctx context.Context, tx pgx.Tx, userID uuid.UUID, passwordHash string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO user_passwords (user_id, password_hash, algorithm, updated_at)
		 VALUES ($1, $2, 'argon2id', now())
		 ON CONFLICT (user_id) DO UPDATE
		   SET password_hash = EXCLUDED.password_hash,
		       algorithm     = EXCLUDED.algorithm,
		       updated_at    = now()`,
		userID, passwordHash,
	)
	if err != nil {
		return fmt.Errorf("identity: set password: %w", err)
	}
	return nil
}

// GetPasswordHash returns the stored PHC string, or ErrNoLocalPassword when
// the user has none — a directory-managed account, or one created before a
// password was ever set. Separated from "wrong password" here because the
// caller needs to know whether to spend the decoy verification; it must not
// be separated in anything the caller then says out loud.
func (s *Store) GetPasswordHash(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (string, error) {
	var hash string
	err := tx.QueryRow(ctx, `SELECT password_hash FROM user_passwords WHERE user_id = $1`, userID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoLocalPassword
	}
	if err != nil {
		return "", fmt.Errorf("identity: get password: %w", err)
	}
	return hash, nil
}

// DeletePassword withdraws local login from an account, leaving any directory
// identity intact. Idempotent: no row is the end state being asked for.
func (s *Store) DeletePassword(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM user_passwords WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("identity: delete password: %w", err)
	}
	return nil
}

// HasAnyUser answers whether this installation has been set up at all. It is
// the one question the first-run endpoint may answer to an unauthenticated
// caller, and it is a count rather than a list on purpose — "is there anyone"
// is one bit, and an instance with no users has nothing to protect yet.
func (s *Store) HasAnyUser(ctx context.Context, tx pgx.Tx) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("identity: has any user: %w", err)
	}
	return exists, nil
}

// ---------------------------------------------------------------------------
// Directory identity — docs/adr/0019
// ---------------------------------------------------------------------------

// UserForDirectoryIdentity resolves a directory's own stable subject to a
// local user, or ErrNotFound when this subject has never signed in here.
// Keyed on the subject and never on the email: an email is a label a person
// can change and an administrator can reassign, and keying on it is how one
// person inherits another's projects.
func (s *Store) UserForDirectoryIdentity(ctx context.Context, tx pgx.Tx, provider, subject string) (uuid.UUID, error) {
	var userID uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT user_id FROM user_identities WHERE provider = $1 AND subject = $2`,
		provider, subject,
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("identity: user for directory identity: %w", err)
	}
	return userID, nil
}

// LinkDirectoryIdentity records that a directory subject is this local user.
// A conflict is ErrAlreadyExists rather than a silent re-point: a subject
// that already maps somewhere else means two people are being treated as one,
// and quietly moving the mapping would hand one of them the other's access.
func (s *Store) LinkDirectoryIdentity(ctx context.Context, tx pgx.Tx, provider, subject string, userID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO user_identities (provider, subject, user_id) VALUES ($1, $2, $3)`,
		provider, subject, userID,
	)
	if isUniqueViolation(err) {
		return ErrAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("identity: link directory identity: %w", err)
	}
	return nil
}

// DirectoryIdentitiesForUser names the directories an account is linked to —
// what the account screen shows, and what tells an admin why someone can sign
// in when no local password exists.
func (s *Store) DirectoryIdentitiesForUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT provider FROM user_identities WHERE user_id = $1 ORDER BY provider`, userID)
	if err != nil {
		return nil, fmt.Errorf("identity: directory identities: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("identity: directory identities: scan: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: directory identities: %w", err)
	}
	return out, nil
}

// PrincipalForUser resolves a user to the principal that acts for them.
// Every authentication path needs it: a password or a directory proves who a
// person is, and everything downstream — RLS, capabilities, audit — is keyed
// on the principal, never on the user (docs/adr/0004).
//
// A user with no principal row is ErrNotFound rather than a created one: the
// pairing is made once, at user creation, and inventing a second principal
// here would silently split one person's memberships across two identities.
func (s *Store) PrincipalForUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (Principal, error) {
	var p Principal
	err := tx.QueryRow(ctx,
		`SELECT id, kind, user_id, service_principal_id, created_at
		 FROM principals WHERE user_id = $1`, userID,
	).Scan(&p.ID, &p.Kind, &p.UserID, &p.ServicePrincipalID, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrNotFound
	}
	if err != nil {
		return Principal{}, fmt.Errorf("identity: principal for user: %w", err)
	}
	return p, nil
}
