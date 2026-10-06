package identity_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/identity"
)

// seedUserWithPassword creates a user, its principal and a local password the
// way the setup and admin paths will, through the store rather than by hand,
// so this test fails if those paths stop agreeing with the schema.
func seedUserWithPassword(t *testing.T, ctx context.Context, pool *db.Pool, email, password string) uuid.UUID {
	t.Helper()
	store := identity.NewStore()
	var principalID uuid.UUID
	err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		user, err := store.CreateUser(ctx, tx, email, "Test Person")
		if err != nil {
			return err
		}
		principal, err := store.CreatePrincipalForUser(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		principalID = principal.ID
		hash, err := identity.HashPassword(password)
		if err != nil {
			return err
		}
		return store.SetPassword(ctx, tx, user.ID, hash)
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return principalID
}

func TestAuthenticatePassword(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}

	const (
		email    = "person@example.test"
		password = "correct horse battery staple"
	)
	principalID := seedUserWithPassword(t, ctx, pool, email, password)

	t.Run("the right password yields the person's principal", func(t *testing.T) {
		got, err := identity.AuthenticatePassword(ctx, pool, email, password)
		if err != nil {
			t.Fatalf("AuthenticatePassword: %v", err)
		}
		if got.ID != principalID {
			t.Errorf("principal = %s, want %s", got.ID, principalID)
		}
	})

	t.Run("the email is matched case-insensitively", func(t *testing.T) {
		// Someone who capitalises their own address must still get in —
		// migrations/000025's functional unique index is what makes this
		// unambiguous rather than merely lenient.
		if _, err := identity.AuthenticatePassword(ctx, pool, strings.ToUpper(email), password); err != nil {
			t.Errorf("a capitalised email was rejected: %v", err)
		}
	})

	t.Run("every failure is the same error", func(t *testing.T) {
		// The point is not that these fail; it is that a caller — and so
		// anyone reading a response — cannot tell WHICH of them happened.
		for name, attempt := range map[string][2]string{
			"wrong password":    {email, "wrong password entirely"},
			"no such email":     {"nobody@example.test", password},
			"empty password":    {email, ""},
			"empty email":       {"", password},
			"password as email": {password, password},
		} {
			_, err := identity.AuthenticatePassword(ctx, pool, attempt[0], attempt[1])
			if !errors.Is(err, identity.ErrInvalidCredentials) {
				t.Errorf("%s: err = %v, want ErrInvalidCredentials", name, err)
			}
		}
	})

	t.Run("a user with no local password cannot sign in with one", func(t *testing.T) {
		store := identity.NewStore()
		var userID uuid.UUID
		err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			user, err := store.CreateUser(ctx, tx, "directory-only@example.test", "Directory Person")
			if err != nil {
				return err
			}
			userID = user.ID
			_, err = store.CreatePrincipalForUser(ctx, tx, user.ID)
			return err
		})
		if err != nil {
			t.Fatalf("seed directory-only user: %v", err)
		}

		if _, err := identity.AuthenticatePassword(ctx, pool, "directory-only@example.test", password); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Errorf("err = %v, want ErrInvalidCredentials", err)
		}

		// And the absence is visible as an absence, not as an empty string
		// that something downstream might compare against.
		err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			_, err := store.GetPasswordHash(ctx, tx, userID)
			return err
		})
		if !errors.Is(err, identity.ErrNoLocalPassword) {
			t.Errorf("GetPasswordHash err = %v, want ErrNoLocalPassword", err)
		}
	})
}

func TestSetPasswordReplacesTheOldOne(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}

	const email = "rotates@example.test"
	const first = "first password here"
	const second = "second password here"
	seedUserWithPassword(t, ctx, pool, email, first)

	store := identity.NewStore()
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		user, err := store.GetUserByEmail(ctx, tx, email)
		if err != nil {
			return err
		}
		hash, err := identity.HashPassword(second)
		if err != nil {
			return err
		}
		return store.SetPassword(ctx, tx, user.ID, hash)
	})
	if err != nil {
		t.Fatalf("set second password: %v", err)
	}

	if _, err := identity.AuthenticatePassword(ctx, pool, email, second); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
	if _, err := identity.AuthenticatePassword(ctx, pool, email, first); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("the old password still works: err = %v", err)
	}
}

func TestSessionIsAnOrdinaryCredential(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}

	principalID := seedUserWithPassword(t, ctx, pool, "session@example.test", "correct horse battery staple")

	token, cred, err := identity.IssueSession(ctx, pool, principalID)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if cred.ClientType != identity.SessionClientType {
		t.Errorf("client_type = %q, want %q", cred.ClientType, identity.SessionClientType)
	}
	if cred.ExpiresAt == nil {
		t.Error("a session was issued with no expiry")
	}

	// The whole point of ADR 0019's first decision: a session verifies
	// through the one chokepoint every adapter already uses.
	verified, err := identity.Authenticate(ctx, pool, token)
	if err != nil {
		t.Fatalf("Authenticate(session token): %v", err)
	}
	if verified.PrincipalID != principalID {
		t.Errorf("principal = %s, want %s", verified.PrincipalID, principalID)
	}

	// Signing out must make the token stop working, not merely make this
	// browser stop sending it.
	if err := identity.EndSession(ctx, pool, cred.ID); err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	if _, err := identity.Authenticate(ctx, pool, token); !errors.Is(err, identity.ErrInvalidToken) {
		t.Errorf("a revoked session still authenticates: err = %v", err)
	}
	// Signing out twice is the same end state, not an error.
	if err := identity.EndSession(ctx, pool, cred.ID); err != nil {
		t.Errorf("EndSession twice: %v", err)
	}
}

func TestSessionsAndTokensAreListedSeparately(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}

	principalID := seedUserWithPassword(t, ctx, pool, "both@example.test", "correct horse battery staple")
	store := identity.NewStore()

	// One PAT and one session for the same principal.
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		_, _, err := store.IssueCredential(ctx, tx, principalID, "claude-code", nil)
		return err
	})
	if err != nil {
		t.Fatalf("issue PAT: %v", err)
	}
	if _, _, err := identity.IssueSession(ctx, pool, principalID); err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	var tokens, sessions, all []identity.ClientCredential
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		if tokens, err = store.ListTokensForPrincipal(ctx, tx, principalID); err != nil {
			return err
		}
		if sessions, err = store.ListSessionsForPrincipal(ctx, tx, principalID); err != nil {
			return err
		}
		all, err = store.ListCredentialsForPrincipal(ctx, tx, principalID)
		return err
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	// The token screen must not offer someone the credential they are
	// currently signed in with, and the session screen must not list a PAT
	// an agent depends on.
	if len(tokens) != 1 || tokens[0].ClientType != "claude-code" {
		t.Errorf("tokens = %+v, want exactly the PAT", clientTypes(tokens))
	}
	if len(sessions) != 1 || sessions[0].ClientType != identity.SessionClientType {
		t.Errorf("sessions = %+v, want exactly the session", clientTypes(sessions))
	}
	// The admin credential view stays unfiltered: a session is exactly what
	// an incident asks about.
	if len(all) != 2 {
		t.Errorf("the admin view listed %d credential(s), want 2: %v", len(all), clientTypes(all))
	}
}

func clientTypes(creds []identity.ClientCredential) []string {
	out := make([]string, len(creds))
	for i, c := range creds {
		out[i] = c.ClientType
	}
	return out
}
