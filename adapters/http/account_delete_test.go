package http_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Deleting an account: only in the interface, confirmed by the email,
// refused for the last OWNER; afterwards the person's tokens and sessions
// are dead, their memberships gone, and what they wrote still there under
// "Deleted user". An administrator can delete someone else.
func TestAccountDeletionOverHTTP(t *testing.T) {
	f := newFixture(t)
	admin := f.newInstallationAdminToken("del-admin@example.test")
	alice := f.newUserWithToken("del-alice@example.test")
	bob := f.newUserWithToken("del-bob@example.test")
	const project = "del-alice/work"
	f.createProject(alice, project, "Work")
	base := "/v1/projects/" + project
	add, _ := json.Marshal(map[string]string{"principal_id": f.principalIDOf(bob), "role": "CONTRIBUTOR"})
	f.mustDo(http.MethodPost, base+"/members", alice, add, http.StatusNoContent, "add bob")
	f.mustDo(http.MethodPost, base+"/memories/", bob, []byte(`{"title":"Bob was here"}`), http.StatusCreated, "bob writes")
	aliceCookie, bobCookie := f.signIn(alice), f.signIn(bob)

	f.mustDo(http.MethodDelete, "/v1/me", bob, []byte(`{"confirm":"del-bob@example.test"}`), http.StatusForbidden, "with a token")
	if r := f.cookieDo(http.MethodDelete, "/v1/me", bobCookie, `{"confirm":"someone@else"}`); r.status != http.StatusBadRequest {
		t.Errorf("wrong confirmation: %d %s", r.status, r.body)
	}
	r := f.cookieDo(http.MethodDelete, "/v1/me", aliceCookie, `{"confirm":"del-alice@example.test"}`)
	if r.status != http.StatusConflict || !strings.Contains(r.body, project) {
		t.Errorf("the last owner: %d %s", r.status, r.body)
	}

	if r := f.cookieDo(http.MethodDelete, "/v1/me", bobCookie, `{"confirm":"DEL-BOB@example.test"}`); r.status != http.StatusNoContent {
		t.Fatalf("bob deletes himself: %d %s", r.status, r.body)
	}
	f.mustDo(http.MethodGet, "/v1/me", bob, nil, http.StatusUnauthorized, "bob's token after deletion")
	if r := f.cookieDo(http.MethodGet, "/v1/me", bobCookie, ""); r.status != http.StatusUnauthorized {
		t.Errorf("bob's session after deletion: %d", r.status)
	}
	var members []struct {
		Email string `json:"email"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/members", alice, nil, http.StatusOK, "members"), &members)
	if len(members) != 1 {
		t.Errorf("members after deletion: %+v", members)
	}
	var mems struct {
		Items []struct {
			Title      string `json:"title"`
			AuthorName string `json:"author_name"`
		} `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/memories/", alice, nil, http.StatusOK, "memories"), &mems)
	if len(mems.Items) != 1 || mems.Items[0].Title != "Bob was here" {
		t.Errorf("bob's memory: %+v", mems.Items)
	}
	var users struct {
		Items []struct {
			Email string `json:"email"`
		} `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/installation/users", admin, nil, http.StatusOK, "directory"), &users)
	for _, u := range users.Items {
		if strings.Contains(u.Email, "del-bob") || strings.Contains(u.Email, "deleted") {
			t.Errorf("a deleted account is still listed: %s", u.Email)
		}
	}

	// An administrator deletes someone else, once Alice is no longer the only
	// owner of anything.
	adminCookie := f.signIn(admin)
	carol := f.newUserWithToken("del-carol@example.test")
	carolID := f.userIDByEmail("del-carol@example.test")
	if r := f.cookieDo(http.MethodDelete, "/v1/installation/users/"+carolID, adminCookie, `{"confirm":"del-carol@example.test"}`); r.status != http.StatusNoContent {
		t.Fatalf("admin deletes carol: %d %s", r.status, r.body)
	}
	f.mustDo(http.MethodGet, "/v1/me", carol, nil, http.StatusUnauthorized, "carol after deletion")
	if r := f.cookieDo(http.MethodDelete, "/v1/installation/users/"+carolID, adminCookie, `{"confirm":"del-carol@example.test"}`); r.status != http.StatusNotFound {
		t.Errorf("deleting twice: %d", r.status)
	}
	// The only administrator cannot delete themselves.
	if r := f.cookieDo(http.MethodDelete, "/v1/me", adminCookie, `{"confirm":"del-admin@example.test"}`); r.status != http.StatusConflict || !strings.Contains(r.body, "last_admin") {
		t.Errorf("the last administrator: %d %s", r.status, r.body)
	}
}
