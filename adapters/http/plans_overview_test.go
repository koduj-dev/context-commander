package http_test

import (
	"fmt"
	"net/http"
	"testing"
)

type overviewJSON struct {
	From  string `json:"from"`
	Days  int    `json:"days"`
	Items []struct {
		Project string `json:"project"`
		Name    string `json:"name"`
		Role    string `json:"role"`
		Counts  struct {
			Open       int `json:"open"`
			InProgress int `json:"in_progress"`
			Done       int `json:"done"`
			Blocked    int `json:"blocked"`
		} `json:"counts"`
		DonePerDay   []int   `json:"done_per_day"`
		LastActivity *string `json:"last_activity"`
	} `json:"items"`
}

// The overview counts items by status, puts what was finished today in the
// last day of the window, leaves abandoned plans out, lists only the caller's
// projects, and orders the most recently active first.
func TestProjectsOverviewOverHTTP(t *testing.T) {
	f := newFixture(t)
	alice := f.newUserWithToken("ov-alice@example.test")
	bob := f.newUserWithToken("ov-bob@example.test")
	f.createProject(alice, "ov-alice/quiet", "Quiet")
	f.createProject(alice, "ov-alice/busy", "Busy")
	f.createProject(bob, "ov-bob/own", "Own")
	base := "/v1/projects/ov-alice/busy"

	var plan struct {
		ID    string `json:"id"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodPost, base+"/plans/", alice,
		[]byte(`{"title":"Ship","items":[{"text":"a"},{"text":"b"},{"text":"c"},{"text":"d"}]}`), http.StatusCreated, "create plan"), &plan)
	for i, status := range []string{"done", "done", "blocked"} {
		f.mustDo(http.MethodPatch, base+"/plans/items/"+plan.Items[i].ID, alice, []byte(fmt.Sprintf(`{"status":%q}`, status)), http.StatusOK, "set "+status)
	}
	var dropped struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	mustJSON(t, f.mustDo(http.MethodPost, base+"/plans/", alice, []byte(`{"title":"Dropped","items":[{"text":"x"}]}`), http.StatusCreated, "create plan"), &dropped)
	f.mustDo(http.MethodPatch, base+"/plans/"+dropped.ID, alice, []byte(fmt.Sprintf(`{"status":"abandoned","expected_version":%d}`, dropped.Version)), http.StatusOK, "abandon")

	var ov overviewJSON
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/projects/overview", alice, nil, http.StatusOK, "overview"), &ov)
	if ov.Days != 14 || ov.From == "" || len(ov.Items) != 2 {
		t.Fatalf("overview %+v", ov)
	}
	busy, quiet := ov.Items[0], ov.Items[1]
	if busy.Project != "ov-alice/busy" || busy.Name != "Busy" || busy.Role != "OWNER" || busy.LastActivity == nil {
		t.Errorf("busy %+v", busy)
	}
	if c := busy.Counts; c.Done != 2 || c.Blocked != 1 || c.Open != 1 || c.InProgress != 0 {
		t.Errorf("busy counts %+v", c)
	}
	if len(busy.DonePerDay) != 14 || busy.DonePerDay[13] != 2 {
		t.Errorf("busy done per day %v", busy.DonePerDay)
	}
	if quiet.Project != "ov-alice/quiet" || quiet.LastActivity != nil || quiet.Counts.Done != 0 || len(quiet.DonePerDay) != 14 {
		t.Errorf("quiet %+v", quiet)
	}

	mustJSON(t, f.mustDo(http.MethodGet, base+"/plans/overview?days=3", alice, nil, http.StatusOK, "project overview"), &ov)
	if len(ov.Items) != 1 || ov.Items[0].Project != "ov-alice/busy" || len(ov.Items[0].DonePerDay) != 3 || ov.Items[0].DonePerDay[2] != 2 {
		t.Errorf("project overview %+v", ov)
	}
	f.mustDo(http.MethodGet, base+"/plans/overview", bob, nil, http.StatusNotFound, "an outsider's project overview")
	f.mustDo(http.MethodGet, "/v1/me/projects/overview?days=0", alice, nil, http.StatusBadRequest, "zero days")
	f.mustDo(http.MethodGet, "/v1/me/projects/overview?days=91", alice, nil, http.StatusBadRequest, "too many days")
	f.mustDo(http.MethodGet, "/v1/me/projects/overview?days=many", alice, nil, http.StatusBadRequest, "days not a number")
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/projects/overview", bob, nil, http.StatusOK, "bob's overview"), &ov)
	if len(ov.Items) != 1 || ov.Items[0].Project != "ov-bob/own" {
		t.Errorf("bob sees %+v", ov.Items)
	}
}
