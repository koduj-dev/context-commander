package contextpack

// An internal test, unlike every other test in this package: it needs the
// hookAfterContentRead seam, which exists precisely because the property
// under test is invisible from outside. Nothing an assembly does publishes
// an event of its own, so "the cursor was taken before the content was
// read" cannot be observed by any external caller — only by landing a write
// exactly in between.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/skills"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

// TestEventCursorDoesNotOutrunTheContent is the regression test for the
// review finding that the continuation cursor could skip a change the pack
// did not contain.
//
// The pack is assembled from several independent transactions, so it is not
// a consistent snapshot. A write that lands mid-assembly must therefore end
// up on the *far* side of the cursor: the pack may carry the old version,
// but event.list from the cursor has to report the change. Before the fix
// the cursor was taken last, so such a write fell before the cursor while
// its content was missing from the pack — invisible from both directions.
func TestEventCursorDoesNotOutrunTheContent(t *testing.T) {
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

	store, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatalf("filesystem.New: %v", err)
	}

	projStore := projects.NewStore()
	authz := auth.NewAuthzService(appPool, projStore)
	artifactsSvc := artifacts.NewService(appPool, cryptoProvider, store, authz, gates.NewEngine(appPool))
	eventsSvc := events.NewService(appPool, authz)
	svc := NewService(appPool, projStore, authz, artifactsSvc,
		skills.NewService(appPool, authz, organizations.NewStore(), projStore, store, installation.NewStore()),
		handoffs.NewService(appPool, cryptoProvider, authz), eventsSvc)

	principalID, projectID := uuid.New(), uuid.New()
	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, principalID, userID); err != nil {
			return err
		}
		namespaceID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			namespaceID, "ns-"+namespaceID.String()[:8], userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'cc', 'CC')`,
			projectID, namespaceID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'CONTRIBUTOR')`,
			projectID, principalID)
		return err
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	const path = ConventionalArtifactPrefix + "overview.md"
	if _, err := artifactsSvc.Write(ctx, principalID, nil, projectID, path, "text/markdown", []byte("version one"), 0); err != nil {
		t.Fatalf("write v1: %v", err)
	}

	// The interleaving: a second client updates the artifact after the pack
	// has read its content — the exact window where the old ordering took
	// the cursor, putting the change behind a cursor that would never
	// report it.
	hookAfterContentRead = func() {
		if _, err := artifactsSvc.Write(ctx, principalID, nil, projectID, path, "text/markdown", []byte("version two"), 1); err != nil {
			t.Errorf("concurrent write v2: %v", err)
		}
	}
	t.Cleanup(func() { hookAfterContentRead = nil })

	pack, err := svc.Bootstrap(ctx, principalID, projectID, Options{})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	hookAfterContentRead = nil

	if len(pack.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(pack.Artifacts))
	}
	if pack.Events.NextCursor == "" {
		t.Fatal("pack has no event cursor")
	}

	// Whichever version the pack happened to catch, the contract is the
	// same: what the pack does not contain must still be reachable from the
	// cursor.
	if pack.Artifacts[0].Content == "version two" {
		return // The read landed after the write; nothing was missed.
	}

	page, err := eventsSvc.List(ctx, principalID, projectID, paging.Options{Cursor: pack.Events.NextCursor})
	if err != nil {
		t.Fatalf("event.list from the pack's cursor: %v", err)
	}
	// internal/artifacts publishes "artifact.updated" against resource type
	// "artifact_version" — the version is the thing that came into
	// existence, the artifact itself already existed.
	for _, e := range page.Items {
		if e.Type == "artifact.updated" {
			return // The change is visible from the cursor. Contract held.
		}
	}
	t.Fatalf("the pack carries %q and its cursor reports no artifact event — "+
		"the update is invisible from both the content and the event stream, which is exactly the bug this test pins. "+
		"events after the cursor: %d", pack.Artifacts[0].Content, len(page.Items))
}
