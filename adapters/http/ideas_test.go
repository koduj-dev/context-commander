package http_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type ideaBody struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	Title           string `json:"title"`
	Proposal        string `json:"proposal"`
	ClaimedByYou    bool   `json:"claimed_by_you"`
	ResultingPlanID string `json:"resulting_plan_id"`
	CreatedBy       string `json:"created_by"`
	People          map[string]struct {
		Name string `json:"name"`
	} `json:"people"`
}

// The whole loop over HTTP: a person adds an idea, an agent claims and
// proposes with a draft plan, the agent's token cannot accept, and the same
// person signed in to the interface can — which activates the plan.
func TestTheIdeaLoopOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner := f.newUserWithToken("ideas-owner@example.test")
	reader := f.newUserWithToken("ideas-reader@example.test")
	createBody, _ := json.Marshal(map[string]string{"project": "ideas-owner/work", "name": "Work"})
	f.mustDo(http.MethodPost, "/v1/projects", owner, createBody, http.StatusCreated, "create project")
	memberBody, _ := json.Marshal(map[string]string{"principal_id": f.principalIDOf(reader), "role": "READER"})
	f.mustDo(http.MethodPost, "/v1/projects/ideas-owner/work/members", owner, memberBody, http.StatusNoContent, "add reader")
	base := "/v1/projects/ideas-owner/work/ideas"

	var idea ideaBody
	mustJSON(t, f.mustDo(http.MethodPost, base+"/", owner, []byte(`{"title":"Speed up the export","priority":"high"}`),
		http.StatusCreated, "add idea"), &idea)
	if idea.Status != "open" || idea.People[idea.CreatedBy].Name == "" {
		t.Errorf("created %+v", idea)
	}
	f.mustDo(http.MethodPost, base+"/", reader, []byte(`{"title":"x"}`), http.StatusForbidden, "reader adds")
	f.mustDo(http.MethodPost, base+"/", owner, []byte(`{"title":"x","ticket_url":"javascript:alert(1)"}`), http.StatusBadRequest, "script URL")

	var list struct {
		Items []ideaBody `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/?status=open", reader, nil, http.StatusOK, "list as reader"), &list)
	if len(list.Items) != 1 || list.Items[0].Title != "Speed up the export" {
		t.Errorf("listed %+v", list.Items)
	}

	var claimed ideaBody
	mustJSON(t, f.mustDo(http.MethodPost, base+"/claim-next", owner, nil, http.StatusOK, "claim next"), &claimed)
	if claimed.ID != idea.ID || claimed.Status != "claimed" || !claimed.ClaimedByYou {
		t.Errorf("claimed %+v", claimed)
	}
	f.mustDo(http.MethodPost, base+"/claim-next", reader, nil, http.StatusForbidden, "reader claims")

	var proposed ideaBody
	mustJSON(t, f.mustDo(http.MethodPost, base+"/"+idea.ID+"/propose", owner,
		[]byte(`{"proposal":"Stream rows instead of buffering.","plan":{"items":[{"text":"Switch to a cursor"}]}}`),
		http.StatusOK, "propose"), &proposed)
	if proposed.Status != "proposed" || proposed.ResultingPlanID == "" {
		t.Fatalf("proposed %+v", proposed)
	}

	var refusal struct {
		Error struct{ Code string } `json:"error"`
	}
	mustJSON(t, f.mustDo(http.MethodPost, base+"/"+idea.ID+"/accept", owner, nil, http.StatusForbidden, "accept with a PAT"), &refusal)
	if refusal.Error.Code != "needs_person" {
		t.Fatalf("accept with a PAT: code %q, want needs_person", refusal.Error.Code)
	}

	signIn := f.post("/v1/auth/token", fmt.Sprintf(`{"token":%q}`, owner))
	cookie := signIn.sessionCookie()
	if cookie == nil {
		t.Fatalf("sign-in with the token: %d %s", signIn.status, signIn.body)
	}
	req := f.request(http.MethodPost, base+"/"+idea.ID+"/accept", "")
	req.AddCookie(cookie)
	req.Header.Set("X-Requested-With", "cc-web")
	accepted := f.send(req)
	if accepted.status != http.StatusOK || !strings.Contains(accepted.body, `"status":"accepted"`) {
		t.Fatalf("accept in a session: %d %s", accepted.status, accepted.body)
	}

	var plan struct{ Status string }
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/projects/ideas-owner/work/plans/"+proposed.ResultingPlanID, owner, nil, http.StatusOK, "get plan"), &plan)
	if plan.Status != "active" {
		t.Errorf("the accepted proposal's plan is %s, want active", plan.Status)
	}

	// Nothing left: 204, not an error.
	f.mustDo(http.MethodPost, base+"/claim-next", owner, nil, http.StatusNoContent, "claim next on an empty stack")
}

// The dashboard's one read of what waits in every idea stack: only projects
// with something waiting, proposals first, nothing from a project the caller
// is not in.
func TestMyIdeaCountsAcrossProjects(t *testing.T) {
	f := newFixture(t)
	alice := f.newUserWithToken("counts-alice@example.test")
	bob := f.newUserWithToken("counts-bob@example.test")
	for _, p := range []string{"counts-alice/quiet", "counts-alice/busy"} {
		body, _ := json.Marshal(map[string]string{"project": p, "name": p})
		f.mustDo(http.MethodPost, "/v1/projects", alice, body, http.StatusCreated, "create "+p)
	}
	other, _ := json.Marshal(map[string]string{"project": "counts-bob/secret", "name": "Secret"})
	f.mustDo(http.MethodPost, "/v1/projects", bob, other, http.StatusCreated, "create bob's")
	f.mustDo(http.MethodPost, "/v1/projects/counts-bob/secret/ideas/", bob, []byte(`{"title":"hidden"}`), http.StatusCreated, "bob's idea")

	base := "/v1/projects/counts-alice/busy/ideas"
	f.mustDo(http.MethodPost, base+"/", alice, []byte(`{"title":"one"}`), http.StatusCreated, "idea one")
	var second ideaBody
	mustJSON(t, f.mustDo(http.MethodPost, base+"/", alice, []byte(`{"title":"two","priority":"high"}`), http.StatusCreated, "idea two"), &second)
	f.mustDo(http.MethodPost, base+"/claim-next", alice, nil, http.StatusOK, "claim")
	f.mustDo(http.MethodPost, base+"/"+second.ID+"/propose", alice, []byte(`{"proposal":"do it"}`), http.StatusOK, "propose")

	var counts struct {
		Items []struct {
			Project  string `json:"project"`
			Open     int    `json:"open"`
			Proposed int    `json:"proposed"`
		} `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/ideas/counts", alice, nil, http.StatusOK, "counts"), &counts)
	if len(counts.Items) != 1 || counts.Items[0].Project != "counts-alice/busy" || counts.Items[0].Open != 1 || counts.Items[0].Proposed != 1 {
		t.Fatalf("counts = %+v, want only counts-alice/busy with 1 open and 1 proposed", counts.Items)
	}
}
