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

	cchttp "github.com/koduj-dev/context-commander/adapters/http"
	"github.com/koduj-dev/context-commander/internal/plans"
	"github.com/koduj-dev/context-commander/internal/ratelimit"
)

// The usage hooks in deploy/mcp-clients run for real against this server,
// on fixture transcripts shaped like the files Claude Code and Codex CLI
// write (testdata/usage-hooks). What they must get right: each response
// counted once, per model, subagents included, and nothing sent twice.

type modelTokens struct {
	Model            string   `json:"model"`
	TokensIn         int64    `json:"tokens_in"`
	TokensOut        int64    `json:"tokens_out"`
	CacheReadTokens  int64    `json:"cache_read_tokens"`
	CacheWriteTokens int64    `json:"cache_write_tokens"`
	EstimatedCost    *float64 `json:"estimated_cost"`
}

type effortJSON struct {
	TokensIn         int64         `json:"tokens_in"`
	TokensOut        int64         `json:"tokens_out"`
	CacheReadTokens  int64         `json:"cache_read_tokens"`
	CacheWriteTokens int64         `json:"cache_write_tokens"`
	Models           []modelTokens `json:"models"`
	CostEstimate     *struct {
		Currency string   `json:"currency"`
		Amount   float64  `json:"amount"`
		Unpriced []string `json:"unpriced_models"`
	} `json:"cost_estimate"`
}

// byModel is the breakdown as model -> [in, out, cache read, cache write].
func (e effortJSON) byModel() map[string][4]int64 {
	out := map[string][4]int64{}
	for _, m := range e.Models {
		out[m.Model] = [4]int64{m.TokensIn, m.TokensOut, m.CacheReadTokens, m.CacheWriteTokens}
	}
	return out
}

type hookBench struct {
	t      *testing.T
	f      *fixture
	python string
	token  string
	base   string
	planID string
	dir    string
}

func newHookBench(t *testing.T, f *fixture, project string) *hookBench {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	b := &hookBench{t: t, f: f, python: python, token: f.newUserWithToken(project + "@example.test"), base: "/v1/projects/" + project + "/work", dir: t.TempDir()}
	createBody, _ := json.Marshal(map[string]string{"project": project + "/work", "name": "Work"})
	f.mustDo(http.MethodPost, "/v1/projects", b.token, createBody, http.StatusCreated, "create project")
	var plan struct {
		ID    string `json:"id"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodPost, b.base+"/plans/", b.token, []byte(`{"title":"P","items":[{"text":"step"}]}`), http.StatusCreated, "plan"), &plan)
	f.mustDo(http.MethodPost, b.base+"/plans/presence", b.token, []byte(fmt.Sprintf(`{"item_id":%q}`, plan.Items[0].ID)), http.StatusOK, "report")
	b.planID = plan.ID
	return b
}

// run runs a hook script with stdin and extra arguments, and fails on any
// output: a hook that has something to say has a problem.
func (b *hookBench) run(script, stdin string, args ...string) {
	b.t.Helper()
	cmd := exec.CommandContext(b.t.Context(), b.python, append([]string{"-I", filepath.Join("..", "..", "deploy", "mcp-clients", script)}, args...)...) //nolint:gosec // python3 from PATH running the repo's own script
	cmd.Env = append(os.Environ(), "CC_TOKEN="+b.token, "CC_URL="+b.f.server.URL, "XDG_STATE_HOME="+b.dir, "CODEX_HOME="+filepath.Join(b.dir, "codex"))
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		b.t.Fatalf("%s: %v: %s", script, err, out)
	}
	if len(out) > 0 {
		b.t.Fatalf("%s said: %s", script, out)
	}
}

func (b *hookBench) effort() effortJSON {
	b.t.Helper()
	var p struct {
		Items []struct {
			Effort effortJSON `json:"effort"`
		} `json:"items"`
	}
	mustJSON(b.t, b.f.mustDo(http.MethodGet, b.base+"/plans/"+b.planID, b.token, nil, http.StatusOK, "read plan"), &p)
	return p.Items[0].Effort
}

// copyTree copies a testdata directory into dst.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		data, err := os.ReadFile(path) //nolint:gosec // the repo's own testdata
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600) //nolint:gosec // G703: a path under t.TempDir() named after the repo's own testdata
	})
	if err != nil {
		t.Fatal(err)
	}
}

func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // a path in t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fh.Close() }()
	for _, l := range lines {
		if _, err := fh.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

func assistantLine(id, model string, in, out, cacheRead, cacheWrite int) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"id": id, "model": model, "usage": map[string]any{
		"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": cacheRead, "cache_creation_input_tokens": cacheWrite,
	}}})
	return string(b)
}

// Claude Code: a response streamed as several copies counts once; a
// subagent's transcript under <session>/subagents counts, and so does an
// old-style sidechain in the session transcript, once even when it is in
// both; a "<synthetic>" placeholder with no usage adds nothing; and the
// next turn sends only what is new.
func TestTheClaudeCodeUsageHookSendsEachResponseOncePerModel(t *testing.T) {
	prices, err := plans.ParsePrices([]byte(`{"currency":"USD","models":{"claude-opus-5-5":{"input":4,"output":20,"cache_read":0.2,"cache_write":5}}}`))
	if err != nil {
		t.Fatal(err)
	}
	f := newFixtureWithDeps(t, nil, ratelimit.Limits{}, func(d *cchttp.Deps) { d.Plans.SetPrices(prices) })
	b := newHookBench(t, f, "hook")
	copyTree(t, filepath.Join("testdata", "usage-hooks", "claude-code"), b.dir)
	transcript := filepath.Join(b.dir, "session.jsonl")
	stdin := fmt.Sprintf(`{"session_id":"s-1","transcript_path":%q}`, transcript)

	b.run("claude-code-usage-hook.py", stdin)
	e := b.effort()
	want := map[string][4]int64{
		"claude-opus-5-5":   {3, 100, 5000, 700},
		"claude-haiku-5-5":  {2, 10, 0, 0},
		"claude-sonnet-5-5": {5, 50, 1000, 200},
	}
	if got := e.byModel(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("after the first turn: %v, want %v", got, want)
	}
	if e.TokensIn != 10 || e.TokensOut != 160 || e.CacheReadTokens != 6000 || e.CacheWriteTokens != 900 {
		t.Errorf("totals after the first turn: %+v", e)
	}
	// Priced where the table has the model, "no price" where it has not.
	if e.CostEstimate == nil || e.CostEstimate.Currency != "USD" || fmt.Sprint(e.CostEstimate.Unpriced) != "[claude-haiku-5-5 claude-sonnet-5-5]" {
		t.Errorf("estimate %+v", e.CostEstimate)
	}
	for _, m := range e.Models {
		if (m.EstimatedCost != nil) != (m.Model == "claude-opus-5-5") {
			t.Errorf("model %q estimated cost %v", m.Model, m.EstimatedCost)
		}
	}

	b.run("claude-code-usage-hook.py", stdin) // nothing new: nothing sent
	appendLines(t, transcript, assistantLine("msg_2", "claude-opus-5-5", 1, 50, 6000, 0))
	appendLines(t, filepath.Join(b.dir, "session", "subagents", "agent-a1.jsonl"), assistantLine("msg_a2", "claude-sonnet-5-5", 1, 1, 1, 1))
	b.run("claude-code-usage-hook.py", stdin)
	want["claude-opus-5-5"] = [4]int64{4, 150, 11000, 700}
	want["claude-sonnet-5-5"] = [4]int64{6, 51, 1001, 201}
	if got := b.effort().byModel(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("after the second turn: %v, want %v", got, want)
	}
}

// A state file written before the hook sent models holds one flat total of
// the session transcript. Those tokens were sent; upgrading the hook must
// not send them again, and the tokens it never sent must go out under their
// own model — not under whichever model sorts first.
func TestTheClaudeCodeUsageHookDoesNotResendAfterAnUpgrade(t *testing.T) {
	f := newFixture(t)
	b := newHookBench(t, f, "upgrade")
	transcript := filepath.Join(b.dir, "old.jsonl")
	if err := os.WriteFile(transcript, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdin := fmt.Sprintf(`{"session_id":"s-old","transcript_path":%q}`, transcript)
	// The old hook sent exactly msg_1. Since then: nothing, at first.
	appendLines(t, transcript, assistantLine("msg_1", "claude-opus-5-5", 100, 0, 0, 0))
	state := filepath.Join(b.dir, "context-commander", "usage", "s-old.json")
	if err := os.MkdirAll(filepath.Dir(state), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, []byte(`{"tokens_in":100,"tokens_out":0,"cache_read_tokens":0,"cache_write_tokens":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b.run("claude-code-usage-hook.py", stdin)
	if got := b.effort().byModel(); len(got) != 0 {
		t.Fatalf("the first run after the upgrade sent %v, want nothing", got)
	}
	// The conversion was saved although nothing was posted.
	saved, err := os.ReadFile(state) //nolint:gosec // a path in t.TempDir()
	if err != nil || !strings.Contains(string(saved), `"models"`) {
		t.Fatalf("state after a run with nothing to send: %s %v", saved, err)
	}

	// A later Haiku response sorts before Opus; it must still be sent as Haiku.
	appendLines(t, transcript, assistantLine("msg_2", "claude-haiku-5-5", 25, 0, 0, 0))
	b.run("claude-code-usage-hook.py", stdin)
	if got := b.effort().byModel(); fmt.Sprint(got) != fmt.Sprint(map[string][4]int64{"claude-haiku-5-5": {25, 0, 0, 0}}) {
		t.Errorf("after the upgrade: %v, want only msg_2, as Haiku", got)
	}
}

// Codex CLI: the running total in token_count events is credited by its
// growth to the model of the turn it grew in; a repeated or empty event adds
// nothing; cached input is taken out of input and sent as cache reads. It
// works as the notify program (event as the last argument, rollout found
// under CODEX_HOME by thread id) and as a Stop hook (event on stdin).
func TestTheCodexUsageHookSendsGrowthPerModel(t *testing.T) {
	f := newFixture(t)
	b := newHookBench(t, f, "codex")
	const thread = "01a09bac-721f-7f23-a9a3-a78fcb71388a"
	day := filepath.Join(b.dir, "codex", "sessions", "2026", "09", "13")
	if err := os.MkdirAll(day, 0o750); err != nil {
		t.Fatal(err)
	}
	copyTree(t, filepath.Join("testdata", "usage-hooks", "codex"), day)
	rollout := filepath.Join(day, "rollout-2026-09-13T18-49-25-"+thread+".jsonl")

	b.run("codex-usage-hook.py", "", fmt.Sprintf(`{"type":"agent-turn-complete","thread-id":%q,"turn-id":"t-2"}`, thread))
	want := map[string][4]int64{
		"gpt-6-astra": {33923 - 27136, 180, 27136, 0},
		"gpt-6-mini":  {6077 - 2864, 20, 2864, 0},
	}
	if got := b.effort().byModel(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("after notify: %v, want %v", got, want)
	}

	// A notify event that is not a finished turn is not a reason to send.
	b.run("codex-usage-hook.py", "", fmt.Sprintf(`{"type":"approval-requested","thread-id":%q}`, thread))

	appendLines(t, rollout,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":41000,"cached_input_tokens":30500,"cache_write_input_tokens":0,"output_tokens":260,"reasoning_output_tokens":14,"total_tokens":41260}}}}`)
	b.run("codex-usage-hook.py", fmt.Sprintf(`{"session_id":%q,"transcript_path":%q,"hook_event_name":"Stop","model":"gpt-6-mini","turn_id":"t-2"}`, thread, rollout))
	want["gpt-6-mini"] = [4]int64{6077 - 2864 + 500, 80, 2864 + 500, 0}
	if got := b.effort().byModel(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("after the Stop hook: %v, want %v", got, want)
	}
	b.run("codex-usage-hook.py", fmt.Sprintf(`{"session_id":%q,"transcript_path":%q}`, thread, rollout)) // nothing new
	if got := b.effort().byModel(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("a run with nothing new sent something: %v", got)
	}
}

// The hook reads the branch from the session's working directory and sends
// it with the tokens, and CC_PROJECT places a client that never reported a
// step, so its turn is counted rather than dropped (docs/adr/0027).
func TestTheUsageHookSendsTheBranchAndCountsAnUnplacedClient(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	f := newFixture(t)
	token := f.newUserWithToken("ledger@example.test")
	createBody, _ := json.Marshal(map[string]string{"project": "ledger/work", "name": "Work"})
	f.mustDo(http.MethodPost, "/v1/projects", token, createBody, http.StatusCreated, "create project")

	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	for _, args := range [][]string{{"init", "-q", "-b", "feature/ledger", repo}} {
		if out, err := exec.CommandContext(t.Context(), git, args...).CombinedOutput(); err != nil { //nolint:gosec // git from PATH on a temp dir
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	copyTree(t, filepath.Join("testdata", "usage-hooks", "claude-code"), dir)
	stdin := fmt.Sprintf(`{"session_id":"s-ledger","transcript_path":%q,"cwd":%q}`, filepath.Join(dir, "session.jsonl"), repo)

	cmd := exec.CommandContext(t.Context(), python, "-I", filepath.Join("..", "..", "deploy", "mcp-clients", "claude-code-usage-hook.py")) //nolint:gosec // the repo's own script
	cmd.Env = append(os.Environ(), "CC_TOKEN="+token, "CC_URL="+f.server.URL, "XDG_STATE_HOME="+dir, "CC_PROJECT=ledger/work")
	cmd.Stdin = strings.NewReader(stdin)
	if out, err := cmd.CombinedOutput(); err != nil || len(out) > 0 {
		t.Fatalf("hook: %v %s", err, out)
	}

	var usage struct {
		Rows []struct {
			Branch    *string `json:"branch"`
			TokensOut int64   `json:"tokens_out"`
			Turns     int64   `json:"turns"`
		} `json:"rows"`
		Total struct {
			TokensOut int64 `json:"tokens_out"`
		} `json:"total"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/projects/ledger/work/usage?group=branch", token, nil, http.StatusOK, "project usage"), &usage)
	if len(usage.Rows) != 1 || usage.Rows[0].Branch == nil || *usage.Rows[0].Branch != "feature/ledger" || usage.Rows[0].TokensOut == 0 {
		t.Fatalf("project usage = %+v, want one feature/ledger row with tokens", usage)
	}
	var mine struct {
		Rows []struct {
			ProjectPath string `json:"project_path"`
		} `json:"rows"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/usage/summary?group=project", token, nil, http.StatusOK, "my usage"), &mine)
	if len(mine.Rows) != 1 || mine.Rows[0].ProjectPath != "ledger/work" {
		t.Fatalf("my usage = %+v", mine)
	}
	f.wantStatus(http.MethodGet, "/v1/me/usage/summary?group=nope", token, nil, http.StatusBadRequest, "unknown group")
	f.wantStatus(http.MethodGet, "/v1/me/usage/summary?from=yesterday", token, nil, http.StatusBadRequest, "bad day")
}
