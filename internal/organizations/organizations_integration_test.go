package organizations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/organizations"
)

type fixture struct {
	t         *testing.T
	ctx       context.Context
	ownerPool *db.Pool
	appPool   *db.Pool
	store     *organizations.Store
	svc       *organizations.Service
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

	store := organizations.NewStore()
	return &fixture{
		t: t, ctx: ctx, ownerPool: ownerPool, appPool: appPool,
		store: store, svc: organizations.NewService(appPool, store),
	}
}

// principal is a (principalID, userID) pair — organization_members is
// keyed by user_id, but RLS session context is keyed by principal_id
// (docs/adr/0006), so tests need both, same as Service's own methods do.
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

func TestCreateAddsCreatorAsOwner(t *testing.T) {
	f := newFixture(t)

	owner := f.newUser("owner@example.test")

	org, err := f.svc.Create(f.ctx, owner.principalID, owner.userID, "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if org.Name != "Acme" {
		t.Errorf("org.Name = %q, want Acme", org.Name)
	}

	got, err := f.svc.Get(f.ctx, owner.principalID, owner.userID, org.ID)
	if err != nil {
		t.Fatalf("Get by creator: %v", err)
	}
	if got.ID != org.ID {
		t.Error("Get returned a different organization than Create produced")
	}
}

func TestAddMemberRequiresManageCapability(t *testing.T) {
	f := newFixture(t)

	owner := f.newUser("owner2@example.test")
	member := f.newUser("member2@example.test")
	newcomer := f.newUser("newcomer@example.test")

	org, err := f.svc.Create(f.ctx, owner.principalID, owner.userID, "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Owner adds member as a plain MEMBER.
	if _, err := f.svc.AddMember(f.ctx, owner.principalID, owner.userID, org.ID, member.userID, organizations.RoleMember); err != nil {
		t.Fatalf("owner AddMember: %v", err)
	}

	// A plain MEMBER must not be able to add another member.
	_, err = f.svc.AddMember(f.ctx, member.principalID, member.userID, org.ID, newcomer.userID, organizations.RoleMember)
	if !errors.Is(err, organizations.ErrForbidden) {
		t.Fatalf("AddMember by MEMBER: got err=%v, want ErrForbidden", err)
	}

	// The owner can still add the newcomer.
	if _, err := f.svc.AddMember(f.ctx, owner.principalID, owner.userID, org.ID, newcomer.userID, organizations.RoleMember); err != nil {
		t.Fatalf("owner AddMember newcomer: %v", err)
	}
}

// TestAdminCannotGrantOwner is the organization-scoped twin of
// internal/members.TestAdminCannotGrantOwner: CapOrgManageMembers lets an
// ADMIN manage members, but promoting an account they control to OWNER
// would be ownership transfer by another name.
func TestAdminCannotGrantOwner(t *testing.T) {
	f := newFixture(t)

	owner := f.newUser("owner-grant-org@example.test")
	admin := f.newUser("admin-grant-org@example.test")
	accomplice := f.newUser("accomplice-grant-org@example.test")

	org, err := f.svc.Create(f.ctx, owner.principalID, owner.userID, "Grant Corp")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.svc.AddMember(f.ctx, owner.principalID, owner.userID, org.ID, admin.userID, organizations.RoleAdmin); err != nil {
		t.Fatalf("owner AddMember admin: %v", err)
	}

	_, err = f.svc.AddMember(f.ctx, admin.principalID, admin.userID, org.ID, accomplice.userID, organizations.RoleOwner)
	if !errors.Is(err, organizations.ErrForbidden) {
		t.Fatalf("ADMIN granting OWNER: got err=%v, want ErrForbidden", err)
	}

	// ADMIN must still be able to do ordinary member management.
	if _, err := f.svc.AddMember(f.ctx, admin.principalID, admin.userID, org.ID, accomplice.userID, organizations.RoleMember); err != nil {
		t.Fatalf("ADMIN granting MEMBER: %v", err)
	}

	// And the OWNER keeps the legitimate co-ownership path.
	coOwner := f.newUser("coowner-org@example.test")
	if _, err := f.svc.AddMember(f.ctx, owner.principalID, owner.userID, org.ID, coOwner.userID, organizations.RoleOwner); err != nil {
		t.Fatalf("OWNER granting OWNER: %v", err)
	}
}

// TestAddMemberRejectsUnknownRole keeps an unrecognized role from reaching
// the database, where RoleCan would later map it to no capabilities at all.
func TestAddMemberRejectsUnknownRole(t *testing.T) {
	f := newFixture(t)

	owner := f.newUser("owner-badrole-org@example.test")
	target := f.newUser("target-badrole-org@example.test")

	org, err := f.svc.Create(f.ctx, owner.principalID, owner.userID, "Bad Role Corp")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.svc.AddMember(f.ctx, owner.principalID, owner.userID, org.ID, target.userID, organizations.Role("SUPERUSER")); !errors.Is(err, organizations.ErrInvalidRole) {
		t.Fatalf("unknown role: got err=%v, want ErrInvalidRole", err)
	}
}

func TestNonMemberCannotReadOrganization(t *testing.T) {
	f := newFixture(t)

	owner := f.newUser("owner3@example.test")
	outsider := f.newUser("outsider3@example.test")

	org, err := f.svc.Create(f.ctx, owner.principalID, owner.userID, "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// checkCapability denies before ever reaching the RLS-protected Get
	// query — the outsider has no membership row at all.
	_, err = f.svc.Get(f.ctx, outsider.principalID, outsider.userID, org.ID)
	if !errors.Is(err, organizations.ErrForbidden) {
		t.Fatalf("Get by non-member: got err=%v, want ErrForbidden", err)
	}

	// RLS itself also hides the row from a raw Store.Get as cc_app, proving
	// the denial isn't just an application-level check with no DB backing
	// (docs/adr/0002's testing requirement).
	err = db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: outsider.principalID}, func(tx pgx.Tx) error {
		_, err := f.store.Get(f.ctx, tx, org.ID)
		return err
	})
	if err == nil {
		t.Fatal("outsider Store.Get through cc_app should fail (RLS), got no error")
	}
}

func TestOrganizationMembersBootstrapCannotBeReusedAfterFirstMember(t *testing.T) {
	f := newFixture(t)

	owner := f.newUser("owner4@example.test")
	attacker := f.newUser("attacker4@example.test")

	org, err := f.svc.Create(f.ctx, owner.principalID, owner.userID, "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The organization already has an OWNER — the attacker's own
	// self-bootstrap insert must be rejected by RLS, mirroring
	// TestProjectMembersBootstrapCannotBeReusedAfterFirstMember for
	// project_members (docs/adr/0015's project analogue).
	err = db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: attacker.principalID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, 'OWNER')`,
			org.ID, attacker.userID)
		return err
	})
	if err == nil {
		t.Fatal("attacker self-inserting membership into an already-owned organization should be denied by RLS, got no error")
	}
}

// TestListMineOnlyReturnsCallerMemberships is the negative test the web
// UI's dashboard org picker depends on: a user must see exactly the
// organizations they belong to, never one they were never added to.
func TestListMineOnlyReturnsCallerMemberships(t *testing.T) {
	f := newFixture(t)

	owner := f.newUser("owner-listmine@example.test")
	outsider := f.newUser("outsider-listmine@example.test")

	org, err := f.svc.Create(f.ctx, owner.principalID, owner.userID, "Acme ListMine")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ownerList, err := f.svc.ListMine(f.ctx, owner.principalID, owner.userID)
	if err != nil {
		t.Fatalf("ListMine(owner): %v", err)
	}
	if len(ownerList) != 1 || ownerList[0].Organization.ID != org.ID {
		t.Fatalf("ListMine(owner) = %+v, want exactly [%s]", ownerList, org.ID)
	}
	if ownerList[0].Role != organizations.RoleOwner {
		t.Errorf("owner's role = %s, want OWNER", ownerList[0].Role)
	}

	outsiderList, err := f.svc.ListMine(f.ctx, outsider.principalID, outsider.userID)
	if err != nil {
		t.Fatalf("ListMine(outsider): %v", err)
	}
	if len(outsiderList) != 0 {
		t.Fatalf("ListMine(outsider) = %+v, want empty", outsiderList)
	}
}
