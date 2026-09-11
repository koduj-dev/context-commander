package members_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/members"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// fixture mirrors internal/auth's: setup runs against ownerPool (RLS-exempt),
// while the Service under test runs as cc_app — the same role the production
// server connects as, or the negative cases below would prove nothing.
type fixture struct {
	t         *testing.T
	ctx       context.Context
	ownerPool *db.Pool
	identity  *identity.Store
	projects  *projects.Store
	svc       *members.Service
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

	projectStore := projects.NewStore()
	return &fixture{
		t: t, ctx: ctx, ownerPool: ownerPool,
		identity: identity.NewStore(),
		projects: projectStore,
		svc:      members.NewService(appPool, projectStore, auth.NewAuthzService(appPool, projectStore)),
	}
}

func (f *fixture) asOwner(fn func(tx pgx.Tx) error) {
	f.t.Helper()
	if err := db.WithoutPrincipal(f.ctx, f.ownerPool, fn); err != nil {
		f.t.Fatalf("fixture setup: %v", err)
	}
}

func (f *fixture) newUserPrincipal(email string) identity.Principal {
	f.t.Helper()
	var p identity.Principal
	f.asOwner(func(tx pgx.Tx) error {
		u, err := f.identity.CreateUser(f.ctx, tx, email, email)
		if err != nil {
			return err
		}
		p, err = f.identity.CreatePrincipalForUser(f.ctx, tx, u.ID)
		return err
	})
	return p
}

func (f *fixture) newProject(ownerUserID uuid.UUID, namespaceSlug, projectSlug string) projects.Project {
	f.t.Helper()
	var proj projects.Project
	f.asOwner(func(tx pgx.Tx) error {
		ns, err := f.projects.CreatePersonalNamespace(f.ctx, tx, ownerUserID, namespaceSlug)
		if err != nil {
			return err
		}
		proj, err = f.projects.Create(f.ctx, tx, ns.ID, projectSlug, projectSlug)
		return err
	})
	return proj
}

func (f *fixture) seedMember(projectID, principalID uuid.UUID, role projects.Role) {
	f.t.Helper()
	f.asOwner(func(tx pgx.Tx) error {
		_, err := f.projects.AddMember(f.ctx, tx, projectID, principalID, role)
		return err
	})
}

// TestAdminCannotGrantOwner is the regression test for the privilege
// escalation this Service exists to close: ADMIN holds CapMemberManage but
// deliberately not CapProjectDelete, so if member.manage alone were enough
// to hand out OWNER, an ADMIN could add a second principal they control as
// OWNER and exercise ownership through it.
func TestAdminCannotGrantOwner(t *testing.T) {
	f := newFixture(t)

	owner := f.newUserPrincipal("owner-grant@example.test")
	admin := f.newUserPrincipal("admin-grant@example.test")
	accomplice := f.newUserPrincipal("accomplice-grant@example.test")
	project := f.newProject(*owner.UserID, "grant-ns", "proj")
	f.seedMember(project.ID, owner.ID, projects.RoleOwner)
	f.seedMember(project.ID, admin.ID, projects.RoleAdmin)

	err := f.svc.Add(f.ctx, admin.ID, project.ID, accomplice.ID, projects.RoleOwner)
	if !errors.Is(err, members.ErrForbidden) {
		t.Fatalf("ADMIN granting OWNER: err = %v, want ErrForbidden", err)
	}

	// The row must not exist — a check that returns an error but writes
	// anyway would still be an escalation.
	list, err := f.svc.List(f.ctx, owner.ID, project.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	for _, m := range list {
		if m.PrincipalID == accomplice.ID {
			t.Fatalf("accomplice was added with role %s despite the denial", m.Role)
		}
	}
}

// TestAdminCanGrantNonOwnerRoles is the other half of the boundary: the fix
// must not turn ADMIN into a role that cannot manage members at all.
func TestAdminCanGrantNonOwnerRoles(t *testing.T) {
	f := newFixture(t)

	owner := f.newUserPrincipal("owner-nonowner@example.test")
	admin := f.newUserPrincipal("admin-nonowner@example.test")
	project := f.newProject(*owner.UserID, "nonowner-ns", "proj")
	f.seedMember(project.ID, owner.ID, projects.RoleOwner)
	f.seedMember(project.ID, admin.ID, projects.RoleAdmin)

	for _, role := range []projects.Role{projects.RoleAdmin, projects.RoleContributor, projects.RoleReader} {
		target := f.newUserPrincipal("target-" + string(role) + "@example.test")
		if err := f.svc.Add(f.ctx, admin.ID, project.ID, target.ID, role); err != nil {
			t.Errorf("ADMIN granting %s: %v", role, err)
		}
	}
}

// TestOwnerCanGrantOwner keeps the legitimate path open — co-ownership is a
// real thing an OWNER may choose.
func TestOwnerCanGrantOwner(t *testing.T) {
	f := newFixture(t)

	owner := f.newUserPrincipal("owner-coowner@example.test")
	coOwner := f.newUserPrincipal("coowner@example.test")
	project := f.newProject(*owner.UserID, "coowner-ns", "proj")
	f.seedMember(project.ID, owner.ID, projects.RoleOwner)

	if err := f.svc.Add(f.ctx, owner.ID, project.ID, coOwner.ID, projects.RoleOwner); err != nil {
		t.Fatalf("OWNER granting OWNER: %v", err)
	}
}

// TestContributorCannotAddAnyone checks the capability below the one under
// test is still enforced — CapMemberManage, not just the OWNER rule.
func TestContributorCannotAddAnyone(t *testing.T) {
	f := newFixture(t)

	owner := f.newUserPrincipal("owner-contrib@example.test")
	contributor := f.newUserPrincipal("contrib@example.test")
	target := f.newUserPrincipal("target-contrib@example.test")
	project := f.newProject(*owner.UserID, "contrib-ns", "proj")
	f.seedMember(project.ID, owner.ID, projects.RoleOwner)
	f.seedMember(project.ID, contributor.ID, projects.RoleContributor)

	if err := f.svc.Add(f.ctx, contributor.ID, project.ID, target.ID, projects.RoleReader); !errors.Is(err, members.ErrForbidden) {
		t.Fatalf("CONTRIBUTOR adding a member: err = %v, want ErrForbidden", err)
	}
}

// TestUnknownRoleRejected — an unrecognized role would otherwise reach the
// database and later map to no capabilities at all via auth.RoleCan.
func TestUnknownRoleRejected(t *testing.T) {
	f := newFixture(t)

	owner := f.newUserPrincipal("owner-badrole@example.test")
	target := f.newUserPrincipal("target-badrole@example.test")
	project := f.newProject(*owner.UserID, "badrole-ns", "proj")
	f.seedMember(project.ID, owner.ID, projects.RoleOwner)

	if err := f.svc.Add(f.ctx, owner.ID, project.ID, target.ID, projects.Role("SUPERUSER")); !errors.Is(err, members.ErrInvalidRole) {
		t.Fatalf("unknown role: err = %v, want ErrInvalidRole", err)
	}
}

// TestNonMemberCannotList is the read-side authz boundary: a caller who is
// not a member must not learn who the project's members are.
func TestNonMemberCannotList(t *testing.T) {
	f := newFixture(t)

	owner := f.newUserPrincipal("owner-list@example.test")
	outsider := f.newUserPrincipal("outsider-list@example.test")
	project := f.newProject(*owner.UserID, "list-ns", "proj")
	f.seedMember(project.ID, owner.ID, projects.RoleOwner)

	if _, err := f.svc.List(f.ctx, outsider.ID, project.ID); !errors.Is(err, members.ErrForbidden) {
		t.Fatalf("outsider listing members: err = %v, want ErrForbidden", err)
	}
}
