package projects_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// TestListMembersReturnsEveryMemberOfTheProject is the mirror-image check
// of TestListForPrincipalOnlyReturnsCallerMemberships: given one project
// with two members, ListMembers must return both rows (not just the
// caller's own), and must not leak members of an unrelated project.
func TestListMembersReturnsEveryMemberOfTheProject(t *testing.T) {
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

	store := projects.NewStore()

	aliceID, aliceUserID := seedPrincipal(t, ctx, ownerPool, "alice-listmembers@example.test")
	bobID, _ := seedPrincipal(t, ctx, ownerPool, "bob-listmembers@example.test")

	var project projects.Project
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: aliceID}, func(tx pgx.Tx) error {
		var err error
		project, err = store.CreateForOwner(ctx, tx, aliceUserID, aliceID, "listmembers-alice/proj", "Alice Proj")
		return err
	})
	if err != nil {
		t.Fatalf("create alice's project: %v", err)
	}

	var otherProject projects.Project
	otherAliceID, otherAliceUserID := seedPrincipal(t, ctx, ownerPool, "alice2-listmembers@example.test")
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: otherAliceID}, func(tx pgx.Tx) error {
		var err error
		otherProject, err = store.CreateForOwner(ctx, tx, otherAliceUserID, otherAliceID, "listmembers-alice2/proj", "Other Proj")
		return err
	})
	if err != nil {
		t.Fatalf("create the unrelated project: %v", err)
	}

	// RLS check: alice isn't a member of otherProject, so ListMembers must
	// come back empty for it — not an error, not other-project's rows.
	var leaked []projects.Membership
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: aliceID}, func(tx pgx.Tx) error {
		var err error
		leaked, err = store.ListMembers(ctx, tx, otherProject.ID)
		return err
	})
	if err != nil {
		t.Fatalf("ListMembers(otherProject) as non-member: %v", err)
	}
	if len(leaked) != 0 {
		t.Errorf("ListMembers(otherProject) as non-member = %+v, want empty (RLS should hide it)", leaked)
	}

	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: aliceID}, func(tx pgx.Tx) error {
		_, err := store.AddMember(ctx, tx, project.ID, bobID, projects.RoleReader)
		return err
	})
	if err != nil {
		t.Fatalf("add bob as READER: %v", err)
	}

	var members []projects.Membership
	err = db.WithPrincipal(ctx, appPool, db.Principal{ID: aliceID}, func(tx pgx.Tx) error {
		var err error
		members, err = store.ListMembers(ctx, tx, project.ID)
		return err
	})
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}

	if len(members) != 2 {
		t.Fatalf("ListMembers = %+v, want 2 members", members)
	}
	if members[0].PrincipalID != aliceID || members[0].Role != projects.RoleOwner {
		t.Errorf("members[0] = %+v, want alice as OWNER", members[0])
	}
	if members[1].PrincipalID != bobID || members[1].Role != projects.RoleReader {
		t.Errorf("members[1] = %+v, want bob as READER", members[1])
	}
}
