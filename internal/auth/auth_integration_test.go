package auth_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// fixture wires together the stores + pools a test needs. Setup runs
// against ownerPool (RLS-exempt); AuthzService itself always runs the
// decision as cc_app, the same role the production server connects as —
// otherwise the negative tests below would prove nothing (docs/adr/0002
// acceptance criteria).
type fixture struct {
	t         *testing.T
	ctx       context.Context
	ownerPool *db.Pool
	appPool   *db.Pool
	identity  *identity.Store
	orgs      *organizations.Store
	projects  *projects.Store
	authz     *auth.AuthzService
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
		t:         t,
		ctx:       ctx,
		ownerPool: ownerPool,
		appPool:   appPool,
		identity:  identity.NewStore(),
		orgs:      organizations.NewStore(),
		projects:  projectStore,
		authz:     auth.NewAuthzService(appPool, projectStore),
	}
}

// asOwner runs fn in a transaction against ownerPool with no RLS
// restriction — fixture setup only, never the thing under test.
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

func (f *fixture) newServicePrincipal(name string) identity.Principal {
	f.t.Helper()
	var p identity.Principal
	f.asOwner(func(tx pgx.Tx) error {
		sp, err := f.identity.CreateServicePrincipal(f.ctx, tx, name, "test service principal")
		if err != nil {
			return err
		}
		p, err = f.identity.CreatePrincipalForService(f.ctx, tx, sp.ID)
		return err
	})
	return p
}

func (f *fixture) newPersonalProject(ownerUserID uuid.UUID, namespaceSlug, projectSlug string) projects.Project {
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

func (f *fixture) addMember(projectID, principalID uuid.UUID, role projects.Role) {
	f.t.Helper()
	f.asOwner(func(tx pgx.Tx) error {
		_, err := f.projects.AddMember(f.ctx, tx, projectID, principalID, role)
		return err
	})
}

// --- Tests -------------------------------------------------------------

func TestReaderCannotWrite(t *testing.T) {
	f := newFixture(t)

	owner := f.newUserPrincipal("owner@example.test")
	reader := f.newUserPrincipal("reader@example.test")
	project := f.newPersonalProject(*owner.UserID, "owner-ns", "proj")
	f.addMember(project.ID, owner.ID, projects.RoleOwner)
	f.addMember(project.ID, reader.ID, projects.RoleReader)

	canRead, err := f.authz.Can(f.ctx, reader.ID, project.ID, auth.CapArtifactRead)
	if err != nil {
		t.Fatalf("Can(read): %v", err)
	}
	if !canRead {
		t.Error("READER should be able to read artifacts")
	}

	canWrite, err := f.authz.Can(f.ctx, reader.ID, project.ID, auth.CapArtifactWrite)
	if err != nil {
		t.Fatalf("Can(write): %v", err)
	}
	if canWrite {
		t.Error("READER write should be DENY, got ALLOW")
	}
}

func TestNonMemberDeniedCrossProject(t *testing.T) {
	f := newFixture(t)

	ownerA := f.newUserPrincipal("owner-a@example.test")
	outsider := f.newUserPrincipal("outsider@example.test")
	projectA := f.newPersonalProject(*ownerA.UserID, "team-a", "secret-project")
	f.addMember(projectA.ID, ownerA.ID, projects.RoleOwner)
	// outsider is a real, valid principal — just never added to projectA.

	allowed, err := f.authz.Can(f.ctx, outsider.ID, projectA.ID, auth.CapArtifactRead)
	if err != nil {
		t.Fatalf("Can: %v", err)
	}
	if allowed {
		t.Error("principal with no membership in the project should be DENY, got ALLOW")
	}
}

func TestServicePrincipalSameRBACAsUser(t *testing.T) {
	f := newFixture(t)

	owner := f.newUserPrincipal("owner2@example.test")
	ciAgent := f.newServicePrincipal("nightly-research-bot")
	project := f.newPersonalProject(*owner.UserID, "automation", "proj")
	f.addMember(project.ID, owner.ID, projects.RoleOwner)
	f.addMember(project.ID, ciAgent.ID, projects.RoleContributor)

	if ciAgent.Kind != identity.PrincipalKindService {
		t.Fatalf("expected a service principal, got kind=%s", ciAgent.Kind)
	}

	allowed, err := f.authz.Can(f.ctx, ciAgent.ID, project.ID, auth.CapArtifactWrite)
	if err != nil {
		t.Fatalf("Can: %v", err)
	}
	if !allowed {
		t.Error("ServicePrincipal with CONTRIBUTOR role should be able to write artifacts, same as a User CONTRIBUTOR")
	}

	deleteAllowed, err := f.authz.Can(f.ctx, ciAgent.ID, project.ID, auth.CapProjectDelete)
	if err != nil {
		t.Fatalf("Can: %v", err)
	}
	if deleteAllowed {
		t.Error("CONTRIBUTOR (service or user) must not have project.delete")
	}
}

func TestOrganizationOwnerHasNoImplicitProjectAccess(t *testing.T) {
	f := newFixture(t)

	var orgOwnerUserID uuid.UUID
	var org organizations.Organization
	var project projects.Project
	var orgOwnerPrincipal identity.Principal

	f.asOwner(func(tx pgx.Tx) error {
		u, err := f.identity.CreateUser(f.ctx, tx, "org-owner@example.test", "Org Owner")
		if err != nil {
			return err
		}
		orgOwnerUserID = u.ID
		orgOwnerPrincipal, err = f.identity.CreatePrincipalForUser(f.ctx, tx, u.ID)
		if err != nil {
			return err
		}

		org, err = f.orgs.Create(f.ctx, tx, "Acme")
		if err != nil {
			return err
		}
		if _, err := f.orgs.AddMember(f.ctx, tx, org.ID, orgOwnerUserID, organizations.RoleOwner); err != nil {
			return err
		}

		otherUser, err := f.identity.CreateUser(f.ctx, tx, "project-owner@example.test", "Project Owner")
		if err != nil {
			return err
		}
		projectOwnerPrincipal, err := f.identity.CreatePrincipalForUser(f.ctx, tx, otherUser.ID)
		if err != nil {
			return err
		}

		ns, err := f.projects.CreateOrganizationNamespace(f.ctx, tx, org.ID, "acme")
		if err != nil {
			return err
		}
		project, err = f.projects.Create(f.ctx, tx, ns.ID, "internal-project", "Internal Project")
		if err != nil {
			return err
		}
		// The org's project is owned by a different principal than the org
		// OWNER — org role must not leak into project content access
		// (docs/adr/0006-organization-vs-project-authorization.md).
		_, err = f.projects.AddMember(f.ctx, tx, project.ID, projectOwnerPrincipal.ID, projects.RoleOwner)
		return err
	})

	allowed, err := f.authz.Can(f.ctx, orgOwnerPrincipal.ID, project.ID, auth.CapArtifactRead)
	if err != nil {
		t.Fatalf("Can: %v", err)
	}
	if allowed {
		t.Error("Organization OWNER without explicit project membership should be DENY on project content, got ALLOW")
	}
}

func TestNamespaceScopedSlugsDoNotCollide(t *testing.T) {
	f := newFixture(t)

	filip := f.newUserPrincipal("filip@example.test")

	var filipProject, acmeProject projects.Project
	f.asOwner(func(tx pgx.Tx) error {
		filipNS, err := f.projects.CreatePersonalNamespace(f.ctx, tx, *filip.UserID, "filip")
		if err != nil {
			return err
		}
		filipProject, err = f.projects.Create(f.ctx, tx, filipNS.ID, "es-migration", "ES Migration")
		if err != nil {
			return err
		}

		org, err := f.orgs.Create(f.ctx, tx, "Acme")
		if err != nil {
			return err
		}
		acmeNS, err := f.projects.CreateOrganizationNamespace(f.ctx, tx, org.ID, "acme")
		if err != nil {
			return err
		}
		acmeProject, err = f.projects.Create(f.ctx, tx, acmeNS.ID, "es-migration", "ES Migration")
		return err
	})

	if filipProject.ID == acmeProject.ID {
		t.Fatal("filip/es-migration and acme/es-migration resolved to the same project")
	}

	f.asOwner(func(tx pgx.Tx) error {
		resolvedFilip, err := f.projects.Resolve(f.ctx, tx, "filip/es-migration")
		if err != nil {
			t.Errorf("resolve filip/es-migration: %v", err)
		} else if resolvedFilip.ID != filipProject.ID {
			t.Error("resolve filip/es-migration returned the wrong project")
		}

		resolvedAcme, err := f.projects.Resolve(f.ctx, tx, "acme/es-migration")
		if err != nil {
			t.Errorf("resolve acme/es-migration: %v", err)
		} else if resolvedAcme.ID != acmeProject.ID {
			t.Error("resolve acme/es-migration returned the wrong project")
		}
		return nil
	})
}

// TestRLSIsolationOfProjectKeys pins the database half for project_keys —
// the table that holds every project's wrapped encryption key, and therefore
// the one where a policy regression matters most. A leaked wrapped key is
// not immediately useful (unwrapping needs the root key, docs/adr/0005), but
// "not immediately" is the whole margin, and this table has no other
// protection: internal/crypto's own tests run entirely as the RLS-exempt
// owner role, deliberately, because they are about crypto correctness rather
// than access — with a comment pointing here for the access half.
func TestRLSIsolationOfProjectKeys(t *testing.T) {
	f := newFixture(t)

	owner := f.newUserPrincipal("keys-owner@example.test")
	outsider := f.newUserPrincipal("keys-outsider@example.test")
	project := f.newPersonalProject(*owner.UserID, "keys-ns", "proj")
	f.addMember(project.ID, owner.ID, projects.RoleOwner)

	// A project key, inserted the way crypto.ensureProjectKey does.
	f.asOwner(func(tx pgx.Tx) error {
		// wrapped_key packs the GCM nonce prefix with the ciphertext; this
		// table has no separate nonce column (migrations/000002).
		_, err := tx.Exec(f.ctx,
			`INSERT INTO project_keys (project_id, key_version, wrapped_key, root_key_ref)
			 VALUES ($1, 1, $2, 'test')`,
			project.ID, []byte("wrapped-key-bytes"))
		return err
	})

	countThrough := func(principalID uuid.UUID) int {
		t.Helper()
		var n int
		if err := db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
			// No project filter: whatever comes back was let through by the
			// policy itself.
			return tx.QueryRow(f.ctx, `SELECT count(*) FROM project_keys`).Scan(&n)
		}); err != nil {
			t.Fatalf("count project_keys through cc_app: %v", err)
		}
		return n
	}

	if n := countThrough(outsider.ID); n != 0 {
		t.Fatalf("project_keys: a non-member's session sees %d wrapped key(s) — RLS is not isolating this table", n)
	}
	if n := countThrough(owner.ID); n != 1 {
		t.Fatalf("the project's own member sees %d keys, want 1", n)
	}
}
