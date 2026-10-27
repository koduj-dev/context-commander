package http_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAWriteOverTheStorageQuotaIs507AndTheAdminCanLiftIt(t *testing.T) {
	f := newSecurityFixture(t)
	f.appPool.SetStorageQuotaDefault(20)
	admin := f.newInstallationAdminToken("storage-admin@example.test")
	owner := f.newUserWithToken("storage-owner@example.test")

	createBody, _ := json.Marshal(map[string]string{"project": "storage-owner/notes", "name": "Notes"})
	f.mustDo(http.MethodPost, "/v1/projects", owner, createBody, http.StatusCreated, "create project")
	base := "/v1/projects/storage-owner/notes/artifacts/content?content_type=text/plain&expected_version=0&path="
	f.mustDo(http.MethodPut, base+"a.txt", owner, []byte("0123456789"), http.StatusOK, "write under the limit")

	resp := f.do(http.MethodPut, base+"b.txt", owner, []byte(strings.Repeat("x", 11)))
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInsufficientStorage || !strings.Contains(string(raw), `"storage_quota_exceeded"`) {
		t.Fatalf("write over the limit: status=%d body=%s, want 507 storage_quota_exceeded", resp.StatusCode, raw)
	}
	if strings.Contains(string(raw), "20") || strings.Contains(string(raw), "10") {
		t.Errorf("the refusal carries numbers: %s", raw)
	}

	type storage struct {
		NamespaceID string `json:"namespace_id"`
		Slug        string `json:"slug"`
		UsedBytes   int64  `json:"used_bytes"`
		LimitBytes  *int64 `json:"limit_bytes"`
		Override    string `json:"override"`
	}
	var mine struct{ Items []storage }
	if err := json.Unmarshal(f.mustDo(http.MethodGet, "/v1/me/storage", owner, nil, http.StatusOK, "read own storage"), &mine); err != nil {
		t.Fatal(err)
	}
	if len(mine.Items) != 1 || mine.Items[0].Slug != "storage-owner" || mine.Items[0].UsedBytes != 10 ||
		mine.Items[0].LimitBytes == nil || *mine.Items[0].LimitBytes != 20 {
		t.Fatalf("own storage = %+v, want storage-owner holding 10 of 20", mine.Items)
	}
	if mine.Items[0].Override != "" {
		t.Errorf("a member is told how the limit was set: %q", mine.Items[0].Override)
	}

	resp = f.do(http.MethodGet, "/v1/installation/storage", owner, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin list status = %d, want 403", resp.StatusCode)
	}

	var all struct{ Items []storage }
	if err := json.Unmarshal(f.mustDo(http.MethodGet, "/v1/installation/storage", admin, nil, http.StatusOK, "admin list"), &all); err != nil {
		t.Fatal(err)
	}
	var target storage
	for _, n := range all.Items {
		if n.Slug == "storage-owner" {
			target = n
		}
	}
	if target.NamespaceID == "" || target.Override != "default" || target.UsedBytes != 10 {
		t.Fatalf("admin listing entry = %+v", target)
	}

	f.mustDo(http.MethodPut, "/v1/installation/storage/"+target.NamespaceID, admin, []byte(`{"unlimited": true}`),
		http.StatusNoContent, "lift the limit")
	f.mustDo(http.MethodPut, base+"b.txt", owner, []byte(strings.Repeat("x", 11)), http.StatusOK, "write after the limit was lifted")

	resp = f.do(http.MethodPut, "/v1/installation/storage/"+target.NamespaceID, admin, []byte(`{"unlimited": true, "limit_bytes": 5}`))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a limit and unlimited together: status = %d, want 400", resp.StatusCode)
	}
	resp = f.do(http.MethodPut, "/v1/installation/storage/"+target.NamespaceID, owner, []byte(`{}`))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin set status = %d, want 403", resp.StatusCode)
	}
}
