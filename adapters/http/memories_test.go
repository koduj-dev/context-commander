package http_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

type memoryBody struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	Version      int      `json:"version"`
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Branch       string   `json:"branch"`
	TicketKey    string   `json:"ticket_key"`
	TicketURL    string   `json:"ticket_url"`
	Tags         []string `json:"tags"`
	Assignee     string   `json:"assignee"`
	SupersededBy string   `json:"superseded_by"`
	CreatedBy    string   `json:"created_by"`
	Project      string   `json:"project"`
	Match        string   `json:"match"`
	Snippet      string   `json:"snippet"`
}

func TestMemoriesOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner := f.newUserWithToken("mem-owner@example.test")
	reader := f.newUserWithToken("mem-reader@example.test")
	f.createProject(owner, "mem-owner/work", "Work")
	memberBody, _ := json.Marshal(map[string]string{"principal_id": f.principalIDOf(reader), "role": "READER"})
	f.mustDo(http.MethodPost, "/v1/projects/mem-owner/work/members", owner, memberBody, http.StatusNoContent, "add reader")
	base := "/v1/projects/mem-owner/work/memories"

	var created memoryBody
	mustJSON(t, f.mustDo(http.MethodPost, base+"/", owner, []byte(fmt.Sprintf(`{
		"kind":"finding","title":"Index on created_at fixes the slow list","body":"EXPLAIN showed a seq scan.",
		"branch":"perf/list","ticket_key":"PROJ-5","ticket_url":"https://tracker.example.test/PROJ-5",
		"tags":["Perf","db"],"assignee":%q}`, f.principalIDOf(owner))), http.StatusCreated, "create memory"), &created)
	if created.Version != 1 || created.Status != "active" || created.Branch != "perf/list" || fmt.Sprint(created.Tags) != "[perf db]" || created.Assignee == "" {
		t.Fatalf("created %+v", created)
	}
	f.mustDo(http.MethodPost, base+"/", owner, []byte(`{"title":"Unrelated note"}`), http.StatusCreated, "second memory")

	// A link the interface would render is refused unless it is http(s).
	f.mustDo(http.MethodPost, base+"/", owner, []byte(`{"title":"x","ticket_url":"javascript:alert(1)"}`), http.StatusBadRequest, "script url")
	f.mustDo(http.MethodPost, base+"/", reader, []byte(`{"title":"x"}`), http.StatusForbidden, "reader create")

	var list struct {
		Items      []memoryBody                     `json:"items"`
		NextCursor string                           `json:"next_cursor"`
		People     map[string]struct{ Name string } `json:"people"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/?branch=perf/list", reader, nil, http.StatusOK, "list by branch"), &list)
	if len(list.Items) != 1 || list.Items[0].ID != created.ID || list.People[created.CreatedBy].Name == "" {
		t.Errorf("list by branch %+v", list)
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/?limit=1", reader, nil, http.StatusOK, "first page"), &list)
	if len(list.Items) != 1 || list.NextCursor == "" {
		t.Errorf("first page %+v", list)
	}
	cursor := list.NextCursor
	list.NextCursor = "" // omitted on the last page, so it must not linger from the first
	mustJSON(t, f.mustDo(http.MethodGet, base+"/?limit=1&cursor="+cursor, reader, nil, http.StatusOK, "second page"), &list)
	if len(list.Items) != 1 || list.Items[0].ID != created.ID || list.NextCursor != "" {
		t.Errorf("second page %+v", list)
	}
	f.mustDo(http.MethodGet, base+"/?kind=rumour", reader, nil, http.StatusBadRequest, "unknown kind")
	f.mustDo(http.MethodGet, base+"/?assignee=me", reader, nil, http.StatusBadRequest, "malformed assignee")

	var facets struct {
		Kinds     []string `json:"kinds"`
		Branches  []string `json:"branches"`
		Tickets   []string `json:"tickets"`
		Tags      []string `json:"tags"`
		Assignees []string `json:"assignees"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/facets", reader, nil, http.StatusOK, "facets"), &facets)
	if len(facets.Kinds) != 6 || fmt.Sprint(facets.Branches) != "[perf/list]" || fmt.Sprint(facets.Tickets) != "[PROJ-5]" ||
		len(facets.Tags) != 2 || len(facets.Assignees) != 1 {
		t.Errorf("facets %+v", facets)
	}

	var hits struct {
		Items     []memoryBody `json:"items"`
		Truncated bool         `json:"truncated"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/search?q=seq+scan&ticket=PROJ-5", reader, nil, http.StatusOK, "search"), &hits)
	if len(hits.Items) != 1 || hits.Items[0].Match != "body" || hits.Items[0].Body != "" || hits.Truncated {
		t.Errorf("search %+v", hits)
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/memories/search?q=index", reader, nil, http.StatusOK, "search mine"), &hits)
	if len(hits.Items) != 1 || hits.Items[0].Project != "mem-owner/work" {
		t.Errorf("search across projects %+v", hits)
	}
	f.mustDo(http.MethodGet, base+"/search", reader, nil, http.StatusBadRequest, "search with no query")

	// Edits need the current version; assignee null clears it.
	var edited memoryBody
	mustJSON(t, f.mustDo(http.MethodPatch, base+"/"+created.ID, owner, []byte(`{"expected_version":1,"assignee":null,"tags":["perf"]}`),
		http.StatusOK, "edit"), &edited)
	if edited.Version != 2 || edited.Assignee != "" || fmt.Sprint(edited.Tags) != "[perf]" || edited.Title != created.Title {
		t.Errorf("edited %+v", edited)
	}
	f.mustDo(http.MethodPatch, base+"/"+created.ID, owner, []byte(`{"expected_version":1,"title":"lost"}`), http.StatusConflict, "stale edit")

	var archived memoryBody
	mustJSON(t, f.mustDo(http.MethodPost, base+"/"+created.ID+"/status", owner, []byte(`{"status":"archived"}`), http.StatusOK, "archive"), &archived)
	if archived.Status != "archived" {
		t.Errorf("archived %+v", archived)
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/?status=archived", reader, nil, http.StatusOK, "archived list"), &list)
	if len(list.Items) != 1 {
		t.Errorf("archived list %+v", list.Items)
	}

	var got memoryBody
	mustJSON(t, f.mustDo(http.MethodGet, base+"/"+created.ID, reader, nil, http.StatusOK, "get"), &got)
	if got.Body != "EXPLAIN showed a seq scan." {
		t.Errorf("get %+v", got)
	}
	f.mustDo(http.MethodDelete, base+"/"+created.ID, reader, nil, http.StatusForbidden, "reader delete")
	f.mustDo(http.MethodDelete, base+"/"+created.ID, owner, nil, http.StatusNoContent, "delete")
	f.mustDo(http.MethodGet, base+"/"+created.ID, owner, nil, http.StatusNotFound, "get deleted")

	// An outsider cannot reach the project's memories at all.
	outsider := f.newUserWithToken("mem-outsider@example.test")
	resp := f.do(http.MethodGet, base+"/", outsider, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusForbidden {
		t.Errorf("outsider list: %d", resp.StatusCode)
	}
}
