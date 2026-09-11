package http_test

import (
	"net/http"
	"net/url"
	"testing"
)

// TestAuditCursorPagesBackward is the HTTP-level counterpart of
// internal/audit's TestQueryCursorPagesBackward: the web UI's "Load more"
// button pages with ?cursor=<the previous response's next_cursor>&limit=1
// and must walk every row exactly once, newest first, with no gaps or
// repeats.
//
// It also pins the contract the UI reads to decide whether to show the
// button at all: next_cursor is absent on the final page, so a client never
// has to issue a request just to discover there is nothing left.
func TestAuditCursorPagesBackward(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("audit-page@example.test")
	f.createProject(token, "audit-page/proj", "Audit Page Proj")

	for i, path := range []string{"a.txt", "b.txt", "c.txt"} {
		resp := f.do(http.MethodPut, "/v1/projects/audit-page/proj/artifacts/content?path="+path+"&expected_version=0", token, []byte("v"))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("write artifact %d: status=%d", i, resp.StatusCode)
		}
	}

	resp := f.do(http.MethodGet, "/v1/projects/audit-page/proj/audit", token, nil)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("list audit: status=%d", resp.StatusCode)
	}
	all, cursor := decodeList(t, resp)
	_ = resp.Body.Close()
	if len(all) != 3 {
		t.Fatalf("audit records = %d, want 3", len(all))
	}
	if cursor != "" {
		t.Errorf("a page holding every record still offered next_cursor %q", cursor)
	}

	var pages [][]map[string]any
	cursor = ""
	for i := range all {
		path := "/v1/projects/audit-page/proj/audit?limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		r := f.do(http.MethodGet, path, token, nil)
		page, next := decodeList(t, r)
		_ = r.Body.Close()
		if len(page) != 1 {
			t.Fatalf("page %d: status=%d, got %d rows, want 1", i, r.StatusCode, len(page))
		}
		pages = append(pages, page)

		onLastRow := i == len(all)-1
		if onLastRow && next != "" {
			t.Errorf("final page offered next_cursor %q, want none", next)
		}
		if !onLastRow && next == "" {
			t.Fatalf("page %d offered no next_cursor but %d rows remain", i, len(all)-1-i)
		}
		cursor = next
	}

	for i, page := range pages {
		if page[0]["id"] != all[i]["id"] {
			t.Errorf("page %d id = %v, want %v (order must match the single-page list)", i, page[0]["id"], all[i]["id"])
		}
	}
}

// TestListRejectsAMalformedCursor pins the error mapping: a cursor is opaque
// to clients, so a corrupted or hand-edited one is a client mistake and has
// to read as 400, not as a 500 from a failed decode deeper down.
func TestListRejectsAMalformedCursor(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("cursor-invalid@example.test")
	f.createProject(token, "cursor-invalid/proj", "Cursor Proj")

	for _, path := range []string{
		"/v1/projects/cursor-invalid/proj/audit?cursor=not-a-cursor",
		"/v1/projects/cursor-invalid/proj/artifacts?cursor=not-a-cursor",
		"/v1/projects/cursor-invalid/proj/handoffs?cursor=not-a-cursor",
		"/v1/projects/cursor-invalid/proj/events?cursor=not-a-cursor",
		"/v1/projects/cursor-invalid/proj/secrets?cursor=not-a-cursor",
		"/v1/projects/cursor-invalid/proj/audit?limit=not-a-number",
	} {
		resp := f.do(http.MethodGet, path, token, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("GET %s: status=%d, want 400", path, resp.StatusCode)
		}
	}
}

// TestArtifactListPagesOverHTTP walks the artifact list through the same
// envelope, so the transport is proven end to end and not just the domain
// service underneath it.
func TestArtifactListPagesOverHTTP(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("artifact-page@example.test")
	f.createProject(token, "artifact-page/proj", "Artifact Page Proj")

	const total = 4
	for i := range total {
		path := "note-" + string(rune('a'+i)) + ".md"
		resp := f.do(http.MethodPut, "/v1/projects/artifact-page/proj/artifacts/content?path="+path+"&expected_version=0", token, []byte("v"))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("write %s: status=%d", path, resp.StatusCode)
		}
	}

	seen := map[string]int{}
	cursor := ""
	for page := 0; ; page++ {
		if page > total+1 {
			t.Fatal("paging did not terminate")
		}
		path := "/v1/projects/artifact-page/proj/artifacts?limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		r := f.do(http.MethodGet, path, token, nil)
		items, next := decodeList(t, r)
		_ = r.Body.Close()
		for _, a := range items {
			seen[a["logical_path"].(string)]++
		}
		if next == "" {
			break
		}
		cursor = next
	}

	if len(seen) != total {
		t.Fatalf("walked %d distinct artifacts, want %d (seen: %v)", len(seen), total, seen)
	}
	for path, count := range seen {
		if count != 1 {
			t.Errorf("%s appeared %d times across pages, want exactly 1", path, count)
		}
	}
}
