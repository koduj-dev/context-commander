package http_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	cchttp "github.com/koduj-dev/context-commander/adapters/http"
	"github.com/koduj-dev/context-commander/internal/plans"
	"github.com/koduj-dev/context-commander/internal/ratelimit"
)

// stream is an open Server-Sent Events response, read line by line.
type stream struct {
	resp  *http.Response
	lines chan string
}

func (f *fixture) openStream(path, token string) *stream {
	f.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, f.server.URL+path, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed in the Cleanup below, after the test reads it
	if err != nil {
		f.t.Fatalf("open stream: %v", err)
	}
	s := &stream{resp: resp, lines: make(chan string, 64)}
	go func() {
		defer close(s.lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
	}()
	// Closed before the fixture's server, whose Close waits for every
	// request in flight — a stream left open would hold the test forever.
	f.t.Cleanup(func() { _ = resp.Body.Close() })
	return s
}

// expect waits for a line, failing on timeout or on the stream ending.
func (s *stream) expect(t *testing.T, want string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				t.Fatalf("the stream ended before %q", want)
			}
			if line == want {
				return
			}
		case <-deadline:
			t.Fatalf("no %q within 5 s", want)
		}
	}
}

// ends waits for the server to end the stream.
func (s *stream) ends(t *testing.T) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-s.lines:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the stream did not end")
		}
	}
}

func TestBoardStreamTellsAReaderThatAnItemChanged(t *testing.T) {
	var svc *plans.Service
	f := newFixtureWithDeps(t, nil, ratelimit.Limits{}, func(d *cchttp.Deps) { svc = d.Plans })
	owner := f.newUserWithToken("stream-owner@example.test")
	reader := f.newUserWithToken("stream-reader@example.test")
	outsider := f.newUserWithToken("stream-outsider@example.test")
	createBody, _ := json.Marshal(map[string]string{"project": "stream-owner/work", "name": "Work"})
	f.mustDo(http.MethodPost, "/v1/projects", owner, createBody, http.StatusCreated, "create project")
	memberBody, _ := json.Marshal(map[string]string{"principal_id": f.principalIDOf(reader), "role": "READER"})
	f.mustDo(http.MethodPost, "/v1/projects/stream-owner/work/members", owner, memberBody, http.StatusNoContent, "add reader")
	base := "/v1/projects/stream-owner/work"
	var plan struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodPost, base+"/plans/", owner, []byte(`{"title":"Watched","items":[{"text":"a"},{"text":"b"}]}`),
		http.StatusCreated, "create plan"), &plan)

	// Refused the way the board itself is refused: no credential is 401, a
	// non-member is 404 (existence is not leaked), and a session cookie
	// without the CSRF header is 403 — streams get no exemption from it.
	for _, tc := range []struct {
		name  string
		token string
		want  int
	}{
		{"no credential", "", http.StatusUnauthorized},
		{"outsider", outsider, http.StatusNotFound},
	} {
		resp := f.do(http.MethodGet, base+"/plans/events", tc.token, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, resp.StatusCode, tc.want)
		}
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, f.server.URL+base+"/plans/events", nil)
	req.AddCookie(&http.Cookie{Name: "cc_session", Value: reader, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cookie without the CSRF header: status %d, want 403", resp.StatusCode)
	}

	s := f.openStream(base+"/plans/events", reader)
	if s.resp.StatusCode != http.StatusOK || s.resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: status %d, content type %q", s.resp.StatusCode, s.resp.Header.Get("Content-Type"))
	}
	if s.resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Error("the stream does not ask nginx not to buffer it")
	}
	s.expect(t, "event: ready")

	f.mustDo(http.MethodPatch, base+"/plans/items/"+plan.Items[0].ID, owner, []byte(`{"status":"done"}`), http.StatusOK, "tick")
	s.expect(t, "event: changed")
	// A move is a change too, and says nothing about what moved.
	f.mustDo(http.MethodPatch, base+"/plans/items/"+plan.Items[1].ID, owner, []byte(`{"position":1}`), http.StatusOK, "move")
	s.expect(t, "event: changed")
	s.expect(t, "data: {}")

	// Shutdown ends every stream, so the server is not held open by them.
	svc.CloseStreams()
	s.ends(t)
}

func TestStreamsAreCappedPerCredentialAndKeptAlive(t *testing.T) {
	defer cchttp.SetStreamTimings(50*time.Millisecond, 400*time.Millisecond)()
	f := newFixture(t)
	owner := f.newUserWithToken("cap-owner@example.test")
	other := f.newUserWithToken("cap-other@example.test")

	streams := make([]*stream, 0, plans.StreamsPerCredential)
	for range plans.StreamsPerCredential {
		s := f.openStream("/v1/me/presence/events", owner)
		s.expect(t, "event: ready")
		streams = append(streams, s)
	}
	resp := f.do(http.MethodGet, "/v1/me/presence/events", owner, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("stream over the cap: status %d, want 429", resp.StatusCode)
	}
	// The cap is the credential's, not the server's.
	f.openStream("/v1/me/presence/events", other).expect(t, "event: ready")

	streams[0].expect(t, ": ping")
	// A stream ends after its lifetime, so it is authorized again on
	// reconnect; its place under the cap is given back.
	streams[0].ends(t)
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp := f.do(http.MethodGet, "/v1/me/presence/events", owner, nil)
		code := resp.StatusCode
		_ = resp.Body.Close()
		if code == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no place under the cap after streams ended: status %d", code)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestPresenceStreamHearsAReportInTheReadersProjects(t *testing.T) {
	f := newFixture(t)
	owner := f.newUserWithToken("watch-owner@example.test")
	watcher := f.newUserWithToken("watch-watcher@example.test")
	createBody, _ := json.Marshal(map[string]string{"project": "watch-owner/work", "name": "Work"})
	f.mustDo(http.MethodPost, "/v1/projects", owner, createBody, http.StatusCreated, "create project")
	memberBody, _ := json.Marshal(map[string]string{"principal_id": f.principalIDOf(watcher), "role": "READER"})
	f.mustDo(http.MethodPost, "/v1/projects/watch-owner/work/members", owner, memberBody, http.StatusNoContent, "add watcher")

	s := f.openStream("/v1/me/presence/events", watcher)
	s.expect(t, "event: ready")
	f.mustDo(http.MethodPost, "/v1/projects/watch-owner/work/plans/presence", owner, []byte(`{"activity":"reading"}`), http.StatusOK, "report")
	s.expect(t, "event: changed")

	// Nothing in the frame says what changed: the reader re-reads.
	var mine struct {
		Items []struct {
			Activity string `json:"activity"`
		} `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/presence", watcher, nil, http.StatusOK, "re-read"), &mine)
	if len(mine.Items) != 1 || mine.Items[0].Activity != "reading" {
		t.Errorf("re-read %s", fmt.Sprint(mine))
	}
}
