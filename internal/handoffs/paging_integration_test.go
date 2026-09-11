package handoffs_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// TestListPagesNewestFirstAcrossASharedTimestamp walks the list one handoff
// at a time with every row forced to the same created_at — the case a
// timestamp-only cursor silently collapses to a single row. The
// (created_at, id) row-value comparison is what keeps the walk exact.
func TestListPagesNewestFirstAcrossASharedTimestamp(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	const total = 5
	for i := range total {
		if _, err := f.svc.Create(ctx, principalID, nil, projectID, nil,
			handoffs.Body{Summary: fmt.Sprintf("handoff %d", i)}); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	if err := db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE handoffs SET created_at = timestamptz '2026-09-11 12:00:00+00' WHERE project_id = $1`, projectID)
		return err
	}); err != nil {
		t.Fatalf("collapse created_at: %v", err)
	}

	whole, err := f.svc.List(ctx, principalID, projectID, nil, paging.Options{Limit: total})
	if err != nil {
		t.Fatalf("List (single page): %v", err)
	}
	if len(whole.Items) != total || whole.NextCursor != "" {
		t.Fatalf("single page: %d items, cursor %q; want %d items and no cursor", len(whole.Items), whole.NextCursor, total)
	}

	seen := map[string]int{}
	cursor := ""
	for page := 0; ; page++ {
		if page > total+1 {
			t.Fatal("paging did not terminate")
		}
		got, err := f.svc.List(ctx, principalID, projectID, nil, paging.Options{Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatalf("List page %d: %v", page, err)
		}
		for _, h := range got.Items {
			seen[h.ID.String()]++
		}
		if got.NextCursor == "" {
			break
		}
		cursor = got.NextCursor
	}

	if len(seen) != total {
		t.Errorf("walked %d distinct handoffs, want %d", len(seen), total)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("handoff %s appeared %d times, want exactly 1", id, count)
		}
	}
}

// TestListPagingRespectsTheStatusFilter pins what the cursor does and does
// not carry: it is a position in an ordering, not a saved query, so a page
// taken with one filter must stay within that filter — and a caller who
// changes the filter mid-walk gets the new filter applied, not the old one.
func TestListPagingRespectsTheStatusFilter(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	const total = 3
	for i := range total {
		if _, err := f.svc.Create(ctx, principalID, nil, projectID, nil,
			handoffs.Body{Summary: fmt.Sprintf("handoff %d", i)}); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	open := "open"
	first, err := f.svc.List(ctx, principalID, projectID, &open, paging.Options{Limit: 1})
	if err != nil {
		t.Fatalf("List(open): %v", err)
	}
	if len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first open page: %d items, cursor %q; want 1 item and a cursor", len(first.Items), first.NextCursor)
	}

	// Same cursor, different filter: no handoff is closed, so the page must
	// be empty rather than leaking rows the filter excludes.
	closed := "closed"
	other, err := f.svc.List(ctx, principalID, projectID, &closed, paging.Options{Limit: 10, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("List(closed) with an open cursor: %v", err)
	}
	if len(other.Items) != 0 {
		t.Errorf("closed filter returned %d handoffs, want 0 — the filter, not the cursor, decides membership", len(other.Items))
	}
}
