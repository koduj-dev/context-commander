package http_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestE2ECrossClientHandoff is NEXT.md Phase 1's reference scenario, run
// over the real HTTP surface: client A writes state and hands off; client B,
// holding a DIFFERENT credential and having seen none of A's conversation,
// bootstraps the project, takes the work over, and finishes it. Then A's
// token is revoked and must stop working immediately.
//
// It is deliberately one long test rather than six small ones. The claim
// being made is that these steps compose into a workflow — a suite of
// independently passing steps would not establish that, and each step here
// consumes output the previous one produced (a handoff ID B never sees, a
// skill version ID only the pack resolves) exactly as a real client does.
func TestE2ECrossClientHandoff(t *testing.T) {
	f := newFixture(t)

	// --- Two separate identities. Not two tokens for one principal: the
	// scenario is two agents on different clients, and a shared credential
	// would prove nothing about authorization between them.
	tokenA := f.newUserWithToken("agent-a@example.test")
	tokenB := f.newUserWithToken("agent-b@example.test")

	const project = "agent-a/es-migration"
	f.createProject(tokenA, project, "ES Migration")
	base := "/v1/projects/" + project

	// B is added as a CONTRIBUTOR by A. Everything below is B acting on its
	// own credential; nothing is ever copied between the two clients.
	principalB := f.principalIDOf(tokenB)
	addMember, _ := json.Marshal(map[string]string{"principal_id": principalB, "role": "CONTRIBUTOR"})
	f.mustDo(http.MethodPost, base+"/members", tokenA, addMember, http.StatusNoContent, "add member B")

	// --- Client A does the work.
	f.mustDo(http.MethodPut, base+"/artifacts/content?path=context/overview.md&content_type=text/markdown&expected_version=0",
		tokenA, []byte("Migrating ES 7 mappings to ES 8."), http.StatusOK, "A writes the overview")
	f.mustDo(http.MethodPut, base+"/artifacts/content?path=notes/decisions.md&content_type=text/markdown&expected_version=0",
		tokenA, []byte("Chose the reindex-in-place approach."), http.StatusOK, "A writes its decisions")

	publish, _ := json.Marshal(map[string]any{
		"name": "es-review", "author": "agent-a", "origin": "local",
		"content": "# ES review\nAlways verify mapping compatibility before reindexing.",
	})
	var published struct {
		ID      string `json:"id"`
		SkillID string `json:"skill_id"`
	}
	f.mustDecode(http.MethodPost, base+"/skills", tokenA, publish, http.StatusOK, "A publishes a skill", &published)

	install, _ := json.Marshal(map[string]any{
		"scope": "project", "scope_ref": f.projectIDOf(tokenA, project),
		"skill_id": published.SkillID, "skill_version_id": published.ID, "overridable": true,
	})
	f.mustDo(http.MethodPost, base+"/skills/install", tokenA, install, http.StatusOK, "A installs the skill")

	createHandoff, _ := json.Marshal(map[string]any{
		"summary":        "products index migrated, orders index still on the old mapping",
		"completed_work": "reindexed products into es8-products-v2",
		"next_actions":   "reindex orders the same way, then flip the alias",
	})
	var handoffA struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	f.mustDecode(http.MethodPost, base+"/handoffs", tokenA, createHandoff, http.StatusOK, "A creates the handoff", &handoffA)
	if handoffA.Status != "open" {
		t.Fatalf("new handoff status = %q, want open", handoffA.Status)
	}

	// --- Client B arrives knowing only the project path and its own token.
	// One call is all it gets before it has to know what to do.
	var pack struct {
		Project struct {
			Path string `json:"path"`
		} `json:"project"`
		Role    string `json:"role"`
		Handoff *struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			NextActions string `json:"next_actions"`
		} `json:"handoff"`
		Artifacts []struct {
			LogicalPath string `json:"logical_path"`
			Content     string `json:"content"`
		} `json:"artifacts"`
		Skills []struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"skills"`
		Events struct {
			NextCursor string `json:"next_cursor"`
		} `json:"events"`
		Budget struct {
			Truncated []map[string]any `json:"truncated"`
		} `json:"budget"`
	}
	f.mustDecode(http.MethodGet, base+"/context", tokenB, nil, http.StatusOK, "B bootstraps", &pack)

	if pack.Role != "CONTRIBUTOR" {
		t.Fatalf("B's role in the pack = %q, want CONTRIBUTOR", pack.Role)
	}
	if pack.Handoff == nil || pack.Handoff.ID != handoffA.ID {
		t.Fatalf("B's pack handoff = %+v, want A's handoff %s", pack.Handoff, handoffA.ID)
	}
	if !strings.Contains(pack.Handoff.NextActions, "reindex orders") {
		t.Fatalf("B did not receive the next actions: %q", pack.Handoff.NextActions)
	}

	// The second client reads the actual CONTENT of artifacts and skills —
	// the Definition of Done's wording, and the thing UUIDs alone never
	// delivered.
	if len(pack.Skills) != 1 || !strings.Contains(pack.Skills[0].Content, "mapping compatibility") {
		t.Fatalf("B did not receive the skill's content: %+v", pack.Skills)
	}
	foundOverview := false
	for _, a := range pack.Artifacts {
		if a.LogicalPath == "context/overview.md" && strings.Contains(a.Content, "ES 7 mappings") {
			foundOverview = true
		}
	}
	if !foundOverview {
		t.Fatalf("B did not receive the conventional artifact's content: %+v", pack.Artifacts)
	}
	if pack.Events.NextCursor == "" {
		t.Fatal("B has no event cursor and cannot follow the project from here")
	}
	if len(pack.Budget.Truncated) != 0 {
		t.Fatalf("B's pack was truncated when it should have fit: %v", pack.Budget.Truncated)
	}

	// --- B takes the work over on its own credential.
	f.mustDo(http.MethodPost, base+"/handoffs/"+handoffA.ID+"/claim", tokenB, nil, http.StatusOK, "B claims the handoff")

	// A, arriving again, must now see that the work is being continued
	// rather than assume it is still waiting.
	var currentForA struct {
		ID              string `json:"id"`
		Status          string `json:"status"`
		ClaimedByClient string `json:"claimed_by_client"`
	}
	f.mustDecode(http.MethodGet, base+"/handoffs/current", tokenA, nil, http.StatusOK, "A re-reads the current handoff", &currentForA)
	if currentForA.Status != "claimed" {
		t.Fatalf("A sees status %q after B claimed it, want claimed", currentForA.Status)
	}
	// Which client holds it, not just that someone does: A must be able to
	// tell B's claim from a claim of its own whose response went missing.
	if currentForA.ClaimedByClient == "" {
		t.Fatal("the claimed handoff does not report which client claimed it")
	}

	// A second client racing for the same handoff loses — the coordination
	// outcome, reported as a conflict rather than a silent double-claim.
	resp := f.do(http.MethodPost, base+"/handoffs/"+handoffA.ID+"/claim", tokenA, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second claim status = %d, want 409", resp.StatusCode)
	}

	// B finishes: writes its result, closes the handoff.
	f.mustDo(http.MethodPut, base+"/artifacts/content?path=notes/decisions.md&content_type=text/markdown&expected_version=1",
		tokenB, []byte("Chose the reindex-in-place approach. Orders reindexed, alias flipped."), http.StatusOK, "B writes its result")
	f.mustDo(http.MethodPost, base+"/handoffs/"+handoffA.ID+"/close", tokenB, nil, http.StatusOK, "B closes the handoff")

	// With everything closed, there is nothing to take over — and the API
	// says so plainly instead of handing back stale work.
	resp = f.do(http.MethodGet, base+"/handoffs/current", tokenB, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("current handoff after close = %d, want 404", resp.StatusCode)
	}

	// --- Revocation takes effect immediately.
	tokenID := f.tokenIDOf(tokenA)
	f.mustDo(http.MethodDelete, "/v1/me/tokens/"+tokenID, tokenA, nil, http.StatusNoContent, "A revokes its own token")

	resp = f.do(http.MethodGet, base+"/context", tokenA, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token status = %d, want 401", resp.StatusCode)
	}

	// B's credential is untouched by A's revocation — two clients, two
	// independent credentials, which is the whole point of not sharing one.
	resp = f.do(http.MethodGet, base+"/context", tokenB, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("B's token after A's revocation = %d, want 200", resp.StatusCode)
	}
}

// TestE2ESkillGetReturnsInstalledContent covers the narrower promise on its
// own: a skill an agent can install is a skill it can load. The pack
// delivers this too, but a client that manages skills without bootstrapping
// must not have to go through the pack to read one.
func TestE2ESkillGetReturnsInstalledContent(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("skills@example.test")
	const project = "skills/proj"
	f.createProject(token, project, "Skills Proj")
	base := "/v1/projects/" + project

	const content = "# Deploy checklist\n1. run migrations\n2. flip the alias\n"
	publish, _ := json.Marshal(map[string]any{"name": "deploy", "author": "ops", "content": content})
	var published struct {
		ID string `json:"id"`
	}
	f.mustDecode(http.MethodPost, base+"/skills", token, publish, http.StatusOK, "publish", &published)

	var got struct {
		SkillName string `json:"skill_name"`
		Content   string `json:"content"`
		Checksum  string `json:"checksum"`
	}
	f.mustDecode(http.MethodGet, base+"/skills/versions/"+published.ID, token, nil, http.StatusOK, "skill.get", &got)
	if got.Content != content {
		t.Fatalf("skill content = %q, want %q", got.Content, content)
	}
	if got.SkillName != "deploy" {
		t.Fatalf("skill name = %q, want deploy", got.SkillName)
	}
}

// mustDo issues a request and fails the test unless it returns wantStatus,
// quoting the body — a scenario test that dies on "status=500" with no
// explanation costs more time than the assertion saves.
func (f *fixture) mustDo(method, path, token string, body []byte, wantStatus int, step string) []byte {
	f.t.Helper()
	resp := f.do(method, path, token, body)
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		f.t.Fatalf("%s: status=%d want=%d body=%s", step, resp.StatusCode, wantStatus, got)
	}
	return got
}

func (f *fixture) mustDecode(method, path, token string, body []byte, wantStatus int, step string, dst any) {
	f.t.Helper()
	raw := f.mustDo(method, path, token, body, wantStatus, step)
	if err := json.Unmarshal(raw, dst); err != nil {
		f.t.Fatalf("%s: decode response %s: %v", step, raw, err)
	}
}

// principalIDOf, projectIDOf and tokenIDOf read back what the API itself
// reports, rather than reaching into the database: the scenario is about
// what a client can do with a token and nothing else.
func (f *fixture) principalIDOf(token string) string {
	f.t.Helper()
	var me struct {
		PrincipalID string `json:"principal_id"`
	}
	f.mustDecode(http.MethodGet, "/v1/me", token, nil, http.StatusOK, "GET /v1/me", &me)
	return me.PrincipalID
}

func (f *fixture) projectIDOf(token, project string) string {
	f.t.Helper()
	var p struct {
		ID string `json:"id"`
	}
	f.mustDecode(http.MethodGet, "/v1/projects/"+project, token, nil, http.StatusOK, "GET project", &p)
	return p.ID
}

// tokenIDOf decodes a bare array, not the {items,next_cursor} envelope:
// a principal's own credential list is bounded by how many tokens that
// principal issued, so it was deliberately left out of the keyset-pagination
// sweep (cookbook §22.1 covers growing lists).
func (f *fixture) tokenIDOf(token string) string {
	f.t.Helper()
	var creds []struct {
		ID string `json:"id"`
	}
	f.mustDecode(http.MethodGet, "/v1/me/tokens", token, nil, http.StatusOK, "GET /v1/me/tokens", &creds)
	if len(creds) != 1 {
		f.t.Fatalf("expected exactly one token for this principal, got %d", len(creds))
	}
	return creds[0].ID
}
