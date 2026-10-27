package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
)

// DirectoryIdentity is what a directory asserted about a person, reduced to
// the fields provisioning needs. It mirrors internal/directory.Identity
// deliberately rather than importing it: identity must not depend on a
// particular directory implementation, and the OIDC backend will fill the same
// three fields from entirely different claims.
type DirectoryIdentity struct {
	Provider    string
	Subject     string
	Email       string
	DisplayName string
}

// ProvisionDirectoryUser turns a directory assertion into a local principal,
// creating the user on first sign-in.
//
// Three cases, in this order, and the order is the whole design:
//
//  1. The (provider, subject) pair is already linked. This is every sign-in
//     after the first, and it is the only case keyed on something the person
//     cannot change.
//
//  2. No link, but a local user holds this email. The directory identity is
//     attached to that existing account rather than refused. Without this, an
//     administrator who pre-creates accounts breaks directory sign-in for
//     everyone they create, and so does anyone who had a local password
//     before the directory was configured.
//
//  3. Neither. Create the user, its principal and the link.
//
// The risk in case 2, stated plainly: whoever controls the directory can take
// over a local account by putting its email on a directory entry. That is
// already true of any installation that trusts a directory to say who people
// are — it is what configuring one means — but it is the reason the link is
// keyed on the subject afterwards and never re-derived from the email.
//
// No role, no membership, no admin grant is given here. A directory proves who
// someone is, not what they may do (docs/adr/0019).
func ProvisionDirectoryUser(ctx context.Context, pool *db.Pool, id DirectoryIdentity) (Principal, error) {
	return ProvisionDirectoryUserWith(ctx, pool, id, ProvisionPolicy{CreateMissing: true})
}

// ErrNoAccount means a directory authenticated someone who has no account
// here, and the policy forbids creating one. Distinct from a failed sign-in:
// the person proved who they are, and what they need is an administrator.
var ErrNoAccount = errors.New("identity: no account for this directory identity, and creating one is not allowed")

// ProvisionPolicy is what a directory is trusted to do beyond proving who
// someone is (docs/adr/0020).
type ProvisionPolicy struct {
	// CreateMissing allows case 3 — a brand-new user on first sign-in. Right
	// for a closed directory (a company's LDAP, its own Keycloak); wrong for
	// an issuer anyone can hold an account at, where it would be open
	// self-registration. Without it, cases 1 and 2 still work: an existing
	// link, or an account an administrator created under the same address.
	CreateMissing bool
}

// ProvisionDirectoryUserWith is ProvisionDirectoryUser under an explicit
// policy.
func ProvisionDirectoryUserWith(ctx context.Context, pool *db.Pool, id DirectoryIdentity, policy ProvisionPolicy) (Principal, error) {
	if id.Provider == "" || id.Subject == "" {
		return Principal{}, errors.New("identity: provision: provider and subject are required")
	}
	if id.Email == "" {
		// The directory layer refuses this earlier with a message naming the
		// entry; this is the backstop, because a user row without an email
		// cannot exist and a synthesised one would be a lie the whole
		// interface then repeats.
		return Principal{}, errors.New("identity: provision: email is required")
	}
	name := id.DisplayName
	if name == "" {
		name = id.Email
	}

	store := NewStore()
	var principal Principal
	// One transaction: a user created without its link would be provisioned
	// again on the next sign-in and fail on the unique email, locking the
	// person out of an account that exists because of them.
	err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		userID, err := store.UserForDirectoryIdentity(ctx, tx, id.Provider, id.Subject)
		switch {
		case err == nil:
			// Case 1.
		case errors.Is(err, ErrNotFound):
			userID, err = linkOrCreate(ctx, tx, store, id, name, policy)
			if err != nil {
				return err
			}
		default:
			return err
		}

		principal, err = store.PrincipalForUser(ctx, tx, userID)
		if errors.Is(err, ErrNotFound) {
			// A user row with no principal predates this path or was made by
			// hand. Pairing one now is correct — the alternative is an
			// account nobody can ever sign in to.
			principal, err = store.CreatePrincipalForUser(ctx, tx, userID)
		}
		return err
	})
	if err != nil {
		return Principal{}, fmt.Errorf("identity: provision directory user: %w", err)
	}
	return principal, nil
}

func linkOrCreate(ctx context.Context, tx pgx.Tx, store *Store, id DirectoryIdentity, name string, policy ProvisionPolicy) (uuid.UUID, error) {
	// Case 2: an account already exists under this address. GetUserByEmail is
	// case-insensitive (migrations/000025), which matters here because a
	// directory records addresses however its administrator typed them.
	existing, err := store.GetUserByEmail(ctx, tx, id.Email)
	switch {
	case err == nil:
		if err := store.LinkDirectoryIdentity(ctx, tx, id.Provider, id.Subject, existing.ID); err != nil {
			return uuid.Nil, err
		}
		return existing.ID, nil
	case errors.Is(err, ErrNotFound):
		// Case 3.
		if !policy.CreateMissing {
			return uuid.Nil, ErrNoAccount
		}
	default:
		return uuid.Nil, err
	}

	created, err := store.CreateUser(ctx, tx, id.Email, name)
	if err != nil {
		return uuid.Nil, err
	}
	if err := store.LinkDirectoryIdentity(ctx, tx, id.Provider, id.Subject, created.ID); err != nil {
		return uuid.Nil, err
	}
	return created.ID, nil
}
