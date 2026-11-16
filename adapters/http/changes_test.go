package http_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type changesJSON struct {
	Now   string `json:"now"`
	Items []struct {
		Kind   string `json:"kind"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Where  string `json:"where"`
		ByName string `json:"by_name"`
	} `json:"items"`
}

// What a colleague did reaches the other agent: a memory, a step blocked
// and then finished, a handoff; the agent's own changes and anything before
// its starting point are left out, and outsiders get a 404.
func TestProjectChangesOverHTTP(t *testing.T) {
	f := newFixture(t)
	alice := f.newUserWithToken("ch-alice@example.test")
	bob := f.newUserWithToken("ch-bob@example.test")
	outsider := f.newUserWithToken("ch-outsider@example.test")
	const project = "ch-alice/work"
	f.createProject(alice, project, "Work")
	base := "/v1/projects/" + project
	add, _ := json.Marshal(map[string]string{"principal_id": f.principalIDOf(bob), "role": "CONTRIBUTOR"})
	f.mustDo(http.MethodPost, base+"/members", alice, add, http.StatusNoContent, "add bob")

	// Before bob's starting point: not reported.
	f.mustDo(http.MethodPost, base+"/memories/", alice, []byte(`{"title":"Old news"}`), http.StatusCreated, "old memory")

	var start changesJSON
	mustJSON(t, f.mustDo(http.MethodGet, base+"/changes", bob, nil, http.StatusOK, "starting point"), &start)
	if start.Now == "" || len(start.Items) != 0 {
		t.Fatalf("starting point %+v", start)
	}

	f.mustDo(http.MethodPost, base+"/memories/", alice, []byte(`{"kind":"gotcha","title":"Staging Ollama needs the VPN"}`), http.StatusCreated, "gotcha")
	f.mustDo(http.MethodPost, base+"/memories/", bob, []byte(`{"title":"Bob's own note"}`), http.StatusCreated, "bob's note")
	var plan struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodPost, base+"/plans/", alice, []byte(`{"title":"Search","items":[{"text":"Wire the index"}]}`), http.StatusCreated, "plan"), &plan)
	f.mustDo(http.MethodPatch, base+"/plans/items/"+plan.Items[0].ID, alice, []byte(`{"status":"blocked"}`), http.StatusOK, "block")
	f.mustDo(http.MethodPost, base+"/handoffs", alice, []byte(`{"summary":"over to you"}`), http.StatusOK, "handoff")

	var got changesJSON
	mustJSON(t, f.mustDo(http.MethodGet, base+"/changes?since="+url.QueryEscape(start.Now), bob, nil, http.StatusOK, "changes"), &got)
	kinds := map[string]string{}
	for _, c := range got.Items {
		kinds[c.Kind+":"+c.Detail] = c.Title
		if c.ByName == "" {
			t.Errorf("no author on %+v", c)
		}
		if c.Title == "Old news" || c.Title == "Bob's own note" {
			t.Errorf("reported %q", c.Title)
		}
	}
	if kinds["memory:gotcha"] != "Staging Ollama needs the VPN" || kinds["item:blocked"] != "Wire the index" {
		t.Errorf("changes %+v", got.Items)
	}
	if _, ok := kinds["handoff:open"]; !ok {
		t.Errorf("no handoff in %+v", got.Items)
	}

	// Picking up from the last now: only what came after.
	f.mustDo(http.MethodPatch, base+"/plans/items/"+plan.Items[0].ID, alice, []byte(`{"status":"done"}`), http.StatusOK, "finish")
	var next changesJSON
	mustJSON(t, f.mustDo(http.MethodGet, base+"/changes?since="+url.QueryEscape(got.Now), bob, nil, http.StatusOK, "next"), &next)
	if len(next.Items) != 1 || next.Items[0].Detail != "done" || next.Items[0].Where != "Search" {
		t.Errorf("next %+v", next.Items)
	}

	f.mustDo(http.MethodGet, base+"/changes", outsider, nil, http.StatusNotFound, "outsider")
	f.mustDo(http.MethodGet, base+"/changes?since=yesterday", bob, nil, http.StatusBadRequest, "bad since")
}

// The Claude Code inbox hook reports what others changed: nothing on a
// session's first poll (that is its starting point), a colleague's gotcha on
// the next, and the same gotcha never twice.
func TestTheInboxHookReportsChanges(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	f := newFixture(t)
	alice := f.newUserWithToken("hk-alice@example.test")
	bob := f.newUserWithToken("hk-bob@example.test")
	const project = "hk-alice/work"
	f.createProject(alice, project, "Work")
	base := "/v1/projects/" + project
	add, _ := json.Marshal(map[string]string{"principal_id": f.principalIDOf(bob), "role": "CONTRIBUTOR"})
	f.mustDo(http.MethodPost, base+"/members", alice, add, http.StatusNoContent, "add bob")
	state := t.TempDir()

	run := func() string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), python, "-I", filepath.Join("..", "..", "deploy", "mcp-clients", "claude-code-inbox-hook.py")) //nolint:gosec // python3 from PATH running the repo's own script
		cmd.Env = append(os.Environ(), "CC_TOKEN="+bob, "CC_URL="+f.server.URL, "XDG_STATE_HOME="+state,
			"CC_INBOX_INTERVAL=0", "CC_INBOX_TIMEOUT=2", "CC_PROJECT="+project)
		cmd.Stdin = strings.NewReader(`{"session_id":"s1","hook_event_name":"PostToolUse","tool_name":"Bash"}`)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("hook: %v: %s", err, out)
		}
		return string(out)
	}

	f.mustDo(http.MethodPost, base+"/memories/", alice, []byte(`{"title":"Before the session"}`), http.StatusCreated, "old")
	if out := run(); out != "" {
		t.Fatalf("first poll printed %q", out)
	}
	f.mustDo(http.MethodPost, base+"/memories/", alice, []byte(`{"kind":"gotcha","title":"Staging Ollama needs the VPN"}`), http.StatusCreated, "gotcha")
	out := run()
	var printed struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &printed); err != nil {
		t.Fatalf("hook output %q: %v", out, err)
	}
	text := printed.HookSpecificOutput.AdditionalContext
	if !strings.Contains(text, `recorded a gotcha: "Staging Ollama needs the VPN"`) || strings.Contains(text, "Before the session") {
		t.Errorf("report %q", text)
	}
	if out := run(); out != "" {
		t.Errorf("the same change was reported again: %q", out)
	}
}
