package mcp_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tools this server offers, exactly.
//
// Asserted rather than merely documented because "is the MCP surface
// complete" is otherwise unanswerable without reading nine files: a domain
// can gain an operation in HTTP and never get a tool, and nothing notices.
// Changing this list is how a change to the surface gets stated out loud.
//
// The rule this list encodes: a domain that can be written over MCP must be
// readable over MCP. Writes without the matching reads leave an agent able
// to create things it can never find again.
var expectedTools = []string{
	"artifact.delete",
	"artifact.history",
	"artifact.list",
	"artifact.read",
	"artifact.stat",
	"artifact.write",
	"audit.list",
	"context.bootstrap",
	"event.list",
	"gate.bind",
	"gate.list_bindings",
	"gate.unbind",
	"handoff.claim",
	"handoff.close",
	"handoff.create",
	"handoff.current",
	"handoff.get",
	"handoff.list",
	"history.append_object",
	"history.create_conversation",
	"history.create_external_session",
	"history.get_lineage",
	"history.list_conversations",
	"history.list_objects",
	"installation.create_service_principal",
	"installation.create_user",
	"installation.issue_token",
	"installation.list_admins",
	"installation.list_service_principals",
	"installation.list_tokens",
	"installation.list_users",
	"installation.revoke_token",
	"me",
	"organization.add_member",
	"organization.create",
	"organization.list",
	"organization.list_members",
	"project.add_member",
	"project.create",
	"project.list",
	"project.list_members",
	"project.resolve",
	"secret.create",
	"secret.delete",
	"secret.list",
	// No secret.resolve, deliberately: it returns plaintext, and handing
	// that to any MCP-connected agent is a decision nobody has made. It
	// stays reachable only from adapters/cli. See tools_secret.go.
	"skill.get",
	"skill.install",
	"skill.list_effective",
	"skill.override",
	"skill.pin",
	"skill.promote",
	"skill.publish",
	"skill.search",
	"skill.unpin",
	"skill.versions",
}

func TestToolInventory(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.newUserWithToken("inventory@example.test")

	session, err := f.connect(ctx, token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	var got []string
	for tool, err := range session.Tools(ctx, &sdkmcp.ListToolsParams{}) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		got = append(got, tool.Name)
	}
	sort.Strings(got)

	want := append([]string(nil), expectedTools...)
	sort.Strings(want)

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		missing := difference(want, got)
		extra := difference(got, want)
		t.Errorf("tool inventory differs\n  missing: %v\n  unexpected: %v\n"+
			"If this is deliberate, update expectedTools and say why in the commit.",
			missing, extra)
	}
}

// Every tool must describe itself. A model picks a tool by its description,
// so an empty one is a tool that will be used wrongly or not at all.
func TestEveryToolIsDescribed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.newUserWithToken("described@example.test")

	session, err := f.connect(ctx, token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	for tool, err := range session.Tools(ctx, &sdkmcp.ListToolsParams{}) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		if len(strings.TrimSpace(tool.Description)) < 30 {
			t.Errorf("%s: description is %q — too short to choose the tool by",
				tool.Name, tool.Description)
		}
	}
}

func difference(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, s := range b {
		inB[s] = true
	}
	var out []string
	for _, s := range a {
		if !inB[s] {
			out = append(out, s)
		}
	}
	return out
}
