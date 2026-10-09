package installation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/paging"
)

type fixture struct {
	t         *testing.T
	ctx       context.Context
	ownerPool *db.Pool
	appPool   *db.Pool
	store     *installation.Store
	identity  *identity.Store
	svc       *installation.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsns := dbtest.New(t)
	ctx := context.Background()

	ownerPool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}
	appPool, err := db.NewPool(ctx, dsns.App)
	if err != nil {
		t.Fatalf("connect as cc_app: %v", err)
	}

	store := installation.NewStore()
	identityStore := identity.NewStore()
	return &fixture{
		t: t, ctx: ctx, ownerPool: ownerPool, appPool: appPool,
		store: store, identity: identityStore, svc: installation.NewService(appPool, store, identityStore),
	}
}

// principal is a (principalID, userID) pair — installation_admins is keyed
// by user_id, but RLS session context is keyed by principal_id
// (docs/adr/0017, same shape as internal/organizations' test fixture).
type principal struct {
	principalID uuid.UUID
	userID      uuid.UUID
}

func (f *fixture) newUser(email string) principal {
	f.t.Helper()
	p := principal{principalID: uuid.New(), userID: uuid.New()}
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			p.userID, email, email); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, p.principalID, p.userID)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed user %s: %v", email, err)
	}
	return p
}

func (f *fixture) grant(userID uuid.UUID) {
	f.t.Helper()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := f.store.Grant(f.ctx, tx, userID, nil)
		return err
	})
	if err != nil {
		f.t.Fatalf("grant admin: %v", err)
	}
}

func TestGrantThenIsAdmin(t *testing.T) {
	f := newFixture(t)
	u := f.newUser("admin@example.test")

	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		isAdmin, err := f.store.IsAdmin(f.ctx, tx, u.userID)
		if err != nil {
			return err
		}
		if isAdmin {
			t.Error("IsAdmin before Grant = true, want false")
		}
		if _, err := f.store.Grant(f.ctx, tx, u.userID, nil); err != nil {
			return err
		}
		isAdmin, err = f.store.IsAdmin(f.ctx, tx, u.userID)
		if err != nil {
			return err
		}
		if !isAdmin {
			t.Error("IsAdmin after Grant = false, want true")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGrantTwiceIsAlreadyAdmin(t *testing.T) {
	f := newFixture(t)
	u := f.newUser("admin@example.test")
	f.grant(u.userID)

	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := f.store.Grant(f.ctx, tx, u.userID, nil)
		return err
	})
	if !errors.Is(err, installation.ErrAlreadyAdmin) {
		t.Fatalf("second Grant: got err=%v, want ErrAlreadyAdmin", err)
	}
}

func TestRevokeThenIsNotAdmin(t *testing.T) {
	f := newFixture(t)
	u := f.newUser("admin@example.test")
	f.grant(u.userID)

	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		if err := f.store.Revoke(f.ctx, tx, u.userID); err != nil {
			return err
		}
		isAdmin, err := f.store.IsAdmin(f.ctx, tx, u.userID)
		if err != nil {
			return err
		}
		if isAdmin {
			t.Error("IsAdmin after Revoke = true, want false")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRevokeNonAdminIsNotFound(t *testing.T) {
	f := newFixture(t)
	u := f.newUser("plain@example.test")

	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		return f.store.Revoke(f.ctx, tx, u.userID)
	})
	if !errors.Is(err, installation.ErrNotFound) {
		t.Fatalf("Revoke a non-admin: got err=%v, want ErrNotFound", err)
	}
}

func TestListReturnsGrantedAdmins(t *testing.T) {
	f := newFixture(t)
	a := f.newUser("a@example.test")
	b := f.newUser("b@example.test")
	f.grant(a.userID)
	f.grant(b.userID)

	list, err := f.svc.ListAdmins(f.ctx, a.principalID, a.userID)
	if err != nil {
		t.Fatalf("ListAdmins: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d admins, want 2: %+v", len(list), list)
	}
}

func TestListAdminsDeniedForNonAdmin(t *testing.T) {
	f := newFixture(t)
	plain := f.newUser("plain@example.test")

	_, err := f.svc.ListAdmins(f.ctx, plain.principalID, plain.userID)
	if !errors.Is(err, installation.ErrForbidden) {
		t.Fatalf("ListAdmins as a non-admin: got err=%v, want ErrForbidden", err)
	}
}

func TestCreateUserRequiresAdmin(t *testing.T) {
	f := newFixture(t)
	plain := f.newUser("plain@example.test")

	_, err := f.svc.CreateUser(f.ctx, plain.principalID, plain.userID, "new@example.test", "New User", nil)
	if !errors.Is(err, installation.ErrForbidden) {
		t.Fatalf("CreateUser as a non-admin: got err=%v, want ErrForbidden", err)
	}
}

func TestCreateUserAsAdmin(t *testing.T) {
	f := newFixture(t)
	admin := f.newUser("admin@example.test")
	f.grant(admin.userID)

	u, err := f.svc.CreateUser(f.ctx, admin.principalID, admin.userID, "new@example.test", "New User", nil)
	if err != nil {
		t.Fatalf("CreateUser as an admin: %v", err)
	}
	if u.Email != "new@example.test" {
		t.Errorf("u.Email = %q, want new@example.test", u.Email)
	}

	// The created user must actually be usable — resolvable via a real
	// Principal, not just a bare users row.
	err = db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		var principalID uuid.UUID
		return tx.QueryRow(f.ctx, `SELECT id FROM principals WHERE user_id = $1`, u.ID).Scan(&principalID)
	})
	if err != nil {
		t.Fatalf("created user has no principal: %v", err)
	}
}

func TestCreateServicePrincipalRequiresAdmin(t *testing.T) {
	f := newFixture(t)
	plain := f.newUser("plain@example.test")

	_, err := f.svc.CreateServicePrincipal(f.ctx, plain.principalID, plain.userID, "ci-agent", "")
	if !errors.Is(err, installation.ErrForbidden) {
		t.Fatalf("CreateServicePrincipal as a non-admin: got err=%v, want ErrForbidden", err)
	}
}

func TestCreateServicePrincipalAsAdmin(t *testing.T) {
	f := newFixture(t)
	admin := f.newUser("admin@example.test")
	f.grant(admin.userID)

	sp, err := f.svc.CreateServicePrincipal(f.ctx, admin.principalID, admin.userID, "ci-agent", "runs CI")
	if err != nil {
		t.Fatalf("CreateServicePrincipal as an admin: %v", err)
	}
	if sp.Name != "ci-agent" {
		t.Errorf("sp.Name = %q, want ci-agent", sp.Name)
	}

	err = db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		var principalID uuid.UUID
		return tx.QueryRow(f.ctx, `SELECT id FROM principals WHERE service_principal_id = $1`, sp.ID).Scan(&principalID)
	})
	if err != nil {
		t.Fatalf("created service principal has no principal: %v", err)
	}
}

func TestCreateUserWithAnInitialPassword(t *testing.T) {
	// The gap this closes: an administrator could create an account and had
	// no way to let anyone into it. The setup code is gone once the
	// installation has a user, `ccd user set-password` needs a shell, and
	// handing over a PAT makes the new person inherit the admin's credential.
	f := newFixture(t)
	admin := f.newUser("admin@example.test")
	f.grant(admin.userID)

	hash, err := identity.HashPassword("an initial password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	u, err := f.svc.CreateUser(f.ctx, admin.principalID, admin.userID, "hire@example.test", "New Hire", &hash)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// The password and the principal land in the same transaction as the
	// user, so there is no state where the account exists and cannot be used.
	principal, err := identity.AuthenticatePassword(f.ctx, f.appPool, "hire@example.test", "an initial password")
	if err != nil {
		t.Fatalf("the new user cannot sign in: %v", err)
	}
	if principal.UserID == nil || *principal.UserID != u.ID {
		t.Errorf("signed in as %v, want the created user %s", principal.UserID, u.ID)
	}
}

func TestCreateUserWithoutAPasswordLeavesNone(t *testing.T) {
	// Empty is a valid choice, not an incomplete form: an administrator
	// creating twenty accounts has no business inventing twenty passwords.
	// What must not happen is a row holding something unusable.
	f := newFixture(t)
	admin := f.newUser("admin@example.test")
	f.grant(admin.userID)

	u, err := f.svc.CreateUser(f.ctx, admin.principalID, admin.userID, "nopass@example.test", "No Password", nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	err = db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := identity.NewStore().GetPasswordHash(f.ctx, tx, u.ID)
		return err
	})
	if !errors.Is(err, identity.ErrNoLocalPassword) {
		t.Errorf("err = %v, want ErrNoLocalPassword", err)
	}
}

func TestSetUserPassword(t *testing.T) {
	f := newFixture(t)
	admin := f.newUser("admin@example.test")
	f.grant(admin.userID)
	target := f.newUser("target@example.test")
	plain := f.newUser("plain@example.test")

	hash, err := identity.HashPassword("a password set by an admin")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	t.Run("a non-admin cannot set anyone's password", func(t *testing.T) {
		// It is the power to sign in as them. The same capability already
		// issues a token for any principal, so this is not a new power — but
		// it must not be a weaker check than that one.
		err := f.svc.SetUserPassword(f.ctx, plain.principalID, plain.userID, target.userID, hash)
		if !errors.Is(err, installation.ErrForbidden) {
			t.Errorf("err = %v, want ErrForbidden", err)
		}
	})

	t.Run("an unknown user is not found, not a constraint error", func(t *testing.T) {
		err := f.svc.SetUserPassword(f.ctx, admin.principalID, admin.userID, uuid.New(), hash)
		if !errors.Is(err, identity.ErrNotFound) {
			t.Errorf("err = %v, want identity.ErrNotFound", err)
		}
	})

	t.Run("an admin sets it and the person can sign in", func(t *testing.T) {
		if err := f.svc.SetUserPassword(f.ctx, admin.principalID, admin.userID, target.userID, hash); err != nil {
			t.Fatalf("SetUserPassword: %v", err)
		}
		if _, err := identity.AuthenticatePassword(f.ctx, f.appPool, "target@example.test", "a password set by an admin"); err != nil {
			t.Errorf("the target cannot sign in with the password just set: %v", err)
		}
	})

	t.Run("it replaces an existing password", func(t *testing.T) {
		replacement, err := identity.HashPassword("the replacement password")
		if err != nil {
			t.Fatalf("HashPassword: %v", err)
		}
		if err := f.svc.SetUserPassword(f.ctx, admin.principalID, admin.userID, target.userID, replacement); err != nil {
			t.Fatalf("SetUserPassword: %v", err)
		}
		if _, err := identity.AuthenticatePassword(f.ctx, f.appPool, "target@example.test", "the replacement password"); err != nil {
			t.Errorf("the replacement does not work: %v", err)
		}
		if _, err := identity.AuthenticatePassword(f.ctx, f.appPool, "target@example.test", "a password set by an admin"); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Errorf("the old password still works: err = %v", err)
		}
	})
}

func TestServicePrincipalRecordsWhoRegisteredIt(t *testing.T) {
	// Raised by the maintainer: a directory of agents was a list of names
	// with no way to ask whose they are. The value was never missing — the
	// capability check already resolves the calling user — only discarded
	// (migrations/000026).
	f := newFixture(t)
	admin := f.newUser("registrar@example.test")
	f.grant(admin.userID)

	sp, err := f.svc.CreateServicePrincipal(f.ctx, admin.principalID, admin.userID, "ci-agent", "runs CI")
	if err != nil {
		t.Fatalf("CreateServicePrincipal: %v", err)
	}
	if sp.CreatedBy == nil || *sp.CreatedBy != admin.userID {
		t.Errorf("sp.CreatedBy = %v, want %s", sp.CreatedBy, admin.userID)
	}

	// The listing resolves it to a person in the same query, because the
	// alternative is one lookup per row.
	page, err := f.svc.ListServicePrincipals(f.ctx, admin.principalID, admin.userID, paging.Options{})
	if err != nil {
		t.Fatalf("ListServicePrincipals: %v", err)
	}
	var found *identity.DirectoryAgent
	for i := range page.Items {
		if page.Items[i].ID == sp.ID {
			found = &page.Items[i]
		}
	}
	if found == nil {
		t.Fatal("the agent just created is not in the directory")
	}
	if found.CreatedByEmail != "registrar@example.test" {
		t.Errorf("CreatedByEmail = %q, want registrar@example.test", found.CreatedByEmail)
	}
}

func TestUnattributedAgentStillAppearsInTheDirectory(t *testing.T) {
	// An agent registered before migrations/000026, or one whose registrar
	// has since been deleted, must still be listed. The LEFT JOIN is what
	// makes that true; an inner join would make agents vanish for a reason
	// that has nothing to do with them.
	f := newFixture(t)
	admin := f.newUser("admin@example.test")
	f.grant(admin.userID)

	var orphanID uuid.UUID
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		sp, err := identity.NewStore().CreateServicePrincipal(f.ctx, tx, "legacy-agent", "", nil)
		if err != nil {
			return err
		}
		orphanID = sp.ID
		_, err = identity.NewStore().CreatePrincipalForService(f.ctx, tx, sp.ID)
		return err
	})
	if err != nil {
		t.Fatalf("seed an unattributed agent: %v", err)
	}

	page, err := f.svc.ListServicePrincipals(f.ctx, admin.principalID, admin.userID, paging.Options{})
	if err != nil {
		t.Fatalf("ListServicePrincipals: %v", err)
	}
	for _, a := range page.Items {
		if a.ID != orphanID {
			continue
		}
		if a.CreatedBy != nil {
			t.Errorf("CreatedBy = %v, want nil", a.CreatedBy)
		}
		// Empty rather than a placeholder: the screen decides how to say
		// "nobody recorded this", and a fake name here would be a false
		// statement about a real person.
		if a.CreatedByEmail != "" {
			t.Errorf("CreatedByEmail = %q, want empty", a.CreatedByEmail)
		}
		return
	}
	t.Fatal("an agent with no recorded registrar is missing from the directory")
}

func TestIssueListRevokeCredentialForOtherPrincipal(t *testing.T) {
	f := newFixture(t)
	admin := f.newUser("admin@example.test")
	f.grant(admin.userID)
	target := f.newUser("target@example.test")

	token, cred, err := f.svc.IssueCredentialFor(f.ctx, admin.principalID, admin.userID, target.principalID, "cli", nil)
	if err != nil {
		t.Fatalf("IssueCredentialFor: %v", err)
	}
	if token == "" {
		t.Fatal("IssueCredentialFor returned an empty token")
	}
	if cred.PrincipalID != target.principalID {
		t.Errorf("cred.PrincipalID = %s, want %s (the target, not the admin)", cred.PrincipalID, target.principalID)
	}

	creds, err := f.svc.ListCredentialsFor(f.ctx, admin.principalID, admin.userID, target.principalID)
	if err != nil {
		t.Fatalf("ListCredentialsFor: %v", err)
	}
	if len(creds) != 1 || creds[0].ID != cred.ID {
		t.Fatalf("got %d credentials, want the one just issued: %+v", len(creds), creds)
	}

	if err := f.svc.RevokeCredentialFor(f.ctx, admin.principalID, admin.userID, cred.ID); err != nil {
		t.Fatalf("RevokeCredentialFor: %v", err)
	}

	creds, err = f.svc.ListCredentialsFor(f.ctx, admin.principalID, admin.userID, target.principalID)
	if err != nil {
		t.Fatalf("ListCredentialsFor after revoke: %v", err)
	}
	if len(creds) != 1 || creds[0].RevokedAt == nil {
		t.Fatalf("credential not marked revoked: %+v", creds)
	}
}

func TestCredentialManagementDeniedForNonAdmin(t *testing.T) {
	f := newFixture(t)
	plain := f.newUser("plain@example.test")
	target := f.newUser("target@example.test")

	if _, _, err := f.svc.IssueCredentialFor(f.ctx, plain.principalID, plain.userID, target.principalID, "cli", nil); !errors.Is(err, installation.ErrForbidden) {
		t.Fatalf("IssueCredentialFor as a non-admin: got err=%v, want ErrForbidden", err)
	}
	if _, err := f.svc.ListCredentialsFor(f.ctx, plain.principalID, plain.userID, target.principalID); !errors.Is(err, installation.ErrForbidden) {
		t.Fatalf("ListCredentialsFor as a non-admin: got err=%v, want ErrForbidden", err)
	}
	if err := f.svc.RevokeCredentialFor(f.ctx, plain.principalID, plain.userID, uuid.New()); !errors.Is(err, installation.ErrForbidden) {
		t.Fatalf("RevokeCredentialFor as a non-admin: got err=%v, want ErrForbidden", err)
	}
}
