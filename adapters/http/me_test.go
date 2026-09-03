package http_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestGetMeReturnsCallerIdentity is the web UI's PAT-login validation path:
// paste a token, call GET /v1/me, and show "signed in as ..." from the
// response — or reject the token if this 401s.
func TestGetMeReturnsCallerIdentity(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("me@example.test")

	resp := f.do(http.MethodGet, "/v1/me", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("get me: status=%d body=%s", resp.StatusCode, b)
	}
	var me map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&me)
	if me["kind"] != "user" {
		t.Errorf("kind = %+v, want user", me["kind"])
	}
	if me["email"] != "me@example.test" {
		t.Errorf("email = %+v, want me@example.test", me["email"])
	}
	if pid, _ := me["principal_id"].(string); pid == "" {
		t.Error("principal_id is empty")
	}
}

// TestGetMeReportsInstallationAdminStatus is the web UI's only signal for
// whether to show the Administration nav entry at all (docs/adr/0017 has
// no cheaper "am I an admin" endpoint than this).
func TestGetMeReportsInstallationAdminStatus(t *testing.T) {
	f := newFixture(t)
	plainToken := f.newUserWithToken("plain-admin-check@example.test")
	adminToken := f.newUserWithToken("admin-admin-check@example.test")
	f.grantInstallationAdmin("admin-admin-check@example.test")

	resp := f.do(http.MethodGet, "/v1/me", plainToken, nil)
	var plainMe map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&plainMe)
	_ = resp.Body.Close()
	if plainMe["is_installation_admin"] != false {
		t.Errorf("plain user is_installation_admin = %+v, want false", plainMe["is_installation_admin"])
	}

	resp = f.do(http.MethodGet, "/v1/me", adminToken, nil)
	var adminMe map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&adminMe)
	_ = resp.Body.Close()
	if adminMe["is_installation_admin"] != true {
		t.Errorf("installation admin is_installation_admin = %+v, want true", adminMe["is_installation_admin"])
	}
}

func TestListMyProjectsOnlyReturnsMemberships(t *testing.T) {
	f := newFixture(t)
	aliceToken := f.newUserWithToken("alice-me@example.test")
	bobToken := f.newUserWithToken("bob-me@example.test")

	f.createProject(aliceToken, "alice-me/proj", "Alice Proj")
	f.createProject(bobToken, "bob-me/proj", "Bob Proj")

	resp := f.do(http.MethodGet, "/v1/me/projects", aliceToken, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("list my projects: status=%d body=%s", resp.StatusCode, b)
	}
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("alice's projects = %+v, want exactly 1", list)
	}
	if list[0]["namespace"] != "alice-me" || list[0]["slug"] != "proj" {
		t.Errorf("project = %+v, want alice-me/proj", list[0])
	}
	if list[0]["role"] != "OWNER" {
		t.Errorf("role = %+v, want OWNER", list[0]["role"])
	}
}

func TestListMyOrganizationsOnlyReturnsMemberships(t *testing.T) {
	f := newFixture(t)
	ownerToken := f.newUserWithToken("org-me-owner@example.test")
	outsiderToken := f.newUserWithToken("org-me-outsider@example.test")

	createBody, _ := json.Marshal(map[string]string{"name": "Acme Me"})
	resp := f.do(http.MethodPost, "/v1/organizations", ownerToken, createBody)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create organization: status=%d", resp.StatusCode)
	}

	resp = f.do(http.MethodGet, "/v1/me/organizations", ownerToken, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list my organizations (owner): status=%d", resp.StatusCode)
	}
	var ownerList []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&ownerList)
	if len(ownerList) != 1 || ownerList[0]["name"] != "Acme Me" {
		t.Fatalf("owner's organizations = %+v, want exactly [Acme Me]", ownerList)
	}

	resp2 := f.do(http.MethodGet, "/v1/me/organizations", outsiderToken, nil)
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("list my organizations (outsider): status=%d", resp2.StatusCode)
	}
	var outsiderList []map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&outsiderList)
	if len(outsiderList) != 0 {
		t.Fatalf("outsider's organizations = %+v, want empty", outsiderList)
	}
}
