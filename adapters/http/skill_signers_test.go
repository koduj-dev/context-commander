package http_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
)

// An organization's owner manages its signing keys over HTTP without being
// an installation administrator; its members read them, outsiders do not,
// and the installation routes stay administrator-only.
func TestOrganizationSkillSignersOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner := f.newUserWithToken("signers-owner@example.test")
	member := f.newUserWithToken("signers-member@example.test")
	outsider := f.newUserWithToken("signers-outsider@example.test")

	var org struct {
		ID string `json:"id"`
	}
	f.mustDecode(http.MethodPost, "/v1/organizations", owner, []byte(`{"name":"Signers Org"}`), http.StatusCreated, "create organization", &org)
	add, _ := json.Marshal(map[string]string{"user_id": f.userIDByEmail("signers-member@example.test"), "role": "MEMBER"})
	f.wantStatus(http.MethodPost, "/v1/organizations/"+org.ID+"/members", owner, add, http.StatusOK, "add member")

	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	key, _ := json.Marshal(map[string]string{"name": "release", "public_key": base64.StdEncoding.EncodeToString(pub)})
	base := "/v1/organizations/" + org.ID + "/skill-signers"

	f.wantStatus(http.MethodPost, "/v1/installation/skill-signers", owner, key, http.StatusForbidden, "installation key by a non-admin")
	f.wantStatus(http.MethodPost, base, member, key, http.StatusForbidden, "organization key by a member")
	var signer struct {
		ID             string `json:"id"`
		OrganizationID string `json:"organization_id"`
	}
	f.mustDecode(http.MethodPost, base, owner, key, http.StatusCreated, "organization key by the owner", &signer)
	if signer.OrganizationID != org.ID {
		t.Fatalf("signer %+v", signer)
	}

	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	f.mustDecode(http.MethodGet, base, member, nil, http.StatusOK, "member lists", &list)
	if len(list.Items) != 1 || list.Items[0].ID != signer.ID {
		t.Errorf("organization keys %+v", list.Items)
	}
	f.wantStatus(http.MethodGet, base, outsider, nil, http.StatusForbidden, "outsider lists")
	f.mustDecode(http.MethodGet, "/v1/installation/skill-signers", outsider, nil, http.StatusOK, "installation keys", &list)
	if len(list.Items) != 0 {
		t.Errorf("an organization key among the installation keys: %+v", list.Items)
	}

	f.wantStatus(http.MethodDelete, "/v1/installation/skill-signers/"+signer.ID, owner, nil, http.StatusForbidden, "revoke via the installation route")
	f.wantStatus(http.MethodDelete, base+"/"+signer.ID, owner, nil, http.StatusNoContent, "revoke")
}
