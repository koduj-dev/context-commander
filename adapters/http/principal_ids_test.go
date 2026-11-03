package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
)

// registerAgent creates a service principal as an installation admin and
// returns both of its ids: the agent's own id and the principal it acts as.
// They are different UUIDs for the same agent, which is exactly the mix-up
// the tests below are about.
func (f *fixture) registerAgent(adminToken, name string) (agentID, principalID string) {
	f.t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	var sp struct {
		ID string `json:"id"`
	}
	f.mustDecode(http.MethodPost, "/v1/installation/service-principals", adminToken, body, http.StatusCreated, "register agent", &sp)
	ctx := context.Background()
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM principals WHERE service_principal_id = $1`, sp.ID).Scan(&principalID)
	})
	if err != nil {
		f.t.Fatalf("register agent: principal for %s: %v", sp.ID, err)
	}
	return sp.ID, principalID
}

func (f *fixture) wantStatus(method, path, token string, body []byte, want int, step string) {
	f.t.Helper()
	resp := f.do(method, path, token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		b, _ := io.ReadAll(resp.Body)
		f.t.Fatalf("%s: status=%d body=%s, want %d", step, resp.StatusCode, b, want)
	}
}

// TestAddMemberWithAnAgentsOwnIDIsBadRequest is the regression for a 500
// found while seeding an E2E: an agent's id where its principal_id belongs
// failed on project_members' foreign key and surfaced as "internal error".
func TestAddMemberWithAnAgentsOwnIDIsBadRequest(t *testing.T) {
	f := newFixture(t)
	adminToken := f.newInstallationAdminToken("admin-wrongid@example.test")
	f.createProject(adminToken, "wrongid/proj", "Wrong Id Proj")
	agentID, agentPrincipalID := f.registerAgent(adminToken, "wrong-id-agent")

	body, _ := json.Marshal(map[string]string{"principal_id": agentID, "role": "CONTRIBUTOR"})
	f.wantStatus(http.MethodPost, "/v1/projects/wrongid/proj/members", adminToken, body, http.StatusBadRequest, "add agent by its own id")

	body, _ = json.Marshal(map[string]string{"principal_id": agentPrincipalID, "role": "CONTRIBUTOR"})
	f.wantStatus(http.MethodPost, "/v1/projects/wrongid/proj/members", adminToken, body, http.StatusNoContent, "add agent by principal_id")

	// Adding the same principal again hit the primary key the same way.
	f.wantStatus(http.MethodPost, "/v1/projects/wrongid/proj/members", adminToken, body, http.StatusConflict, "add agent twice")
}

// TestIssueTokenForAnAgentsOwnIDIsNotFound: the same mix-up on the admin's
// token endpoints. The id is the resource in the path, so it is a 404 — and
// the listing says so too, rather than reporting an empty list of tokens
// for something that does not exist.
func TestIssueTokenForAnAgentsOwnIDIsNotFound(t *testing.T) {
	f := newFixture(t)
	adminToken := f.newInstallationAdminToken("admin-wrongid-token@example.test")
	agentID, agentPrincipalID := f.registerAgent(adminToken, "wrong-id-token-agent")

	body := []byte(`{"client_type":"claude-code"}`)
	f.wantStatus(http.MethodPost, "/v1/installation/principals/"+agentID+"/tokens", adminToken, body, http.StatusNotFound, "issue for agent id")
	f.wantStatus(http.MethodGet, "/v1/installation/principals/"+agentID+"/tokens", adminToken, nil, http.StatusNotFound, "list for agent id")

	f.wantStatus(http.MethodPost, "/v1/installation/principals/"+agentPrincipalID+"/tokens", adminToken, body, http.StatusCreated, "issue for principal_id")
	f.wantStatus(http.MethodGet, "/v1/installation/principals/"+agentPrincipalID+"/tokens", adminToken, nil, http.StatusOK, "list for principal_id")
}

// TestAddOrganizationMemberWithAPrincipalIDIsBadRequest: organization
// membership is keyed by user, so the mirror-image mistake — a person's
// principal_id where their user id belongs — hit the same kind of foreign
// key.
func TestAddOrganizationMemberWithAPrincipalIDIsBadRequest(t *testing.T) {
	f := newFixture(t)
	ownerToken := f.newUserWithToken("org-owner-wrongid@example.test")
	memberToken := f.newUserWithToken("org-member-wrongid@example.test")

	var org struct {
		ID string `json:"id"`
	}
	f.mustDecode(http.MethodPost, "/v1/organizations", ownerToken, []byte(`{"name":"Wrong Id Org"}`), http.StatusCreated, "create organization", &org)

	body, _ := json.Marshal(map[string]string{"user_id": f.principalIDViaMe(memberToken), "role": "MEMBER"})
	f.wantStatus(http.MethodPost, "/v1/organizations/"+org.ID+"/members", ownerToken, body, http.StatusBadRequest, "add by principal_id")

	body, _ = json.Marshal(map[string]string{"user_id": f.userIDByEmail("org-member-wrongid@example.test"), "role": "MEMBER"})
	f.wantStatus(http.MethodPost, "/v1/organizations/"+org.ID+"/members", ownerToken, body, http.StatusOK, "add by user id")
	f.wantStatus(http.MethodPost, "/v1/organizations/"+org.ID+"/members", ownerToken, body, http.StatusConflict, "add twice")
}
