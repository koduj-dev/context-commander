package http_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// A password an administrator sets is temporary: the session it signs in
// may read /me and change the password and nothing else, a token of the
// same person keeps working, and the person's own change clears it.
func TestTemporaryPasswordMustBeChanged(t *testing.T) {
	f := newFixture(t)
	admin := f.newInstallationAdminToken("pw-admin@example.test")
	bobToken := f.newUserWithToken("pw-bob@example.test")
	bobUser := f.userIDByEmail("pw-bob@example.test")

	f.mustDo(http.MethodPut, "/v1/installation/users/"+bobUser+"/password", admin,
		[]byte(`{"password":"a temporary passphrase"}`), http.StatusNoContent, "admin sets a password")

	r := f.post("/v1/auth/password", `{"email":"pw-bob@example.test","password":"a temporary passphrase"}`)
	cookie := r.sessionCookie()
	if r.status != http.StatusOK || cookie == nil {
		t.Fatalf("sign in: %d %s", r.status, r.body)
	}

	var me struct {
		MustChangePassword bool `json:"must_change_password"`
	}
	got := f.cookieDo(http.MethodGet, "/v1/me", cookie, "")
	mustJSON(t, []byte(got.body), &me)
	if got.status != http.StatusOK || !me.MustChangePassword {
		t.Fatalf("/me: %d %s", got.status, got.body)
	}
	got = f.cookieDo(http.MethodGet, "/v1/me/projects", cookie, "")
	var refusal struct {
		Error struct{ Code string } `json:"error"`
	}
	mustJSON(t, []byte(got.body), &refusal)
	if got.status != http.StatusForbidden || refusal.Error.Code != "password_change_required" {
		t.Errorf("anything else before the change: %d %s", got.status, got.body)
	}
	// Bob's own token is not a session and is not held back.
	f.mustDo(http.MethodGet, "/v1/me/projects", bobToken, nil, http.StatusOK, "bob's token")

	body, _ := json.Marshal(map[string]string{"current_password": "a temporary passphrase", "new_password": "bobs own long passphrase"})
	if got := f.cookieDo(http.MethodPut, "/v1/me/password", cookie, string(body)); got.status != http.StatusNoContent {
		t.Fatalf("change: %d %s", got.status, got.body)
	}
	if got := f.cookieDo(http.MethodGet, "/v1/me/projects", cookie, ""); got.status != http.StatusOK {
		t.Errorf("after the change: %d %s", got.status, got.body)
	}
	var after struct {
		MustChangePassword bool `json:"must_change_password"`
	}
	mustJSON(t, []byte(f.cookieDo(http.MethodGet, "/v1/me", cookie, "").body), &after)
	if after.MustChangePassword {
		t.Error("still flagged after the change")
	}
}
