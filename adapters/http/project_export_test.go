package http_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// readArchive unpacks a downloaded export into name → content.
func readArchive(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	files := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		b, _ := io.ReadAll(tr)
		files[h.Name] = b
	}
}

// The export carries every kind of content in plaintext, every artifact
// version, the people it names, and a complete manifest; a CONTRIBUTOR may
// not export and an outsider is told the project does not exist.
func TestProjectExportOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner := f.newUserWithToken("ex-owner@example.test")
	contributor := f.newUserWithToken("ex-contrib@example.test")
	outsider := f.newUserWithToken("ex-outsider@example.test")
	const project = "ex-owner/work"
	f.createProject(owner, project, "Work")
	base := "/v1/projects/" + project
	add, _ := json.Marshal(map[string]string{"principal_id": f.principalIDOf(contributor), "role": "CONTRIBUTOR"})
	f.mustDo(http.MethodPost, base+"/members", owner, add, http.StatusNoContent, "add contributor")

	f.mustDo(http.MethodPut, base+"/artifacts/content?path=docs/a.md&content_type=text/markdown&expected_version=0", owner, []byte("first"), http.StatusOK, "v1")
	f.mustDo(http.MethodPut, base+"/artifacts/content?path=docs/a.md&content_type=text/markdown&expected_version=1", contributor, []byte("second"), http.StatusOK, "v2")
	f.mustDo(http.MethodPost, base+"/memories/", owner, []byte(`{"kind":"decision","title":"Vectors stay encrypted","body":"because"}`), http.StatusCreated, "memory")
	f.mustDo(http.MethodPost, base+"/plans/", owner, []byte(`{"title":"Ship","items":[{"text":"step one"}]}`), http.StatusCreated, "plan")
	f.mustDo(http.MethodPost, base+"/handoffs", owner, []byte(`{"summary":"over to you"}`), http.StatusOK, "handoff")

	var conv, session, raw struct{ ID string }
	mustJSON(t, f.mustDo(http.MethodPost, base+"/conversations", owner, nil, http.StatusOK, "conversation"), &conv)
	mustJSON(t, f.mustDo(http.MethodPost, base+"/conversations/"+conv.ID+"/sessions", owner,
		[]byte(`{"provider":"anthropic","client":"claude-code","provider_session_id":"sess-1","relation":"started"}`), http.StatusOK, "session"), &session)
	mustJSON(t, f.mustDo(http.MethodPost, base+"/conversations/"+conv.ID+"/objects", owner,
		[]byte(`{"external_session_id":"`+session.ID+`","type":"raw","content":"hello world"}`), http.StatusOK, "raw"), &raw)
	f.mustDo(http.MethodPost, base+"/conversations/"+conv.ID+"/objects", owner,
		[]byte(`{"external_session_id":"`+session.ID+`","type":"compacted","derived_from":"`+raw.ID+`","content":"hello"}`), http.StatusOK, "compacted")

	f.mustDo(http.MethodGet, base+"/export", contributor, nil, http.StatusForbidden, "contributor")
	f.mustDo(http.MethodGet, base+"/export", outsider, nil, http.StatusNotFound, "outsider")

	resp := f.do(http.MethodGet, base+"/export", owner, nil)
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/gzip" ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), ".ccproject.tar.gz") {
		t.Fatalf("export: %d %v %s", resp.StatusCode, resp.Header, data)
	}
	files := readArchive(t, data)

	var m struct {
		Format   string `json:"format"`
		Complete bool   `json:"complete"`
		Project  struct{ Path string }
		Counts   map[string]int `json:"counts"`
		Excluded []string       `json:"excluded"`
	}
	mustJSON(t, files["manifest.json"], &m)
	if m.Format != "context-commander/project" || !m.Complete || m.Project.Path != project ||
		m.Counts["artifacts"] != 1 || m.Counts["memories"] != 1 || m.Counts["plans"] != 1 || m.Counts["handoffs"] != 1 ||
		!strings.Contains(strings.Join(m.Excluded, ","), "secrets") {
		t.Fatalf("manifest %+v", m)
	}

	var arts []struct {
		Path     string `json:"path"`
		Versions []struct {
			Version   int    `json:"version"`
			Blob      string `json:"blob"`
			CreatedBy string `json:"created_by"`
		} `json:"versions"`
	}
	mustJSON(t, files["artifacts.json"], &arts)
	if len(arts) != 1 || len(arts[0].Versions) != 2 || arts[0].Versions[0].Version != 1 ||
		string(files[arts[0].Versions[0].Blob]) != "first" || string(files[arts[0].Versions[1].Blob]) != "second" {
		t.Fatalf("artifacts %+v", arts)
	}
	var mems []struct{ Title, Body string }
	mustJSON(t, files["memories.json"], &mems)
	if len(mems) != 1 || mems[0].Body != "because" {
		t.Errorf("memories %+v", mems)
	}
	var people []struct{ ID, Email string }
	mustJSON(t, files["people.json"], &people)
	emails := map[string]string{}
	for _, p := range people {
		emails[p.ID] = p.Email
	}
	if emails[arts[0].Versions[1].CreatedBy] != "ex-contrib@example.test" {
		t.Errorf("people %+v", people)
	}
	if strings.Contains(string(data), "over to you") {
		t.Log("the archive is compressed; plaintext is inside, not on the wire as-is")
	}
	var hist struct {
		Conversations []struct {
			Sessions []struct {
				ProviderSessionID string `json:"provider_session_id"`
			} `json:"sessions"`
			Objects []struct {
				Type, Blob  string
				DerivedFrom string `json:"derived_from"`
			} `json:"objects"`
		} `json:"conversations"`
	}
	mustJSON(t, files["history.json"], &hist)
	if len(hist.Conversations) != 1 || len(hist.Conversations[0].Sessions) != 1 || len(hist.Conversations[0].Objects) != 2 {
		t.Fatalf("history %+v", hist)
	}
	for _, o := range hist.Conversations[0].Objects {
		if o.Type == "compacted" && (o.DerivedFrom == "" || string(files[o.Blob]) != "hello") {
			t.Errorf("compacted object %+v", o)
		}
	}
	var hand []struct{ Summary string }
	mustJSON(t, files["handoffs.json"], &hand)
	if len(hand) != 1 || hand[0].Summary != "over to you" {
		t.Errorf("handoffs %+v", hand)
	}
}
