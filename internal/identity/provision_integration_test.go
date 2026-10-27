package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/identity"
)

func directoryIdentity(subject, email, name string) identity.DirectoryIdentity {
	return identity.DirectoryIdentity{Provider: "ldap", Subject: subject, Email: email, DisplayName: name}
}

func TestProvisionDirectoryUser(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}
	store := identity.NewStore()

	t.Run("creates the account on first sign-in", func(t *testing.T) {
		id := directoryIdentity("uuid-alice", "alice@example.test", "Alice Ashworth")
		principal, err := identity.ProvisionDirectoryUser(ctx, pool, id)
		if err != nil {
			t.Fatalf("ProvisionDirectoryUser: %v", err)
		}
		if principal.ID == uuid.Nil {
			t.Fatal("no principal was returned")
		}

		var email, name string
		err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			u, err := store.GetUser(ctx, tx, *principal.UserID)
			email, name = u.Email, u.DisplayName
			return err
		})
		if err != nil {
			t.Fatalf("read the created user: %v", err)
		}
		if email != "alice@example.test" || name != "Alice Ashworth" {
			t.Errorf("created %q / %q, want alice@example.test / Alice Ashworth", email, name)
		}
	})

	t.Run("is idempotent, and keyed on the subject rather than the email", func(t *testing.T) {
		first, err := identity.ProvisionDirectoryUser(ctx, pool,
			directoryIdentity("uuid-bob", "bob@example.test", "Bob Bellweather"))
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		// The same person after an administrator changed their address in the
		// directory. It must still be the same account: keying on the email
		// would create a second one and leave their projects behind in the
		// first.
		second, err := identity.ProvisionDirectoryUser(ctx, pool,
			directoryIdentity("uuid-bob", "robert@example.test", "Robert Bellweather"))
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		if first.ID != second.ID {
			t.Errorf("a changed email produced a new principal: %s then %s", first.ID, second.ID)
		}
	})

	t.Run("links to a local account that already holds the address", func(t *testing.T) {
		// The case that matters for a real rollout: an administrator
		// pre-creates people, or they had local passwords before the
		// directory was configured. Without linking, directory sign-in would
		// fail on the unique email for every one of them.
		var existing uuid.UUID
		err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			u, err := store.CreateUser(ctx, tx, "preexisting@example.test", "Pre Existing")
			if err != nil {
				return err
			}
			existing = u.ID
			_, err = store.CreatePrincipalForUser(ctx, tx, u.ID)
			return err
		})
		if err != nil {
			t.Fatalf("seed a local account: %v", err)
		}

		principal, err := identity.ProvisionDirectoryUser(ctx, pool,
			directoryIdentity("uuid-pre", "preexisting@example.test", "Pre Existing"))
		if err != nil {
			t.Fatalf("ProvisionDirectoryUser: %v", err)
		}
		if principal.UserID == nil || *principal.UserID != existing {
			t.Errorf("provisioned a new user instead of linking: got %v, want %s", principal.UserID, existing)
		}

		// And the link is recorded, so the next sign-in resolves by subject
		// and never looks at the email again.
		var providers []string
		err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			var err error
			providers, err = store.DirectoryIdentitiesForUser(ctx, tx, existing)
			return err
		})
		if err != nil {
			t.Fatalf("read directory identities: %v", err)
		}
		if len(providers) != 1 || providers[0] != "ldap" {
			t.Errorf("linked providers = %v, want [ldap]", providers)
		}
	})

	t.Run("matches an existing address case-insensitively", func(t *testing.T) {
		// A directory records addresses however its administrator typed them,
		// and migrations/000025 made our own lookup case-insensitive. If this
		// regressed, the same person would get a second account.
		err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			u, err := store.CreateUser(ctx, tx, "mixed@example.test", "Mixed Case")
			if err != nil {
				return err
			}
			_, err = store.CreatePrincipalForUser(ctx, tx, u.ID)
			return err
		})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		principal, err := identity.ProvisionDirectoryUser(ctx, pool,
			directoryIdentity("uuid-mixed", "Mixed@Example.Test", "Mixed Case"))
		if err != nil {
			t.Fatalf("ProvisionDirectoryUser: %v", err)
		}
		var email string
		err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			u, err := store.GetUser(ctx, tx, *principal.UserID)
			email = u.Email
			return err
		})
		if err != nil {
			t.Fatalf("read user: %v", err)
		}
		// The original row, not a second one — and its address is left as it
		// was, because the local record is not the directory's to rewrite.
		if email != "mixed@example.test" {
			t.Errorf("email = %q, want the existing mixed@example.test", email)
		}
	})

	t.Run("grants nothing", func(t *testing.T) {
		// A directory proves who someone is, not what they may do
		// (docs/adr/0019). If this ever starts granting membership, every
		// person in the directory silently gains access to something.
		principal, err := identity.ProvisionDirectoryUser(ctx, pool,
			directoryIdentity("uuid-plain", "plain@example.test", "Plain Person"))
		if err != nil {
			t.Fatalf("ProvisionDirectoryUser: %v", err)
		}
		var admins, memberships int
		err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM installation_admins WHERE user_id = $1`, *principal.UserID).Scan(&admins); err != nil {
				return err
			}
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM project_members WHERE principal_id = $1`, principal.ID).Scan(&memberships)
		})
		if err != nil {
			t.Fatalf("count grants: %v", err)
		}
		if admins != 0 || memberships != 0 {
			t.Errorf("provisioning granted %d admin row(s) and %d membership(s), want none", admins, memberships)
		}
	})

	t.Run("refuses an identity with no email", func(t *testing.T) {
		if _, err := identity.ProvisionDirectoryUser(ctx, pool,
			directoryIdentity("uuid-nomail", "", "No Mail")); err == nil {
			t.Error("accepted an identity with no email address")
		}
	})

	t.Run("refuses an identity with no subject", func(t *testing.T) {
		if _, err := identity.ProvisionDirectoryUser(ctx, pool,
			directoryIdentity("", "nosubject@example.test", "No Subject")); err == nil {
			t.Error("accepted an identity with no subject")
		}
	})
}

// Without CreateMissing, provisioning reaches only accounts that exist —
// linked already, or created by an administrator under the same address —
// and refuses the rest with ErrNoAccount, creating nothing (docs/adr/0020).
func TestProvisionDirectoryUserWithoutCreate(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}
	store := identity.NewStore()
	linkOnly := identity.ProvisionPolicy{CreateMissing: false}

	t.Run("refuses someone with no account and creates nothing", func(t *testing.T) {
		_, err := identity.ProvisionDirectoryUserWith(ctx, pool,
			directoryIdentity("uuid-nobody", "nobody@example.test", "Nobody"), linkOnly)
		if !errors.Is(err, identity.ErrNoAccount) {
			t.Fatalf("want ErrNoAccount, got %v", err)
		}
		err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			_, err := store.GetUserByEmail(ctx, tx, "nobody@example.test")
			return err
		})
		if !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("a user was created anyway (lookup: %v)", err)
		}
		// Nor a dangling link that would make the next attempt case 1.
		err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			_, err := store.UserForDirectoryIdentity(ctx, tx, "ldap", "uuid-nobody")
			return err
		})
		if !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("a link was recorded for a refused identity (lookup: %v)", err)
		}
	})

	t.Run("links an account an administrator created", func(t *testing.T) {
		var userID uuid.UUID
		err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			u, err := store.CreateUser(ctx, tx, "precreated@example.test", "Pre Created")
			userID = u.ID
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		principal, err := identity.ProvisionDirectoryUserWith(ctx, pool,
			directoryIdentity("uuid-pre", "precreated@example.test", "Pre Created"), linkOnly)
		if err != nil {
			t.Fatalf("ProvisionDirectoryUserWith: %v", err)
		}
		if principal.UserID == nil || *principal.UserID != userID {
			t.Fatalf("linked to %v, want the administrator's account %v", principal.UserID, userID)
		}

		// And once linked, the link is what is used — even with no account
		// creation allowed and a changed address at the provider.
		again, err := identity.ProvisionDirectoryUserWith(ctx, pool,
			directoryIdentity("uuid-pre", "renamed@example.test", "Pre Created"), linkOnly)
		if err != nil || again.ID != principal.ID {
			t.Fatalf("second sign-in: principal %v, error %v; want %v", again.ID, err, principal.ID)
		}
	})
}
