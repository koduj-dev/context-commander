package history_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/projects"
)

type fixture struct {
	svc     *history.Service
	appPool *db.Pool
}

func (f *fixture) seedProject(t *testing.T, ownerPool *db.Pool, role projects.Role) (principalID, projectID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	principalID, projectID = uuid.New(), uuid.New()

	err := db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
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
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`,
			projectID, principalID, role)
		return err
	})
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return principalID, projectID
}

func newFixture(t *testing.T) (*fixture, *db.Pool) {
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
	authz := auth.NewAuthzService(appPool, projects.NewStore())

	return &fixture{
		svc:     history.NewService(appPool, cryptoProvider, authz),
		appPool: appPool,
	}, ownerPool
}

func TestLineageGraphAcrossCompaction(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	conv, err := f.svc.CreateConversation(ctx, principalID, projectID)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	sessionA, err := f.svc.CreateExternalSession(ctx, principalID, projectID, conv.ID,
		"anthropic", "claude-code", "sess-A", nil, "started", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateExternalSession A: %v", err)
	}
	sessionB, err := f.svc.CreateExternalSession(ctx, principalID, projectID, conv.ID,
		"anthropic", "claude-code", "sess-B", &sessionA.ID, "compacted_from", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateExternalSession B: %v", err)
	}

	rawA, err := f.svc.AppendHistoryObject(ctx, principalID, projectID, conv.ID, sessionA.ID, "raw", nil, []byte("raw transcript A"))
	if err != nil {
		t.Fatalf("AppendHistoryObject raw: %v", err)
	}
	compactB, err := f.svc.AppendHistoryObject(ctx, principalID, projectID, conv.ID, sessionB.ID, "compacted", &rawA.ID, []byte("compacted summary of A"))
	if err != nil {
		t.Fatalf("AppendHistoryObject compacted: %v", err)
	}

	objs, err := f.svc.ListHistoryObjects(ctx, principalID, projectID, conv.ID)
	if err != nil {
		t.Fatalf("ListHistoryObjects: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("ListHistoryObjects returned %d objects, want 2", len(objs))
	}
	bodies := map[uuid.UUID]string{}
	for _, o := range objs {
		bodies[o.ID] = string(o.Body)
	}
	if bodies[rawA.ID] != "raw transcript A" || bodies[compactB.ID] != "compacted summary of A" {
		t.Fatalf("decrypted bodies mismatch: %+v", bodies)
	}

	edges, err := f.svc.GetLineage(ctx, principalID, projectID, conv.ID)
	if err != nil {
		t.Fatalf("GetLineage: %v", err)
	}
	wantDerived := false
	wantCompactedFrom := false
	for _, e := range edges {
		if e.Kind == "derived" && e.FromID == rawA.ID && e.ToID == compactB.ID {
			wantDerived = true
		}
		if e.Kind == "compacted_from" && e.FromID == sessionA.ID && e.ToID == sessionB.ID {
			wantCompactedFrom = true
		}
	}
	if !wantDerived {
		t.Errorf("GetLineage missing derived edge rawA->compactB: %+v", edges)
	}
	if !wantCompactedFrom {
		t.Errorf("GetLineage missing compacted_from edge sessionA->sessionB: %+v", edges)
	}
}

func TestHistoryObjectsAreAppendOnly(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	conv, err := f.svc.CreateConversation(ctx, principalID, projectID)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	session, err := f.svc.CreateExternalSession(ctx, principalID, projectID, conv.ID,
		"anthropic", "claude-code", "sess-A", nil, "started", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateExternalSession: %v", err)
	}
	obj, err := f.svc.AppendHistoryObject(ctx, principalID, projectID, conv.ID, session.ID, "raw", nil, []byte("original"))
	if err != nil {
		t.Fatalf("AppendHistoryObject: %v", err)
	}

	// No Service.Update exists — and the database itself refuses an UPDATE
	// even attempted directly against cc_app, since cc_app has no UPDATE
	// grant on history_objects (migrations/000011_history.up.sql).
	err = db.WithPrincipal(ctx, f.appPool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE history_objects SET content_hash = 'tampered' WHERE id = $1`, obj.ID)
		return err
	})
	if err == nil {
		t.Fatal("direct UPDATE on history_objects succeeded, want permission error (append-only invariant broken)")
	}
}

func TestRBACReaderCannotAppend(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	readerID, projectID := f.seedProject(t, ownerPool, projects.RoleReader)

	if _, err := f.svc.CreateConversation(ctx, readerID, projectID); !errors.Is(err, history.ErrForbidden) {
		t.Fatalf("READER CreateConversation: got %v, want ErrForbidden", err)
	}
}

func TestRLSIsolationBetweenProjects(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalA, projectA := f.seedProject(t, ownerPool, projects.RoleContributor)
	principalB, projectB := f.seedProject(t, ownerPool, projects.RoleContributor)

	conv, err := f.svc.CreateConversation(ctx, principalA, projectA)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	if _, err := f.svc.ListHistoryObjects(ctx, principalB, projectA, conv.ID); !errors.Is(err, history.ErrForbidden) {
		t.Fatalf("cross-project ListHistoryObjects: got %v, want ErrForbidden", err)
	}

	listB, err := f.svc.ListHistoryObjects(ctx, principalB, projectB, conv.ID)
	if err != nil {
		t.Fatalf("ListHistoryObjects (own project): %v", err)
	}
	if len(listB) != 0 {
		t.Fatalf("projectB should see 0 history objects for a foreign conversation ID, got %d", len(listB))
	}
}
