package http_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Changing a role and removing a member over HTTP: the codes a client sees
// for each refusal, and that removal really ends access.
func TestMemberRoleAndRemovalOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner := f.newUserWithToken("mm-owner@example.test")
	bob := f.newUserWithToken("mm-bob@example.test")
	outsider := f.newUserWithToken("mm-outsider@example.test")
	const project = "mm-owner/work"
	f.createProject(owner, project, "Work")
	base := "/v1/projects/" + project + "/members/"
	ownerID, bobID := f.principalIDOf(owner), f.principalIDOf(bob)
	add, _ := json.Marshal(map[string]string{"principal_id": bobID, "role": "CONTRIBUTOR"})
	f.mustDo(http.MethodPost, "/v1/projects/"+project+"/members", owner, add, http.StatusNoContent, "add bob")

	f.mustDo(http.MethodPatch, base+bobID, bob, []byte(`{"role":"OWNER"}`), http.StatusForbidden, "bob crowns himself")
	f.mustDo(http.MethodPatch, base+bobID, owner, []byte(`{"role":"BOSS"}`), http.StatusBadRequest, "unknown role")
	f.mustDo(http.MethodPatch, base+"not-a-uuid", owner, []byte(`{"role":"READER"}`), http.StatusBadRequest, "bad id")
	f.mustDo(http.MethodPatch, base+f.principalIDOf(outsider), owner, []byte(`{"role":"READER"}`), http.StatusNotFound, "not a member")
	f.mustDo(http.MethodPatch, base+bobID, outsider, []byte(`{"role":"READER"}`), http.StatusNotFound, "outsider")

	var refusal struct {
		Error struct{ Code string } `json:"error"`
	}
	mustJSON(t, f.mustDo(http.MethodDelete, base+ownerID, owner, nil, http.StatusConflict, "last owner leaves"), &refusal)
	if refusal.Error.Code != "last_owner" {
		t.Errorf("last owner leaving: code %q", refusal.Error.Code)
	}

	f.mustDo(http.MethodPatch, base+bobID, owner, []byte(`{"role":"ADMIN"}`), http.StatusNoContent, "promote bob")
	if got := f.projectRoleOf("mm-bob@example.test", project); got != "ADMIN" {
		t.Errorf("bob's role = %q", got)
	}
	f.mustDo(http.MethodDelete, base+bobID, owner, nil, http.StatusNoContent, "remove bob")
	f.mustDo(http.MethodGet, "/v1/projects/"+project+"/members", bob, nil, http.StatusNotFound, "bob after removal")
	f.mustDo(http.MethodDelete, base+bobID, owner, nil, http.StatusNotFound, "remove bob twice")
}
