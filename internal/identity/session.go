package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
)

// ErrInvalidCredentials is what every failed sign-in returns, whatever
// actually went wrong: no such email, no local password, wrong password, or a
// user with no principal. The caller cannot tell them apart because the
// person presenting a guess must not be able to either — "no account with
// that email" is an answer to a question an attacker is asking, and answering
// it turns the login form into an account directory.
var ErrInvalidCredentials = errors.New("identity: invalid credentials")

// SessionLifetime bounds how long a browser session stays valid without
// signing in again. Twelve hours is a working day: long enough that nobody is
// re-authenticating over lunch, short enough that a session left open on a
// machine someone no longer has is not still valid next week.
//
// It is an absolute expiry, not an idle timeout. An idle timeout needs a
// write on every request to be honest about what "idle" means, and
// last_used_at is already written best-effort — which is the right trade for
// telling a person where they are signed in, and the wrong one for deciding
// whether a credential is still valid.
const SessionLifetime = 12 * time.Hour

// AuthenticatePassword verifies an email and password and returns the
// principal that acts for that person.
//
// Every failure path costs an argon2id verification, including the ones where
// no hash was found. Without that, the response time tells an unauthenticated
// caller whether an email has an account here — an enumeration oracle that a
// rate limit slows but does not remove.
func AuthenticatePassword(ctx context.Context, pool *db.Pool, email, password string) (Principal, error) {
	store := NewStore()

	var (
		principal Principal
		hash      string
		found     bool
	)
	// WithoutPrincipal by necessity: there is no principal yet — establishing
	// one is what this function does. These tables carry no RLS for exactly
	// this reason (migrations/000025).
	err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		user, err := store.GetUserByEmail(ctx, tx, email)
		if errors.Is(err, ErrNotFound) {
			return nil // found stays false; the decoy runs below
		}
		if err != nil {
			return err
		}

		hash, err = store.GetPasswordHash(ctx, tx, user.ID)
		if errors.Is(err, ErrNoLocalPassword) {
			return nil
		}
		if err != nil {
			return err
		}

		principal, err = store.PrincipalForUser(ctx, tx, user.ID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return nil
	})
	if err != nil {
		return Principal{}, fmt.Errorf("identity: authenticate password: %w", err)
	}

	if !found {
		VerifyAgainstDecoy(password)
		return Principal{}, ErrInvalidCredentials
	}
	if !VerifyPassword(password, hash) {
		return Principal{}, ErrInvalidCredentials
	}
	return principal, nil
}

// IssueSession creates a browser session and returns its token exactly once,
// to be set as an HttpOnly cookie and never handed to JavaScript
// (docs/adr/0019).
//
// It is an ordinary client_credentials row — see SessionClientType — so it is
// verified by Authenticate, revoked by RevokeCredentialByID, expired by
// ClientCredential.Active and rate-limited by the limiter already keyed on
// credential id. Nothing here re-implements any of those.
func IssueSession(ctx context.Context, pool *db.Pool, principalID uuid.UUID) (token string, cred ClientCredential, err error) {
	expiresAt := time.Now().Add(SessionLifetime)
	store := NewStore()
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		token, cred, err = store.IssueCredential(ctx, tx, principalID, SessionClientType, &expiresAt)
		return err
	})
	if err != nil {
		return "", ClientCredential{}, fmt.Errorf("identity: issue session: %w", err)
	}
	return token, cred, nil
}

// EndSession revokes one session. Signing out has to revoke rather than only
// clear the cookie: a cookie the browser forgets is still a valid credential
// to anyone who captured it, and "log out" has to mean the token stops
// working, not that this browser stopped sending it.
func EndSession(ctx context.Context, pool *db.Pool, credentialID uuid.UUID) error {
	store := NewStore()
	err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		return store.RevokeCredentialByID(ctx, tx, credentialID)
	})
	// Already revoked or already gone is the end state being asked for.
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("identity: end session: %w", err)
	}
	return nil
}
