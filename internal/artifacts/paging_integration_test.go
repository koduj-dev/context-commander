package artifacts_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/paging"
)

// TestListPagesWithoutSkippingOrRepeating walks the whole list one row at a
// time and checks the two failures keyset pagination exists to prevent.
//
// Every artifact is forced to the same updated_at first. That is the case a
// timestamp-only cursor gets wrong — "updated_at < last seen" skips every
// other row sharing that timestamp — and the (updated_at, id) row-value
// comparison is what makes the walk exact. The collision is set explicitly
// rather than hoped for: writing in a tight loop does NOT produce it, since
// each Write is its own transaction and Postgres gives each a distinct
// now(). Without this step the test passes against a broken cursor, which
// was the first version of it.
func TestListPagesWithoutSkippingOrRepeating(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	const total = 7
	for i := range total {
		path := fmt.Sprintf("note-%d.md", i)
		if _, err := f.svc.Write(ctx, f.principalID, f.clientID, f.projectID, path, "text/markdown", []byte("x"), 0); err != nil {
			t.Fatalf("Write %s: %v", path, err)
		}
	}

	if err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE artifacts SET updated_at = timestamptz '2026-09-11 12:00:00+00' WHERE project_id = $1`,
			f.projectID)
		return err
	}); err != nil {
		t.Fatalf("collapse updated_at: %v", err)
	}

	whole, err := f.svc.List(ctx, f.principalID, f.projectID, paging.Options{Limit: total})
	if err != nil {
		t.Fatalf("List (single page): %v", err)
	}
	if len(whole.Items) != total {
		t.Fatalf("single page returned %d artifacts, want %d", len(whole.Items), total)
	}
	if whole.NextCursor != "" {
		t.Errorf("a page holding every artifact still offered NextCursor %q", whole.NextCursor)
	}

	seen := make(map[string]int, total)
	var order []string
	cursor := ""
	for page := 0; ; page++ {
		if page > total+1 {
			t.Fatal("paging did not terminate")
		}
		got, err := f.svc.List(ctx, f.principalID, f.projectID, paging.Options{Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatalf("List page %d: %v", page, err)
		}
		for _, a := range got.Items {
			seen[a.LogicalPath]++
			order = append(order, a.LogicalPath)
		}
		if got.NextCursor == "" {
			break
		}
		cursor = got.NextCursor
	}

	if len(order) != total {
		t.Errorf("walked %d artifacts one page at a time, want %d", len(order), total)
	}
	for path, count := range seen {
		if count != 1 {
			t.Errorf("%s appeared %d times across pages, want exactly 1", path, count)
		}
	}
	for i, a := range whole.Items {
		if i < len(order) && order[i] != a.LogicalPath {
			t.Errorf("position %d: paged %q, single page had %q — order must match", i, order[i], a.LogicalPath)
		}
	}
}

// TestListRejectsACursorFromAnotherList guards the wire contract: cursors
// are opaque, so the only protection against one being echoed to the wrong
// endpoint is that decoding refuses it.
func TestListRejectsACursorFromAnotherList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.svc.List(ctx, f.principalID, f.projectID, paging.Options{Cursor: paging.EncodeSeq(12)})
	if err == nil {
		t.Fatal("List accepted a sequence cursor from a differently-ordered list")
	}
}
