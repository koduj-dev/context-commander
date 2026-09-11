package secrets_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// TestListPagesInNameOrder walks a project's secret references one page at a
// time. Unlike the timestamp-ordered lists, the key here is the name itself:
// (project_id, name) is unique, so the name alone is a complete position and
// needs no tiebreaker — this test is what says that claim still holds.
//
// Names are created out of order on purpose, so a walk that merely followed
// insertion order would visit them in the wrong sequence.
func TestListPagesInNameOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, projects.RoleContributor)

	created := []string{"npm", "github", "stripe", "aws", "datadog"}
	for _, name := range created {
		if _, err := f.svc.Create(ctx, principalID, projectID, name, fmt.Appendf(nil, "value-of-%s", name)); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	wantOrder := []string{"aws", "datadog", "github", "npm", "stripe"}

	var order []string
	cursor := ""
	for page := 0; ; page++ {
		if page > len(created)+1 {
			t.Fatal("paging did not terminate")
		}
		got, err := f.svc.List(ctx, principalID, projectID, paging.Options{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("List page %d: %v", page, err)
		}
		for _, ref := range got.Items {
			order = append(order, ref.Name)
		}
		if got.NextCursor == "" {
			break
		}
		cursor = got.NextCursor
	}

	if len(order) != len(wantOrder) {
		t.Fatalf("walked %d references (%v), want %d", len(order), order, len(wantOrder))
	}
	for i, name := range wantOrder {
		if order[i] != name {
			t.Errorf("position %d = %q, want %q (name order, not insertion order)", i, order[i], name)
		}
	}
}

// TestListRejectsACursorFromAnotherList — secret names are ordered text, so
// this list's cursor is shaped differently from every other one. Decoding
// has to notice.
func TestListRejectsACursorFromAnotherList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, projects.RoleContributor)

	if _, err := f.svc.List(ctx, principalID, projectID, paging.Options{Cursor: paging.EncodeSeq(7)}); err == nil {
		t.Fatal("List accepted a sequence cursor from a differently-ordered list")
	}
}
