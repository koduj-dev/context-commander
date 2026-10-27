package http_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The Claude Code usage hook (deploy/mcp-clients/claude-code-usage-hook.py),
// run for real against this server: a transcript that repeats each streamed
// response, as Claude Code's does, must be counted once per response, and
// running the hook again after a turn must send only what is new.
func TestTheClaudeCodeUsageHookSendsEachResponseOnce(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	f := newFixture(t)
	token := f.newUserWithToken("hook@example.test")
	createBody, _ := json.Marshal(map[string]string{"project": "hook/work", "name": "Work"})
	f.mustDo(http.MethodPost, "/v1/projects", token, createBody, http.StatusCreated, "create project")
	base := "/v1/projects/hook/work"
	var plan struct {
		ID    string `json:"id"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodPost, base+"/plans/", token, []byte(`{"title":"P","items":[{"text":"step"}]}`), http.StatusCreated, "plan"), &plan)
	f.mustDo(http.MethodPost, base+"/plans/presence", token, []byte(fmt.Sprintf(`{"item_id":%q}`, plan.Items[0].ID)), http.StatusOK, "report")

	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	line := func(id string, in, out, cacheRead, cacheWrite int) string {
		b, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"id": id, "usage": map[string]any{
			"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": cacheRead, "cache_creation_input_tokens": cacheWrite,
		}}})
		return string(b) + "\n"
	}
	write := func(lines ...string) {
		t.Helper()
		fh, err := os.OpenFile(transcript, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // a path in t.TempDir()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = fh.Close() }()
		for _, l := range lines {
			if _, err := fh.WriteString(l); err != nil {
				t.Fatal(err)
			}
		}
	}
	run := func() {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), python, filepath.Join("..", "..", "deploy", "mcp-clients", "claude-code-usage-hook.py")) //nolint:gosec // python3 from PATH running the repo's own script
		cmd.Env = append(os.Environ(), "CC_TOKEN="+token, "CC_URL="+f.server.URL, "XDG_STATE_HOME="+dir)
		cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"session_id":"s-1","transcript_path":%q}`, transcript))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("hook: %v: %s", err, out)
		}
		if len(out) > 0 {
			t.Fatalf("hook said: %s", out)
		}
	}
	effort := func() map[string]int64 {
		t.Helper()
		var p struct {
			Items []struct {
				Effort map[string]int64 `json:"effort"`
			} `json:"items"`
		}
		mustJSON(t, f.mustDo(http.MethodGet, base+"/plans/"+plan.ID, token, nil, http.StatusOK, "read plan"), &p)
		return p.Items[0].Effort
	}

	// One response streamed as three copies, plus a user line and junk.
	write(line("msg_1", 3, 100, 5000, 700), line("msg_1", 3, 100, 5000, 700), line("msg_1", 3, 100, 5000, 700),
		`{"type":"user","message":{"content":"hi"}}`+"\n", "not json\n")
	run()
	if e := effort(); e["tokens_in"] != 3 || e["tokens_out"] != 100 || e["cache_read_tokens"] != 5000 || e["cache_write_tokens"] != 700 {
		t.Fatalf("after the first turn: %v", e)
	}

	run() // nothing new: nothing sent
	write(line("msg_2", 1, 50, 6000, 0))
	run()
	if e := effort(); e["tokens_in"] != 4 || e["tokens_out"] != 150 || e["cache_read_tokens"] != 11000 || e["cache_write_tokens"] != 700 {
		t.Errorf("after the second turn: %v", e)
	}
}
