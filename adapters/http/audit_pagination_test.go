package http_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestAuditBeforeIDPagesBackward is the HTTP-level counterpart of
// internal/audit's TestQueryBeforeIDPagesBackward: the web UI's "Load more"
// button pages with ?before_id=<last row's id>&limit=1 and must walk every
// row exactly once, newest first, with no gaps or repeats.
func TestAuditBeforeIDPagesBackward(t *testing.T) {
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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list audit: status=%d", resp.StatusCode)
	}
	var all []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&all)
	if len(all) != 3 {
		t.Fatalf("audit records = %d, want 3", len(all))
	}

	var pages [][]map[string]any
	var beforeID string
	for i := 0; i < 3; i++ {
		path := "/v1/projects/audit-page/proj/audit?limit=1"
		if beforeID != "" {
			path += "&before_id=" + beforeID
		}
		r := f.do(http.MethodGet, path, token, nil)
		var page []map[string]any
		_ = json.NewDecoder(r.Body).Decode(&page)
		_ = r.Body.Close()
		if len(page) != 1 {
			t.Fatalf("page %d: status=%d, got %d rows, want 1", i, r.StatusCode, len(page))
		}
		pages = append(pages, page)
		beforeID = strconv.FormatFloat(page[0]["id"].(float64), 'f', -1, 64)
	}

	// One more page past the last row must come back empty.
	lastResp := f.do(http.MethodGet, "/v1/projects/audit-page/proj/audit?limit=1&before_id="+beforeID, token, nil)
	var lastPage []map[string]any
	_ = json.NewDecoder(lastResp.Body).Decode(&lastPage)
	_ = lastResp.Body.Close()
	if len(lastPage) != 0 {
		t.Fatalf("page past the last row = %+v, want empty", lastPage)
	}

	for i, page := range pages {
		if page[0]["id"] != all[i]["id"] {
			t.Errorf("page %d id = %v, want %v (order must match the unpaginated list)", i, page[0]["id"], all[i]["id"])
		}
	}

	invalidResp := f.do(http.MethodGet, "/v1/projects/audit-page/proj/audit?before_id=not-a-number", token, nil)
	_ = invalidResp.Body.Close()
	if invalidResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("before_id=not-a-number: status=%d, want 400", invalidResp.StatusCode)
	}
}
