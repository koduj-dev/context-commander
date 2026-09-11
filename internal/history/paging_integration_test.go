package history_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// TestListHistoryObjectsPagesForward walks a conversation oldest-first, one
// object per page, with every object forced to the same created_at — the
// collision a timestamp-only cursor gets wrong.
//
// This list ascends while the others descend, so the cursor comparison is
// "(created_at, id) > position" here. Getting that direction backwards would
// return the same first page forever, which is why the walk asserts it
// terminates and covers every object exactly once.
func TestListHistoryObjectsPagesForward(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	conv, err := f.svc.CreateConversation(ctx, principalID, projectID)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	session, err := f.svc.CreateExternalSession(ctx, principalID, projectID, conv.ID,
		"anthropic", "claude-code", "sess-paging", nil, "started", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateExternalSession: %v", err)
	}

	const total = 6
	for i := range total {
		if _, err := f.svc.AppendHistoryObject(ctx, principalID, projectID, conv.ID, session.ID,
			"raw", nil, fmt.Appendf(nil, "turn %d", i)); err != nil {
			t.Fatalf("AppendHistoryObject %d: %v", i, err)
		}
	}
	if err := db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE history_objects SET created_at = timestamptz '2026-09-11 12:00:00+00' WHERE conversation_id = $1`,
			conv.ID)
		return err
	}); err != nil {
		t.Fatalf("collapse created_at: %v", err)
	}

	whole, err := f.svc.ListHistoryObjects(ctx, principalID, projectID, conv.ID, paging.Options{Limit: total})
	if err != nil {
		t.Fatalf("ListHistoryObjects (single page): %v", err)
	}
	if len(whole.Items) != total || whole.NextCursor != "" {
		t.Fatalf("single page: %d items, cursor %q; want %d items and no cursor", len(whole.Items), whole.NextCursor, total)
	}

	var order []string
	cursor := ""
	for page := 0; ; page++ {
		if page > total+1 {
			t.Fatal("paging did not terminate — an ascending list with a descending comparison repeats its first page")
		}
		got, err := f.svc.ListHistoryObjects(ctx, principalID, projectID, conv.ID, paging.Options{Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatalf("ListHistoryObjects page %d: %v", page, err)
		}
		for _, obj := range got.Items {
			order = append(order, obj.ID.String())
			// Paging must not change what a row contains: the body is
			// still decrypted on every page, not just the first.
			if len(obj.Body) == 0 {
				t.Errorf("object %s came back with an empty body", obj.ID)
			}
		}
		if got.NextCursor == "" {
			break
		}
		cursor = got.NextCursor
	}

	if len(order) != total {
		t.Fatalf("walked %d objects one page at a time, want %d", len(order), total)
	}
	for i, obj := range whole.Items {
		if order[i] != obj.ID.String() {
			t.Errorf("position %d: paged %s, single page had %s — order must match", i, order[i], obj.ID)
		}
	}
}
