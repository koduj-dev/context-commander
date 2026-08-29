package audit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type fixture struct {
	svc            *audit.Service
	crypto         *crypto.Provider
	ownerPool      *db.Pool
	appPool        *db.Pool
	projectID      uuid.UUID
	ownerPrincipal uuid.UUID
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

	ownerPrincipal := uuid.New()
	projectID := uuid.New()
	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture Owner"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`,
			ownerPrincipal, userID); err != nil {
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
			projectID, ownerPrincipal)
		return err
	})
	if err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	rootKeyB64, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatalf("GenerateRootKeyBase64: %v", err)
	}
	t.Setenv("CC_ROOT_KEY", rootKeyB64)
	root, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
	if err != nil {
		t.Fatalf("NewEnvRootKeyProvider: %v", err)
	}
	cryptoProvider := crypto.NewProvider(root)

	return &fixture{
		svc:            audit.NewService(appPool, auth.NewAuthzService(appPool, projects.NewStore())),
		crypto:         cryptoProvider,
		ownerPool:      ownerPool,
		appPool:        appPool,
		projectID:      projectID,
		ownerPrincipal: ownerPrincipal,
	}
}

// addMember seeds an additional project member with the given role, via
// the RLS-exempt owner pool (fixture setup only).
func (f *fixture) addMember(t *testing.T, ctx context.Context, role projects.Role) uuid.UUID {
	t.Helper()
	principalID := uuid.New()
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture Member"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, principalID, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`,
			f.projectID, principalID, role)
		return err
	})
	if err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return principalID
}

func (f *fixture) write(t *testing.T, ctx context.Context, action, resourceType string) {
	t.Helper()
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		return audit.Write(ctx, tx, audit.Entry{
			ProjectID: &f.projectID, PrincipalID: &f.ownerPrincipal,
			Action: action, ResourceType: resourceType, Result: audit.ResultAllow,
		})
	})
	if err != nil {
		t.Fatalf("audit.Write: %v", err)
	}
}

func TestWriteAndQueryRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.write(t, ctx, "artifact.write", "artifact_version")
	f.write(t, ctx, "artifact.delete", "artifact_version")
	f.write(t, ctx, "handoff.create", "handoff")

	var all []audit.Record
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		var err error
		all, err = audit.Query(ctx, tx, f.projectID, audit.QueryOptions{})
		return err
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("Query returned %d rows, want 3", len(all))
	}
	// most-recent-first
	if all[0].Action != "handoff.create" {
		t.Errorf("first row action = %q, want handoff.create (most recent first)", all[0].Action)
	}

	var filtered []audit.Record
	err = db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		var err error
		filtered, err = audit.Query(ctx, tx, f.projectID, audit.QueryOptions{ResourceType: "artifact_version"})
		return err
	})
	if err != nil {
		t.Fatalf("Query filtered: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("Query(resource_type=artifact_version) returned %d rows, want 2", len(filtered))
	}

	var byAction []audit.Record
	err = db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		var err error
		byAction, err = audit.Query(ctx, tx, f.projectID, audit.QueryOptions{Action: "artifact.delete"})
		return err
	})
	if err != nil {
		t.Fatalf("Query by action: %v", err)
	}
	if len(byAction) != 1 {
		t.Fatalf("Query(action=artifact.delete) returned %d rows, want 1", len(byAction))
	}
}

func TestQueryTimeRangeFilter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.write(t, ctx, "artifact.write", "artifact_version")

	future := time.Now().Add(time.Hour)
	var empty []audit.Record
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		var err error
		empty, err = audit.Query(ctx, tx, f.projectID, audit.QueryOptions{Since: &future})
		return err
	})
	if err != nil {
		t.Fatalf("Query since future: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("Query(since=future) returned %d rows, want 0", len(empty))
	}
}

func TestWriteWithPayloadRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	resourceID := uuid.New()
	plaintext := []byte(`{"diff":"before -> after"}`)

	var auditLogID int64
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		if err := audit.WriteWithPayload(ctx, tx, audit.Entry{
			ProjectID: &f.projectID, PrincipalID: &f.ownerPrincipal,
			Action: "handoff.create", ResourceType: "handoff", ResourceID: &resourceID,
			Result: audit.ResultAllow,
		}, plaintext, f.crypto); err != nil {
			return err
		}
		rows, err := audit.Query(ctx, tx, f.projectID, audit.QueryOptions{Action: "handoff.create"})
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row after WriteWithPayload, got %d", len(rows))
		}
		auditLogID = rows[0].ID
		return nil
	})
	if err != nil {
		t.Fatalf("WriteWithPayload: %v", err)
	}

	var got []byte
	err = db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		var err error
		got, err = audit.ReadPayload(ctx, tx, f.crypto, f.projectID, auditLogID, resourceID)
		return err
	})
	if err != nil {
		t.Fatalf("ReadPayload: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("ReadPayload = %q, want %q", got, plaintext)
	}
}

func TestQueryIsolatedAcrossProjects(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.write(t, ctx, "artifact.write", "artifact_version")

	otherProjectID := uuid.New()
	err := db.WithPrincipal(ctx, f.appPool, db.Principal{ID: f.ownerPrincipal}, func(tx pgx.Tx) error {
		rows, err := audit.Query(ctx, tx, otherProjectID, audit.QueryOptions{})
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			t.Errorf("Query for an unrelated project returned %d rows, want 0", len(rows))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Query other project: %v", err)
	}
}

func TestServiceQueryDeniedForContributor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.write(t, ctx, "artifact.write", "artifact_version")

	contributorID := f.addMember(t, ctx, projects.RoleContributor)

	_, err := f.svc.Query(ctx, contributorID, f.projectID, audit.QueryOptions{})
	if !errors.Is(err, audit.ErrForbidden) {
		t.Fatalf("Query by CONTRIBUTOR: got err=%v, want ErrForbidden (audit.read is ADMIN/OWNER-only)", err)
	}
}

func TestServiceQueryAllowedForOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.write(t, ctx, "artifact.write", "artifact_version")

	rows, err := f.svc.Query(ctx, f.ownerPrincipal, f.projectID, audit.QueryOptions{})
	if err != nil {
		t.Fatalf("Query by OWNER: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Query returned %d rows, want 1", len(rows))
	}
}
