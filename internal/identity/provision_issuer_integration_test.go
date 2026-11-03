package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/identity"
)

// The issuer beside a link (migrations/000034). Run as cc_app, the role the
// server uses, because adopting an issuer is an UPDATE that role is granted
// on one column only.
func TestProvisionDirectoryUserRecordsAndChecksTheIssuer(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	owner, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatal(err)
	}
	app, err := db.NewPool(ctx, dsns.App)
	if err != nil {
		t.Fatal(err)
	}
	store := identity.NewStore()
	const oldIssuer, newIssuer = "https://old.example.test/realms/x", "https://new.example.test/realms/x"
	oidcIdentity := func(subject, email, issuer string) identity.DirectoryIdentity {
		return identity.DirectoryIdentity{Provider: "oidc", Subject: subject, Email: email, DisplayName: email, Issuer: issuer}
	}
	issuerOf := func(subject string) string {
		t.Helper()
		var link identity.DirectoryLinkRow
		err := db.WithoutPrincipal(ctx, owner, func(tx pgx.Tx) error {
			var err error
			link, err = store.DirectoryLink(ctx, tx, "oidc", subject)
			return err
		})
		if err != nil {
			t.Fatalf("read link %s: %v", subject, err)
		}
		return link.Issuer
	}

	t.Run("a new link records the issuer", func(t *testing.T) {
		if _, err := identity.ProvisionDirectoryUser(ctx, app, oidcIdentity("sub-a", "a@example.test", oldIssuer)); err != nil {
			t.Fatal(err)
		}
		if got := issuerOf("sub-a"); got != oldIssuer {
			t.Fatalf("issuer = %q, want %q", got, oldIssuer)
		}
	})

	t.Run("the same subject under another issuer is refused", func(t *testing.T) {
		_, err := identity.ProvisionDirectoryUser(ctx, app, oidcIdentity("sub-a", "someone-else@example.test", newIssuer))
		if !errors.Is(err, identity.ErrIssuerMismatch) {
			t.Fatalf("want ErrIssuerMismatch, got %v", err)
		}
		if got := issuerOf("sub-a"); got != oldIssuer {
			t.Fatalf("a refused sign-in changed the recorded issuer to %q", got)
		}
	})

	t.Run("a link from before the migration adopts the issuer once", func(t *testing.T) {
		// A pre-000034 row: no issuer.
		err := db.WithoutPrincipal(ctx, owner, func(tx pgx.Tx) error {
			u, err := store.CreateUser(ctx, tx, "legacy@example.test", "Legacy")
			if err != nil {
				return err
			}
			if _, err := store.CreatePrincipalForUser(ctx, tx, u.ID); err != nil {
				return err
			}
			return store.LinkDirectoryIdentity(ctx, tx, "oidc", "", "sub-legacy", u.ID)
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := issuerOf("sub-legacy"); got != "" {
			t.Fatalf("legacy row has issuer %q", got)
		}

		if _, err := identity.ProvisionDirectoryUser(ctx, app, oidcIdentity("sub-legacy", "legacy@example.test", oldIssuer)); err != nil {
			t.Fatalf("legacy sign-in: %v", err)
		}
		if got := issuerOf("sub-legacy"); got != oldIssuer {
			t.Fatalf("issuer after first sign-in = %q, want %q", got, oldIssuer)
		}
		// Adopted means fixed: the next issuer is a different one.
		_, err = identity.ProvisionDirectoryUser(ctx, app, oidcIdentity("sub-legacy", "legacy@example.test", newIssuer))
		if !errors.Is(err, identity.ErrIssuerMismatch) {
			t.Fatalf("want ErrIssuerMismatch after adoption, got %v", err)
		}
	})

	t.Run("LDAP links carry no issuer and need none", func(t *testing.T) {
		ldap := identity.DirectoryIdentity{Provider: "ldap", Subject: "entry-uuid", Email: "l@example.test", DisplayName: "L"}
		for range 2 {
			if _, err := identity.ProvisionDirectoryUser(ctx, app, ldap); err != nil {
				t.Fatal(err)
			}
		}
	})
}
