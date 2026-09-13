package mcp_test

import (
	"context"
	"testing"
)

type meResult struct {
	PrincipalID         string `json:"principal_id"`
	Kind                string `json:"kind"`
	Email               string `json:"email"`
	DisplayName         string `json:"display_name"`
	IsInstallationAdmin bool   `json:"is_installation_admin"`
}

type myProjectResult struct {
	Project    string `json:"project"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
	Role       string `json:"role"`
}

type myOrganizationResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// An agent holding a token could previously act on a project only if someone
// had told it the project's path out of band — every tool took a
// "namespace/slug" and nothing produced one. These three answer "who am I
// and what can I reach", which is where an agent's first turn starts.
func TestAgentCanDiscoverItselfAndItsProjects(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.newUserWithToken("discovery@example.test")

	session, err := f.connect(ctx, token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	me := callTool[meResult](t, ctx, session, "me", map[string]any{})
	if me.Kind != "user" || me.Email != "discovery@example.test" {
		t.Fatalf("me: got %+v", me)
	}
	if me.PrincipalID == "" {
		t.Error("me: no principal_id — it is what every project membership and audit row names")
	}

	// Nothing yet: an empty list, not a refusal.
	if got := callTool[[]myProjectResult](t, ctx, session, "project.list", map[string]any{}); len(got) != 0 {
		t.Fatalf("project.list before creating one: got %+v", got)
	}

	callTool[projectResult](t, ctx, session, "project.create", map[string]any{
		"project": "discovery/first", "name": "First",
	})

	projects := callTool[[]myProjectResult](t, ctx, session, "project.list", map[string]any{})
	if len(projects) != 1 {
		t.Fatalf("project.list: got %d projects, want 1", len(projects))
	}
	// The path is returned in the one form every other tool takes, so the
	// obvious next call needs no string assembly by the caller.
	if projects[0].Project != "discovery/first" || projects[0].Role != "OWNER" {
		t.Errorf("project.list: got %+v", projects[0])
	}

	orgs := callTool[[]myOrganizationResult](t, ctx, session, "organization.list", map[string]any{})
	if len(orgs) != 0 {
		t.Fatalf("organization.list: got %+v, want none", orgs)
	}

	org := callTool[organizationResult](t, ctx, session, "organization.create", map[string]any{
		"name": "Discovery Org",
	})
	orgs = callTool[[]myOrganizationResult](t, ctx, session, "organization.list", map[string]any{})
	if len(orgs) != 1 || orgs[0].ID != org.ID || orgs[0].Role != "OWNER" {
		t.Fatalf("organization.list after create: got %+v", orgs)
	}

	members := callTool[[]organizationMemberResult](t, ctx, session, "organization.list_members",
		map[string]any{"organization_id": org.ID})
	if len(members) != 1 {
		t.Fatalf("organization.list_members: got %d, want 1", len(members))
	}
	// Named, not just identified: a list of UUIDs answers "who is in this
	// organization" with nothing a model can use.
	if members[0].Email != "discovery@example.test" {
		t.Errorf("organization.list_members: got %+v, want the member's email", members[0])
	}
}

type organizationResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type organizationMemberResult struct {
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}
