package http_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestPlansOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner := f.newUserWithToken("plans-owner@example.test")
	reader := f.newUserWithToken("plans-reader@example.test")
	createBody, _ := json.Marshal(map[string]string{"project": "plans-owner/work", "name": "Work"})
	f.mustDo(http.MethodPost, "/v1/projects", owner, createBody, http.StatusCreated, "create project")
	memberBody, _ := json.Marshal(map[string]string{"principal_id": f.principalIDOf(reader), "role": "READER"})
	f.mustDo(http.MethodPost, "/v1/projects/plans-owner/work/members", owner, memberBody, http.StatusNoContent, "add reader")
	base := "/v1/projects/plans-owner/work"

	var phase struct{ ID string }
	mustJSON(t, f.mustDo(http.MethodPost, base+"/phases", owner, []byte(`{"title":"Release 1.2"}`), http.StatusCreated, "create phase"), &phase)

	type item struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Text     string `json:"text"`
		Assignee string `json:"assignee"`
	}
	var plan struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
		Items   []item `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodPost, base+"/plans/", owner, []byte(fmt.Sprintf(
		`{"phase_id":%q,"title":"Release checklist","items":[{"text":"Tag"},{"text":"Smoke test","kind":"check"}]}`, phase.ID)),
		http.StatusCreated, "create plan"), &plan)

	var started item
	mustJSON(t, f.mustDo(http.MethodPatch, base+"/plans/items/"+plan.Items[0].ID, owner, []byte(`{"status":"in_progress"}`),
		http.StatusOK, "start an item"), &started)
	if started.Status != "in_progress" || started.Assignee == "" {
		t.Errorf("started %+v", started)
	}
	f.mustDo(http.MethodPost, base+"/plans/presence", owner, []byte(fmt.Sprintf(`{"item_id":%q,"activity":"tagging"}`, plan.Items[0].ID)),
		http.StatusOK, "report presence")

	var board struct {
		Phases []struct {
			Counts struct {
				InProgress int `json:"in_progress"`
				Open       int `json:"open"`
			} `json:"counts"`
		} `json:"phases"`
		Presence []struct {
			You      bool   `json:"you"`
			ItemText string `json:"item_text"`
		} `json:"presence"`
		People map[string]struct{ Name string } `json:"people"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/plans/board", reader, nil, http.StatusOK, "board as reader"), &board)
	if len(board.Phases) != 1 || board.Phases[0].Counts.InProgress != 1 || board.Phases[0].Counts.Open != 1 {
		t.Errorf("phases %+v", board.Phases)
	}
	if len(board.Presence) != 1 || board.Presence[0].You || board.Presence[0].ItemText != "Tag" || len(board.People) == 0 {
		t.Errorf("presence %+v people %+v", board.Presence, board.People)
	}

	var mine struct {
		Items []struct {
			Project string `json:"project"`
		} `json:"items"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/presence", reader, nil, http.StatusOK, "my presence"), &mine)
	if len(mine.Items) != 1 || mine.Items[0].Project != "plans-owner/work" {
		t.Errorf("my presence %+v", mine)
	}

	var hits []struct {
		ItemID string `json:"item_id"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/plans/search?q=smoke", reader, nil, http.StatusOK, "search"), &hits)
	if len(hits) != 1 || hits[0].ItemID != plan.Items[1].ID {
		t.Errorf("hits %+v", hits)
	}
	var history []struct{ From, To string }
	mustJSON(t, f.mustDo(http.MethodGet, base+"/plans/items/"+plan.Items[0].ID+"/history", reader, nil, http.StatusOK, "history"), &history)
	if len(history) != 1 || history[0].To != "in_progress" {
		t.Errorf("history %+v", history)
	}

	// A reader reads and cannot write.
	resp := f.do(http.MethodPatch, base+"/plans/items/"+plan.Items[1].ID, reader, []byte(`{"status":"done"}`))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("reader tick: status %d, want 403", resp.StatusCode)
	}
	// A stale rewrite of the plan's own text is a conflict.
	f.mustDo(http.MethodPatch, base+"/plans/items/"+plan.Items[1].ID, owner, []byte(`{"note":"needs staging"}`), http.StatusOK, "note")
	resp = f.do(http.MethodPatch, base+"/plans/"+plan.ID, owner, []byte(fmt.Sprintf(`{"title":"New","expected_version":%d}`, plan.Version)))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("stale rewrite: status %d, want 409", resp.StatusCode)
	}
	// phase_id null takes the plan out of its phase; absent would leave it.
	var moved struct {
		PhaseID string `json:"phase_id"`
	}
	mustJSON(t, f.mustDo(http.MethodPatch, base+"/plans/"+plan.ID, owner, []byte(`{"phase_id":null}`), http.StatusOK, "unphase"), &moved)
	if moved.PhaseID != "" {
		t.Errorf("phase_id null left the plan in phase %s", moved.PhaseID)
	}
	resp = f.do(http.MethodPatch, base+"/plans/items/"+plan.Items[1].ID, owner, []byte(`{"status":"finished"}`))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown status: %d, want 400", resp.StatusCode)
	}

	f.mustDo(http.MethodDelete, base+"/plans/presence", owner, nil, http.StatusNoContent, "clear presence")
	f.mustDo(http.MethodDelete, base+"/plans/"+plan.ID, owner, nil, http.StatusNoContent, "delete plan")
	f.mustDo(http.MethodDelete, base+"/phases/"+phase.ID, owner, nil, http.StatusNoContent, "delete phase")
}

func mustJSON(t *testing.T, raw []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}
