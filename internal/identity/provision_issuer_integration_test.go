package identity_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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

	// Two sign-ins under different issuers both read the legacy NULL; the
	// one that adopts second must lose, not be signed in under an issuer
	// the link no longer has.
	t.Run("a race to adopt a legacy link has one winner", func(t *testing.T) {
		err := db.WithoutPrincipal(ctx, owner, func(tx pgx.Tx) error {
			u, err := store.CreateUser(ctx, tx, "raced@example.test", "Raced")
			if err != nil {
				return err
			}
			return store.LinkDirectoryIdentity(ctx, tx, "oidc", "", "sub-raced", u.ID)
		})
		if err != nil {
			t.Fatal(err)
		}

		bothRead := make(chan struct{})
		firstAdopted := make(chan struct{})
		readNull := func(tx pgx.Tx) error {
			link, err := store.DirectoryLink(ctx, tx, "oidc", "sub-raced")
			if err == nil && link.Issuer != "" {
				err = errors.New("the link already had an issuer")
			}
			return err
		}
		var wg sync.WaitGroup
		var firstErr, secondErr error
		ready := make(chan struct{}, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			firstErr = db.WithoutPrincipal(ctx, app, func(tx pgx.Tx) error {
				if err := readNull(tx); err != nil {
					return err
				}
				ready <- struct{}{}
				<-bothRead
				if err := store.AdoptDirectoryIssuer(ctx, tx, "oidc", "sub-raced", oldIssuer); err != nil {
					return err
				}
				// Adopted but not committed: the second UPDATE starts now
				// and has to wait on this row's lock.
				close(firstAdopted)
				time.Sleep(300 * time.Millisecond)
				return nil
			})
		}()
		go func() {
			defer wg.Done()
			secondErr = db.WithoutPrincipal(ctx, app, func(tx pgx.Tx) error {
				if err := readNull(tx); err != nil {
					return err
				}
				ready <- struct{}{}
				<-firstAdopted
				return store.AdoptDirectoryIssuer(ctx, tx, "oidc", "sub-raced", newIssuer)
			})
		}()
		<-ready
		<-ready
		close(bothRead)
		wg.Wait()

		if firstErr != nil {
			t.Fatalf("first adoption: %v", firstErr)
		}
		if !errors.Is(secondErr, identity.ErrIssuerMismatch) {
			t.Fatalf("second adoption under another issuer: want ErrIssuerMismatch, got %v", secondErr)
		}
		if got := issuerOf("sub-raced"); got != oldIssuer {
			t.Fatalf("issuer = %q, want the winner's %q", got, oldIssuer)
		}
		// The winner's issuer adopting again is a match, not a mismatch.
		err = db.WithoutPrincipal(ctx, app, func(tx pgx.Tx) error {
			return store.AdoptDirectoryIssuer(ctx, tx, "oidc", "sub-raced", oldIssuer)
		})
		if err != nil {
			t.Fatalf("re-adopting the same issuer: %v", err)
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
