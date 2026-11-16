package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
)

// cookieDo sends a browser-session request.
func (f *fixture) cookieDo(method, path string, cookie *http.Cookie, body string) reply {
	req := f.request(method, path, body)
	req.AddCookie(cookie)
	req.Header.Set("X-Requested-With", "cc-web")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return f.send(req)
}

func (f *fixture) signIn(token string) *http.Cookie {
	f.t.Helper()
	r := f.post("/v1/auth/token", fmt.Sprintf(`{"token":%q}`, token))
	c := r.sessionCookie()
	if c == nil {
		f.t.Fatalf("sign in: %d %s", r.status, r.body)
	}
	return c
}

// Deleting a project: only an OWNER, only in the interface, only with the
// path typed back. Once scheduled it is gone for every member; its OWNERs
// see it pending and can cancel; a legal hold refuses the request.
func TestProjectDeletionOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner := f.newUserWithToken("del-owner@example.test")
	contributor := f.newUserWithToken("del-contrib@example.test")
	const project = "del-owner/work"
	f.createProject(owner, project, "Work")
	base := "/v1/projects/" + project
	add, _ := json.Marshal(map[string]string{"principal_id": f.principalIDOf(contributor), "role": "CONTRIBUTOR"})
	f.mustDo(http.MethodPost, base+"/members", owner, add, http.StatusNoContent, "add contributor")
	ownerCookie, contribCookie := f.signIn(owner), f.signIn(contributor)
	confirm := fmt.Sprintf(`{"confirm":%q}`, project)

	var refusal struct {
		Error struct{ Code string } `json:"error"`
	}
	mustJSON(t, f.mustDo(http.MethodDelete, base+"/", owner, []byte(confirm), http.StatusForbidden, "with a token"), &refusal)
	if refusal.Error.Code != "needs_person" {
		t.Errorf("token: code %q", refusal.Error.Code)
	}
	if r := f.cookieDo(http.MethodDelete, base+"/", contribCookie, confirm); r.status != http.StatusForbidden {
		t.Errorf("contributor: %d %s", r.status, r.body)
	}
	if r := f.cookieDo(http.MethodDelete, base+"/", ownerCookie, `{"confirm":"something else"}`); r.status != http.StatusBadRequest {
		t.Errorf("wrong confirmation: %d %s", r.status, r.body)
	}

	r := f.cookieDo(http.MethodDelete, base+"/", ownerCookie, confirm)
	if r.status != http.StatusAccepted {
		t.Fatalf("delete: %d %s", r.status, r.body)
	}
	var scheduled struct {
		PurgeAfter time.Time `json:"purge_after"`
	}
	mustJSON(t, []byte(r.body), &scheduled)
	if d := time.Until(scheduled.PurgeAfter); d < 6*24*time.Hour || d > 8*24*time.Hour {
		t.Errorf("purge after %v", scheduled.PurgeAfter)
	}

	// Gone for everyone, owner included, everywhere.
	f.mustDo(http.MethodGet, base+"/", owner, nil, http.StatusNotFound, "owner reads")
	f.mustDo(http.MethodGet, base+"/memories/", contributor, nil, http.StatusNotFound, "contributor reads")
	var mine []struct{ Slug string }
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/projects", owner, nil, http.StatusOK, "my projects"), &mine)
	if len(mine) != 0 {
		t.Errorf("still listed: %+v", mine)
	}

	var pending struct {
		Items []struct{ ID, Project string } `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/projects/pending-deletion", contributor, nil, http.StatusOK, "contributor pending"), &pending)
	if len(pending.Items) != 0 {
		t.Errorf("a contributor sees pending deletions: %+v", pending.Items)
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/projects/pending-deletion", owner, nil, http.StatusOK, "owner pending"), &pending)
	if len(pending.Items) != 1 || pending.Items[0].Project != project {
		t.Fatalf("owner pending %+v", pending.Items)
	}
	cancel := "/v1/me/projects/pending-deletion/" + pending.Items[0].ID + "/cancel"
	if r := f.cookieDo(http.MethodPost, cancel, contribCookie, ""); r.status != http.StatusForbidden {
		t.Errorf("contributor cancels: %d %s", r.status, r.body)
	}
	if r := f.cookieDo(http.MethodPost, cancel, ownerCookie, ""); r.status != http.StatusNoContent {
		t.Fatalf("owner cancels: %d %s", r.status, r.body)
	}
	f.mustDo(http.MethodGet, base+"/memories/", contributor, nil, http.StatusOK, "back")

	// A legal hold refuses the request.
	err := db.WithoutPrincipal(context.Background(), f.ownerPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE projects SET legal_hold = true WHERE id = $1`, pending.Items[0].ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	r = f.cookieDo(http.MethodDelete, base+"/", ownerCookie, confirm)
	mustJSON(t, []byte(r.body), &refusal)
	if r.status != http.StatusConflict || refusal.Error.Code != "legal_hold" {
		t.Errorf("under hold: %d %s", r.status, r.body)
	}
}
