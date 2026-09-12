package security_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/security"
)

type fixture struct {
	t         *testing.T
	ctx       context.Context
	ownerPool *db.Pool
	appPool   *db.Pool
	rec       *security.Recorder
	svc       *security.Service
	authz     *auth.AuthzService
}

func newFixture(t *testing.T, recordingEnabled bool) *fixture {
	t.Helper()
	ctx := context.Background()
	dsns := dbtest.New(t)

	ownerPool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}
	appPool, err := db.NewPool(ctx, dsns.App)
	if err != nil {
		t.Fatalf("connect as cc_app: %v", err)
	}

	installationStore := installation.NewStore()
	rec := security.NewRecorder(appPool, recordingEnabled)
	authz := auth.NewAuthzService(appPool, projects.NewStore())
	authz.SetDenyRecorder(security.NewDenyRecorder(rec))

	return &fixture{
		t: t, ctx: ctx, ownerPool: ownerPool, appPool: appPool,
		rec: rec, svc: security.NewService(appPool, installationStore), authz: authz,
	}
}

// seedMember creates a principal holding role on a fresh project, and
// returns the principal, its user, and the project.
func (f *fixture) seedMember(role projects.Role) (principalID, userID, projectID uuid.UUID) {
	f.t.Helper()
	principalID, userID, projectID = uuid.New(), uuid.New(), uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture"); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, principalID, userID); err != nil {
			return err
		}
		nsID := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			nsID, "ns-"+nsID.String()[:8], userID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'p', 'P')`,
			projectID, nsID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`,
			projectID, principalID, role)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed member: %v", err)
	}
	return principalID, userID, projectID
}

func (f *fixture) grantInstallationAdmin(userID uuid.UUID) {
	f.t.Helper()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := installation.NewStore().Grant(f.ctx, tx, userID, nil)
		return err
	})
	if err != nil {
		f.t.Fatalf("grant installation admin: %v", err)
	}
}

// TestCapabilityDenialIsRecorded is the gap this package exists to close:
// before it, audit.ResultDeny was a defined constant with zero call sites
// and no refusal anywhere in the server was written down.
func TestCapabilityDenialIsRecorded(t *testing.T) {
	f := newFixture(t, true)
	readerID, readerUserID, projectID := f.seedMember(projects.RoleReader)
	f.grantInstallationAdmin(readerUserID)

	// A READER attempting a write: denied by role, not by membership.
	allowed, err := f.authz.Can(f.ctx, readerID, projectID, auth.CapArtifactWrite)
	if err != nil {
		t.Fatalf("Can: %v", err)
	}
	if allowed {
		t.Fatal("READER was allowed CapArtifactWrite")
	}

	page, err := f.svc.List(f.ctx, readerID, readerUserID, paging.Options{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("security events = %d, want 1: %+v", len(page.Items), page.Items)
	}
	e := page.Items[0]
	if e.Kind != security.KindAuthorizationDenied {
		t.Fatalf("kind = %q, want %q", e.Kind, security.KindAuthorizationDenied)
	}
	if e.Capability != string(auth.CapArtifactWrite) {
		t.Fatalf("capability = %q, want %q", e.Capability, auth.CapArtifactWrite)
	}
	if e.PrincipalID == nil || *e.PrincipalID != readerID {
		t.Fatalf("principal_id = %v, want %s", e.PrincipalID, readerID)
	}
	// The distinction that makes the record worth reading: a member hitting
	// the edge of their role is routine, a non-member reaching into a
	// project is not.
	if e.Reason == "" || e.Reason == "principal is not a member of this project" {
		t.Fatalf("reason = %q, want it to name the role's missing capability", e.Reason)
	}
}

// TestNonMemberDenialIsRecorded is the case audit_log structurally cannot
// hold: its RLS policy doubles as the insert check, so a row saying "this
// principal has no access to project X" cannot be written by that
// principal. security_events has no RLS for exactly this reason.
func TestNonMemberDenialIsRecorded(t *testing.T) {
	f := newFixture(t, true)
	_, _, projectID := f.seedMember(projects.RoleOwner)
	outsiderID, outsiderUserID, _ := f.seedMember(projects.RoleOwner) // owner of their OWN project, stranger to this one
	f.grantInstallationAdmin(outsiderUserID)

	allowed, err := f.authz.Can(f.ctx, outsiderID, projectID, auth.CapArtifactRead)
	if err != nil {
		t.Fatalf("Can: %v", err)
	}
	if allowed {
		t.Fatal("a non-member was allowed to read")
	}

	page, err := f.svc.List(f.ctx, outsiderID, outsiderUserID, paging.Options{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, e := range page.Items {
		if e.ProjectID != nil && *e.ProjectID == projectID && e.Reason == "principal is not a member of this project" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the non-member denial was not recorded: %+v", page.Items)
	}
}

// TestOptOutRecordsNothing pins the CRA Annex I opt-out: with recording
// disabled the server must write nothing at all, rather than write less.
func TestOptOutRecordsNothing(t *testing.T) {
	f := newFixture(t, false)
	readerID, readerUserID, projectID := f.seedMember(projects.RoleReader)
	f.grantInstallationAdmin(readerUserID)

	if _, err := f.authz.Can(f.ctx, readerID, projectID, auth.CapArtifactWrite); err != nil {
		t.Fatalf("Can: %v", err)
	}
	f.rec.Record(f.ctx, security.Event{Kind: security.KindAuthenticationFailed, Reason: "should not be written"})

	page, err := f.svc.List(f.ctx, readerID, readerUserID, paging.Options{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("recording was opted out of, but %d event(s) were written", len(page.Items))
	}
}

// TestReadsAreInstallationAdminOnly: the refusal timeline names principals
// and the projects they were refused on, across the whole instance. A
// project role is not enough to read it.
func TestReadsAreInstallationAdminOnly(t *testing.T) {
	f := newFixture(t, true)
	ownerID, ownerUserID, projectID := f.seedMember(projects.RoleOwner)

	if _, err := f.authz.Can(f.ctx, ownerID, projectID, auth.CapArtifactRead); err != nil {
		t.Fatalf("Can: %v", err)
	}

	// An OWNER of the project, but not an installation admin.
	if _, err := f.svc.List(f.ctx, ownerID, ownerUserID, paging.Options{}); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("List as project OWNER = %v, want ErrForbidden", err)
	}

	f.grantInstallationAdmin(ownerUserID)
	if _, err := f.svc.List(f.ctx, ownerID, ownerUserID, paging.Options{}); err != nil {
		t.Fatalf("List as installation admin: %v", err)
	}
}

// TestRecordingAnUnknownCredentialWorks is the row an audit table with
// foreign keys could never hold, and the single most interesting one here:
// a token naming a credential that does not exist.
func TestRecordingAnUnknownCredentialWorks(t *testing.T) {
	f := newFixture(t, true)
	adminPrincipal, adminUserID, _ := f.seedMember(projects.RoleOwner)
	f.grantInstallationAdmin(adminUserID)

	ghost := uuid.New()
	f.rec.Record(f.ctx, security.Event{
		Kind:         security.KindAuthenticationFailed,
		CredentialID: &ghost,
		Reason:       "presented token did not resolve to an active credential",
	})

	page, err := f.svc.List(f.ctx, adminPrincipal, adminUserID, paging.Options{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, e := range page.Items {
		if e.CredentialID != nil && *e.CredentialID == ghost {
			found = true
			if e.Kind != security.KindAuthenticationFailed {
				t.Fatalf("kind = %q", e.Kind)
			}
		}
	}
	if !found {
		t.Fatalf("the unknown-credential event was not recorded: %+v", page.Items)
	}
}
