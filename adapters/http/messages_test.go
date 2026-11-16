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

type messageJSON struct {
	ID          string `json:"id"`
	Project     string `json:"project"`
	From        string `json:"from"`
	FromName    string `json:"from_name"`
	Audience    string `json:"audience"`
	ToClient    string `json:"to_client"`
	Body        string `json:"body"`
	Redelivered bool   `json:"redelivered"`
	Deliveries  []struct {
		ClientID string `json:"client_id"`
		AckedAt  string `json:"acked_at"`
	} `json:"deliveries"`
}

type messagesBench struct {
	f       *fixture
	owner   string // the person's PAT, used by their agent
	agentID string // that PAT's client id, as presence shows it
	cookie  *http.Cookie
	base    string
}

func newMessagesBench(t *testing.T, f *fixture, user string) *messagesBench {
	t.Helper()
	b := &messagesBench{f: f, owner: f.newUserWithToken(user + "@example.test"), base: "/v1/projects/" + user + "/work"}
	body, _ := json.Marshal(map[string]string{"project": user + "/work", "name": "Work"})
	f.mustDo(http.MethodPost, "/v1/projects", b.owner, body, http.StatusCreated, "create project")
	b.agentID = f.tokenIDOf(b.owner)
	signIn := f.post("/v1/auth/token", fmt.Sprintf(`{"token":%q}`, b.owner))
	if b.cookie = signIn.sessionCookie(); b.cookie == nil {
		t.Fatalf("sign in: %d %s", signIn.status, signIn.body)
	}
	return b
}

// sendAsPerson sends from the browser session.
func (b *messagesBench) sendAsPerson(body string) reply {
	req := b.f.request(http.MethodPost, b.base+"/messages", body)
	req.AddCookie(b.cookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "cc-web")
	return b.f.send(req)
}

func (b *messagesBench) sent() []messageJSON {
	req := b.f.request(http.MethodGet, b.base+"/messages", "")
	req.AddCookie(b.cookie)
	req.Header.Set("X-Requested-With", "cc-web")
	r := b.f.send(req)
	if r.status != http.StatusOK {
		b.f.t.Fatalf("sent list: %d %s", r.status, r.body)
	}
	var out struct {
		Items []messageJSON `json:"items"`
	}
	mustJSON(b.f.t, []byte(r.body), &out)
	return out.Items
}

// The loop over HTTP: a person in the interface sends to their agent's
// session, the agent's token cannot send, the agent reads its inbox, acks,
// and the person sees the ack.
func TestMessagesOverHTTP(t *testing.T) {
	f := newFixture(t)
	b := newMessagesBench(t, f, "msg-owner")
	outsider := f.newUserWithToken("msg-outsider@example.test")

	var refusal struct {
		Error struct{ Code string } `json:"error"`
	}
	mustJSON(t, f.mustDo(http.MethodPost, b.base+"/messages", b.owner, []byte(`{"body":"hi"}`), http.StatusForbidden, "send with a PAT"), &refusal)
	if refusal.Error.Code != "needs_person" {
		t.Errorf("send with a PAT: code %q, want needs_person", refusal.Error.Code)
	}
	f.mustDo(http.MethodPost, b.base+"/messages", outsider, []byte(`{"body":"hi"}`), http.StatusNotFound, "send as an outsider")
	if r := b.sendAsPerson(`{"body":""}`); r.status != http.StatusBadRequest {
		t.Errorf("empty body: %d %s", r.status, r.body)
	}
	if r := b.sendAsPerson(fmt.Sprintf(`{"body":%q}`, strings.Repeat("x", 8*1024+1))); r.status != http.StatusBadRequest {
		t.Errorf("a body over 8 KiB: %d", r.status)
	}

	r := b.sendAsPerson(fmt.Sprintf(`{"body":"Rebase on main first.","to_client":%q,"expires_in_minutes":60}`, b.agentID))
	if r.status != http.StatusCreated {
		t.Fatalf("send: %d %s", r.status, r.body)
	}
	var sent messageJSON
	mustJSON(t, []byte(r.body), &sent)
	if sent.Audience != "session" || sent.ToClient != b.agentID {
		t.Errorf("sent %+v", sent)
	}

	var inbox struct {
		Items []messageJSON `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/inbox", outsider, nil, http.StatusOK, "outsider's inbox"), &inbox)
	if len(inbox.Items) != 0 {
		t.Errorf("an outsider's inbox: %+v", inbox.Items)
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/inbox?project=msg-owner/work", b.owner, nil, http.StatusOK, "agent's inbox"), &inbox)
	if len(inbox.Items) != 1 || inbox.Items[0].Body != "Rebase on main first." || inbox.Items[0].Project != "msg-owner/work" ||
		inbox.Items[0].FromName == "" || inbox.Items[0].Redelivered {
		t.Fatalf("inbox %+v", inbox.Items)
	}
	if got := b.sent(); len(got[0].Deliveries) != 1 || got[0].Deliveries[0].AckedAt != "" {
		t.Errorf("after delivery the sender sees %+v", got[0].Deliveries)
	}
	f.mustDo(http.MethodPost, "/v1/me/inbox/ack", b.owner, []byte(fmt.Sprintf(`{"message_ids":[%q]}`, sent.ID)), http.StatusOK, "ack")
	if got := b.sent(); got[0].Deliveries[0].AckedAt == "" {
		t.Errorf("after the ack the sender sees %+v", got[0].Deliveries)
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/inbox", b.owner, nil, http.StatusOK, "inbox after ack"), &inbox)
	if len(inbox.Items) != 0 {
		t.Errorf("after ack: %+v", inbox.Items)
	}
	f.mustDo(http.MethodGet, b.base+"/messages", outsider, nil, http.StatusNotFound, "outsider lists sent")
	f.mustDo(http.MethodGet, "/v1/me/inbox?cursor=nonsense", b.owner, nil, http.StatusBadRequest, "bad cursor")
}

// The Claude Code inbox hook runs for real: it prints what is waiting as
// additionalContext, acknowledges it, stays quiet when there is nothing,
// keeps to its interval, and says nothing at all when the server is down.
func TestTheClaudeCodeInboxHook(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	f := newFixture(t)
	b := newMessagesBench(t, f, "msg-hook")
	state := t.TempDir()

	run := func(event, url, interval string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), python, "-I", filepath.Join("..", "..", "deploy", "mcp-clients", "claude-code-inbox-hook.py")) //nolint:gosec // python3 from PATH running the repo's own script
		cmd.Env = append(os.Environ(), "CC_TOKEN="+b.owner, "CC_URL="+url, "XDG_STATE_HOME="+state, "CC_INBOX_INTERVAL="+interval, "CC_INBOX_TIMEOUT=2")
		cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"session_id":"s1","hook_event_name":%q,"tool_name":"Bash"}`, event))
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("hook: %v: %s", err, out)
		}
		return string(out)
	}

	if out := run("PostToolUse", f.server.URL, "0"); out != "" {
		t.Errorf("an empty inbox printed %q", out)
	}
	if r := b.sendAsPerson(`{"body":"Don't touch the billing module today."}`); r.status != http.StatusCreated {
		t.Fatalf("send: %d %s", r.status, r.body)
	}
	out := run("PostToolUse", f.server.URL, "0")
	var printed struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &printed); err != nil {
		t.Fatalf("hook output %q: %v", out, err)
	}
	if printed.HookSpecificOutput.HookEventName != "PostToolUse" ||
		!strings.Contains(printed.HookSpecificOutput.AdditionalContext, "Don't touch the billing module today.") ||
		!strings.Contains(printed.HookSpecificOutput.AdditionalContext, "msg-hook/work") {
		t.Errorf("hook printed %+v", printed)
	}
	if got := b.sent(); len(got) != 1 || len(got[0].Deliveries) != 1 || got[0].Deliveries[0].AckedAt == "" {
		t.Errorf("the hook did not acknowledge: %+v", got)
	}
	if out := run("PostToolUse", f.server.URL, "0"); out != "" {
		t.Errorf("an acknowledged message was printed again: %q", out)
	}

	// Within the interval a tool call does not poll, and a prompt does.
	if r := b.sendAsPerson(`{"body":"second"}`); r.status != http.StatusCreated {
		t.Fatalf("send: %d", r.status)
	}
	if out := run("PostToolUse", f.server.URL, "3600"); out != "" {
		t.Errorf("polled within the interval: %q", out)
	}
	if out := run("UserPromptSubmit", f.server.URL, "3600"); !strings.Contains(out, "second") || !strings.Contains(out, `"UserPromptSubmit"`) {
		t.Errorf("a prompt did not poll: %q", out)
	}

	// Claude Code caps additionalContext at 10,000 characters and shows only a
	// preview past it. Three messages near the 8 KiB limit do not fit in one
	// poll: the hook prints one whole, acknowledges only that one, and the
	// next poll delivers the next.
	long := strings.Repeat("a", 7000)
	for range 3 {
		if r := b.sendAsPerson(fmt.Sprintf(`{"body":%q}`, long)); r.status != http.StatusCreated {
			t.Fatalf("send long: %d %s", r.status, r.body)
		}
	}
	for poll := 1; poll <= 3; poll++ {
		out := run("PostToolUse", f.server.URL, "0")
		var got struct {
			HookSpecificOutput struct {
				AdditionalContext string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("poll %d output %q: %v", poll, out, err)
		}
		text := got.HookSpecificOutput.AdditionalContext
		if len(text) > 10000 || strings.Count(text, long) != 1 {
			t.Fatalf("poll %d printed %d characters holding %d long messages", poll, len(text), strings.Count(text, long))
		}
	}
	if out := run("PostToolUse", f.server.URL, "0"); out != "" {
		t.Errorf("a fourth poll printed %q", out)
	}

	// A server that is not there: silence, exit 0.
	if out := run("UserPromptSubmit", "http://127.0.0.1:1", "0"); out != "" {
		t.Errorf("a down server printed %q", out)
	}
}
