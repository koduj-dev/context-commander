package handoffs_test

import (
	"context"
	"testing"

	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// ListWaiting answers "is anything waiting on me" across every membership,
// and must answer it about the caller's projects only.
//
// The risk this exists to catch: the query spans projects, so unlike every
// other read in this package it has no project id to constrain it. If RLS
// stopped applying, or the membership join were dropped, the symptom would
// be another tenant's work appearing on someone's dashboard — visible as
// data, not as an error.
func TestListWaitingSpansOwnProjectsOnly(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()

	mine, myProject := f.seedProject(t, ownerPool, projects.RoleContributor)
	stranger, strangerProject := f.seedProject(t, ownerPool, projects.RoleOwner)

	if _, err := f.svc.Create(ctx, mine, nil, myProject, nil,
		handoffs.Body{Summary: "reindex, then flip the alias"}); err != nil {
		t.Fatalf("Create in my project: %v", err)
	}
	if _, err := f.svc.Create(ctx, stranger, nil, strangerProject, nil,
		handoffs.Body{Summary: "not yours"}); err != nil {
		t.Fatalf("Create in the stranger's project: %v", err)
	}

	waiting, err := f.svc.ListWaiting(ctx, mine)
	if err != nil {
		t.Fatalf("ListWaiting: %v", err)
	}
	if len(waiting) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(waiting), waiting)
	}
	if waiting[0].ProjectID != myProject {
		t.Errorf("got project %s, want %s — another tenant's work reached this list",
			waiting[0].ProjectID, myProject)
	}
	// The path is the reason the answer is usable: "there is work in 3f2a…"
	// sends the reader off to find out which project that is.
	if waiting[0].ProjectPath == "" {
		t.Error("ProjectPath is empty — the row cannot be acted on")
	}
	if waiting[0].Role != string(projects.RoleContributor) {
		t.Errorf("Role = %q, want CONTRIBUTOR", waiting[0].Role)
	}
}

// One row per project, and it is the newest — the same handoff Current
// returns for that project. Three open handoffs in one project are one line
// on a dashboard, not three.
func TestListWaitingReturnsTheNewestPerProject(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	if _, err := f.svc.Create(ctx, principalID, nil, projectID, nil,
		handoffs.Body{Summary: "older"}); err != nil {
		t.Fatalf("Create older: %v", err)
	}
	newest, err := f.svc.Create(ctx, principalID, nil, projectID, nil,
		handoffs.Body{Summary: "newer"})
	if err != nil {
		t.Fatalf("Create newer: %v", err)
	}

	waiting, err := f.svc.ListWaiting(ctx, principalID)
	if err != nil {
		t.Fatalf("ListWaiting: %v", err)
	}
	if len(waiting) != 1 {
		t.Fatalf("got %d rows, want 1 per project: %+v", len(waiting), waiting)
	}
	if waiting[0].ID != newest.ID {
		t.Errorf("got handoff %s, want the newest %s", waiting[0].ID, newest.ID)
	}

	// The same row Current returns, which is what makes the dashboard and
	// the project screen agree.
	current, err := f.svc.Current(ctx, principalID, projectID)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.ID != waiting[0].ID {
		t.Errorf("ListWaiting says %s, Current says %s — the two disagree", waiting[0].ID, current.ID)
	}
}

// A closed handoff is finished work and must drop off the list; nothing else
// distinguishes "waiting" from "happened".
func TestListWaitingDropsClosedWork(t *testing.T) {
	f, ownerPool := newFixture(t)
	ctx := context.Background()
	principalID, projectID := f.seedProject(t, ownerPool, projects.RoleContributor)

	h, err := f.svc.Create(ctx, principalID, nil, projectID, nil, handoffs.Body{Summary: "work"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.svc.Close(ctx, principalID, nil, projectID, h.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	waiting, err := f.svc.ListWaiting(ctx, principalID)
	if err != nil {
		t.Fatalf("ListWaiting: %v", err)
	}
	if len(waiting) != 0 {
		t.Fatalf("closed handoff still listed: %+v", waiting)
	}
}
