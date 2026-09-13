package gates_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type engineFixture struct {
	t           *testing.T
	ctx         context.Context
	ownerPool   *db.Pool
	appPool     *db.Pool
	engine      *gates.Engine
	projectID   uuid.UUID
	principalID uuid.UUID
}

func newEngineFixture(t *testing.T) *engineFixture {
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

	principalID := uuid.New()
	projectID := uuid.New()
	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture User"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			principalID, userID); err != nil {
			return err
		}
		namespaceID := uuid.New()
		if _, err := tx.Exec(ctx,
			`INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			namespaceID, "fixture-"+namespaceID.String()[:8], userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'fixture', 'Fixture')`,
			projectID, namespaceID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'OWNER')`,
			projectID, principalID)
		return err
	})
	if err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	return &engineFixture{
		t: t, ctx: ctx, ownerPool: ownerPool, appPool: appPool,
		engine: gates.NewEngine(appPool), projectID: projectID, principalID: principalID,
	}
}

func (f *engineFixture) bindGate(hook, scope, gateName string, scopeRef *uuid.UUID) {
	f.t.Helper()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`INSERT INTO gate_bindings (id, scope, scope_ref, gate_name, hook) VALUES ($1, $2, $3, $4, $5)`,
			uuid.New(), scope, scopeRef, gateName, hook)
		return err
	})
	if err != nil {
		f.t.Fatalf("bindGate: %v", err)
	}
}

func TestEngineEvaluateNoBindingsAllows(t *testing.T) {
	f := newEngineFixture(t)
	r, err := f.engine.Evaluate(f.ctx, f.principalID, f.projectID, nil, "artifact.before_write", []byte("hello"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if r.Verdict != gates.Allow {
		t.Fatalf("got verdict %s, want ALLOW with no bindings", r.Verdict)
	}
}

func TestEngineDenyTakesPriorityAcrossScopes(t *testing.T) {
	f := newEngineFixture(t)
	f.bindGate("artifact.before_write", "installation", "secret-scanner", nil)

	r, err := f.engine.Evaluate(f.ctx, f.principalID, f.projectID, nil, "artifact.before_write", []byte("AWS_KEY=AKIAABCDEFGHIJKLMNOP"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if r.Verdict != gates.Deny {
		t.Fatalf("got verdict %s, want DENY", r.Verdict)
	}
}

func TestEngineFailsClosedOnUnregisteredGateName(t *testing.T) {
	f := newEngineFixture(t)
	f.bindGate("artifact.before_write", "project", "no-such-gate", &f.projectID)

	_, err := f.engine.Evaluate(f.ctx, f.principalID, f.projectID, nil, "artifact.before_write", []byte("hello"))
	if err == nil {
		t.Fatal("Evaluate: want an error when the bound gate name is unregistered (fail closed, not ALLOW)")
	}
}

func TestEngineOnlyMatchesBoundHook(t *testing.T) {
	f := newEngineFixture(t)
	f.bindGate("artifact.before_delete", "installation", "secret-scanner", nil)

	r, err := f.engine.Evaluate(f.ctx, f.principalID, f.projectID, nil, "artifact.before_write", []byte("AWS_KEY=AKIAABCDEFGHIJKLMNOP"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if r.Verdict != gates.Allow {
		t.Fatalf("got verdict %s, want ALLOW — bound gate is on a different hook", r.Verdict)
	}
}

// TestEngineOrganizationBindingVisibleThroughProjectMembershipButNotMutable
// exercises both halves of migration 000012's split RLS policy: a principal
// with project membership but no organization membership must see and execute
// the organization's gate, but that read visibility must not allow them to
// mutate the organization-scoped binding directly through cc_app.
func TestEngineOrganizationBindingVisibleThroughProjectMembershipButNotMutable(t *testing.T) {
	f := newEngineFixture(t)
	organizationID := uuid.New()
	organizationProjectID := uuid.New()
	bindingID := uuid.New()

	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `INSERT INTO organizations (id, name) VALUES ($1, 'Fixture Org')`, organizationID); err != nil {
			return err
		}
		namespaceID := uuid.New()
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO namespaces (id, kind, slug, organization_id) VALUES ($1, 'organization', $2, $3)`,
			namespaceID, "fixture-org-"+namespaceID.String()[:8], organizationID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'org-project', 'Org Project')`,
			organizationProjectID, namespaceID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'CONTRIBUTOR')`,
			organizationProjectID, f.principalID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx,
			`INSERT INTO gate_bindings (id, scope, scope_ref, gate_name, hook) VALUES ($1, 'organization', $2, 'secret-scanner', 'artifact.before_write')`,
			bindingID, organizationID)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed organization project and binding: %v", err)
	}

	r, err := f.engine.Evaluate(f.ctx, f.principalID, organizationProjectID, &organizationID, "artifact.before_write", []byte("AWS_KEY=AKIAABCDEFGHIJKLMNOP"))
	if err != nil {
		f.t.Fatalf("Evaluate: %v", err)
	}
	if r.Verdict != gates.Deny {
		f.t.Fatalf("got verdict %s, want DENY from organization binding visible through project membership", r.Verdict)
	}

	err = db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: f.principalID}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(f.ctx, `DELETE FROM gate_bindings WHERE id = $1`, bindingID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 0 {
			f.t.Fatalf("deleted %d organization bindings without organization membership, want 0", tag.RowsAffected())
		}
		return nil
	})
	if err != nil {
		f.t.Fatalf("attempt direct binding delete: %v", err)
	}
}

// --- AdminService (Bind/Unbind/ListBindings) ---

type adminFixture struct {
	*engineFixture
	admin               *gates.AdminService
	installationAdmins  *installation.Store
	userID              uuid.UUID
	contributorID       uuid.UUID
	contributorUserID   uuid.UUID
	organizationID      uuid.UUID
	organizationOwnerID uuid.UUID
}

// grantInstallationAdmin makes f.userID an installation admin — the
// equivalent of `ccd installation grant-admin` for tests (docs/adr/0017).
func (f *adminFixture) grantInstallationAdmin() {
	f.t.Helper()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := f.installationAdmins.Grant(f.ctx, tx, f.userID, nil)
		return err
	})
	if err != nil {
		f.t.Fatalf("grant installation admin: %v", err)
	}
}

func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	f := newEngineFixture(t)

	var userID, contributorID, contributorUserID, organizationID, organizationOwnerUserID uuid.UUID
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		// f.principalID's user_id, needed as the "userID" AdminService.Bind
		// checks organization membership against.
		if err := tx.QueryRow(f.ctx, `SELECT user_id FROM principals WHERE id = $1`, f.principalID).Scan(&userID); err != nil {
			return err
		}

		contributorUserID = uuid.New()
		contributorID = uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			contributorUserID, contributorUserID.String()+"@example.test", "Contributor"); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			contributorID, contributorUserID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'CONTRIBUTOR')`,
			f.projectID, contributorID); err != nil {
			return err
		}

		orgs := organizations.NewStore()
		org, err := orgs.Create(f.ctx, tx, "fixture-org")
		if err != nil {
			return err
		}
		organizationID = org.ID
		organizationOwnerUserID = userID
		_, err = orgs.AddMember(f.ctx, tx, organizationID, organizationOwnerUserID, organizations.RoleOwner)
		return err
	})
	if err != nil {
		t.Fatalf("seed admin fixture: %v", err)
	}

	projStore := projects.NewStore()
	authz := auth.NewAuthzService(f.appPool, projStore)
	installationStore := installation.NewStore()

	return &adminFixture{
		engineFixture:       f,
		admin:               gates.NewAdminService(f.appPool, authz, organizations.NewStore(), projStore, installationStore),
		installationAdmins:  installationStore,
		userID:              userID,
		contributorID:       contributorID,
		contributorUserID:   contributorUserID,
		organizationID:      organizationID,
		organizationOwnerID: organizationOwnerUserID,
	}
}

func TestAdminBindAndListBindingsProjectScope(t *testing.T) {
	f := newAdminFixture(t)

	b, err := f.admin.Bind(f.ctx, f.principalID, f.userID, f.projectID, gates.ScopeProject, &f.projectID, "secret-scanner", "artifact.before_write", nil)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if b.Scope != gates.ScopeProject || !b.Enabled {
		t.Fatalf("unexpected binding: %+v", b)
	}

	list, err := f.admin.ListBindings(f.ctx, f.principalID, f.projectID, nil)
	if err != nil {
		t.Fatalf("ListBindings: %v", err)
	}
	if len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("got %d bindings, want the one just created: %+v", len(list), list)
	}
}

func TestAdminBindInstallationScopeRequiresAdmin(t *testing.T) {
	f := newAdminFixture(t)

	_, err := f.admin.Bind(f.ctx, f.principalID, f.userID, f.projectID, gates.ScopeInstallation, nil, "secret-scanner", "artifact.before_write", nil)
	if !errors.Is(err, gates.ErrForbidden) {
		t.Fatalf("Bind at installation scope as a non-admin: got %v, want ErrForbidden", err)
	}
}

// TestAdminBindInstallationScopeAllowedForAdmin is the positive
// counterpart — docs/adr/0017 replaced the old "operator-seeded only" hard
// rejection with a real capability check.
func TestAdminBindInstallationScopeAllowedForAdmin(t *testing.T) {
	f := newAdminFixture(t)
	f.grantInstallationAdmin()

	b, err := f.admin.Bind(f.ctx, f.principalID, f.userID, f.projectID, gates.ScopeInstallation, nil, "secret-scanner", "artifact.before_write", nil)
	if err != nil {
		t.Fatalf("Bind at installation scope as an admin: %v", err)
	}
	if b.Scope != gates.ScopeInstallation {
		t.Errorf("b.Scope = %q, want %q", b.Scope, gates.ScopeInstallation)
	}
}

func TestAdminBindOrganizationScopeRequiresOrgMembership(t *testing.T) {
	f := newAdminFixture(t)

	// f.userID is OWNER of f.organizationID — should succeed.
	if _, err := f.admin.Bind(f.ctx, f.principalID, f.userID, f.projectID, gates.ScopeOrganization, &f.organizationID, "secret-scanner", "artifact.before_write", nil); err != nil {
		t.Fatalf("Bind as org owner: %v", err)
	}

	// contributorUserID has no organization membership at all.
	_, err := f.admin.Bind(f.ctx, f.contributorID, f.contributorUserID, f.projectID, gates.ScopeOrganization, &f.organizationID, "secret-scanner", "artifact.before_write", nil)
	if !errors.Is(err, gates.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden for a caller with no organization membership", err)
	}
}

func TestAdminBindDeniedWithoutCapGateManage(t *testing.T) {
	f := newAdminFixture(t)

	_, err := f.admin.Bind(f.ctx, f.contributorID, f.contributorUserID, f.projectID, gates.ScopeProject, &f.projectID, "secret-scanner", "artifact.before_write", nil)
	if !errors.Is(err, gates.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden — CONTRIBUTOR lacks CapGateManage", err)
	}
}

// TestAdminBindAndUnbindDeniedForNonManagerOfScopeRefProject
// regression-guards against binding into (or unbinding from) a project
// other than the anchor projectID used for the initial capability check:
// f.principalID is OWNER on f.projectID (the anchor, giving CapGateManage)
// but only CONTRIBUTOR on a second project — Bind must deny writing a
// binding scoped to that second project, and Unbind must likewise deny
// removing an existing binding scoped there, rather than authorizing
// either off the unrelated anchor.
func TestAdminBindAndUnbindDeniedForNonManagerOfScopeRefProject(t *testing.T) {
	f := newAdminFixture(t)
	otherProjectID := uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		otherOwnerUserID := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			otherOwnerUserID, otherOwnerUserID.String()+"@example.test", "Other Project Owner"); err != nil {
			return err
		}
		namespaceID := uuid.New()
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			namespaceID, "other-"+namespaceID.String()[:8], otherOwnerUserID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'other', 'Other')`,
			otherProjectID, namespaceID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'CONTRIBUTOR')`,
			otherProjectID, f.principalID)
		return err
	})
	if err != nil {
		t.Fatalf("seed other project: %v", err)
	}

	if _, err := f.admin.Bind(f.ctx, f.principalID, f.userID, f.projectID, gates.ScopeProject, &otherProjectID, "secret-scanner", "artifact.before_write", nil); !errors.Is(err, gates.ErrForbidden) {
		t.Fatalf("Bind into a project the caller is only CONTRIBUTOR on: got err=%v, want ErrForbidden", err)
	}

	// Seed an existing project-scope binding directly (bypassing
	// AdminService) on otherProjectID, then confirm Unbind — anchored at
	// f.projectID, where the caller has CapGateManage — cannot remove it
	// either.
	bindingID := uuid.New()
	err = db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`INSERT INTO gate_bindings (id, scope, scope_ref, gate_name, hook) VALUES ($1, 'project', $2, 'secret-scanner', 'artifact.before_write')`,
			bindingID, otherProjectID)
		return err
	})
	if err != nil {
		t.Fatalf("seed other project binding: %v", err)
	}
	if err := f.admin.Unbind(f.ctx, f.principalID, f.userID, f.projectID, bindingID); !errors.Is(err, gates.ErrForbidden) {
		t.Fatalf("Unbind a binding scoped to a project the caller is only CONTRIBUTOR on: got err=%v, want ErrForbidden", err)
	}
}

func TestAdminUnbind(t *testing.T) {
	f := newAdminFixture(t)

	b, err := f.admin.Bind(f.ctx, f.principalID, f.userID, f.projectID, gates.ScopeProject, &f.projectID, "secret-scanner", "artifact.before_write", nil)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if err := f.admin.Unbind(f.ctx, f.principalID, f.userID, f.projectID, b.ID); err != nil {
		t.Fatalf("Unbind: %v", err)
	}

	list, err := f.admin.ListBindings(f.ctx, f.principalID, f.projectID, nil)
	if err != nil {
		t.Fatalf("ListBindings: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("got %d bindings after Unbind, want 0: %+v", len(list), list)
	}

	if err := f.admin.Unbind(f.ctx, f.principalID, f.userID, f.projectID, b.ID); !errors.Is(err, gates.ErrNotFound) {
		t.Fatalf("second Unbind: got %v, want ErrNotFound", err)
	}
}

// TestRLSIsolationOfGateBindings pins the database half for gate_bindings.
// A gate binding says which policy checks run against a project's writes, so
// leaking the list tells an outsider what a project screens for — and
// migration 000012 exists precisely because this table's visibility was got
// wrong once already.
func TestRLSIsolationOfGateBindings(t *testing.T) {
	f := newEngineFixture(t)
	f.bindGate("artifact.before_write", "project", "secret-scanner", &f.projectID)

	outsider := uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Outsider"); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, outsider, userID)
		return err
	})
	if err != nil {
		t.Fatalf("seed outsider: %v", err)
	}

	err = db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: outsider}, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(f.ctx, `SELECT count(*) FROM gate_bindings`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatalf("gate_bindings: a non-member's session sees %d row(s) — RLS is not isolating which gates a project runs", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("count gate_bindings through cc_app: %v", err)
	}

	err = db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: f.principalID}, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(f.ctx, `SELECT count(*) FROM gate_bindings`).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("the project's own member sees %d gate bindings, want 1", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("count as member: %v", err)
	}
}
