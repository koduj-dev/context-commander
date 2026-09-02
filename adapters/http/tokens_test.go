package http_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestIssueListRevokeToken walks the full self-service PAT lifecycle: issue
// (the raw token appears exactly once), list (never re-exposes it), revoke,
// then confirm the revoked credential still lists (with revoked_at set)
// rather than disappearing.
func TestIssueListRevokeToken(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("tokens@example.test")

	body, _ := json.Marshal(map[string]any{"client_type": "cli"})
	resp := f.do(http.MethodPost, "/v1/me/tokens", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("issue token: status=%d body=%s", resp.StatusCode, b)
	}
	var issued map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&issued)
	newToken, _ := issued["token"].(string)
	credID, _ := issued["credential_id"].(string)
	if newToken == "" || credID == "" {
		t.Fatalf("issue token response = %+v, want token and credential_id", issued)
	}

	// The new token actually authenticates.
	meResp := f.do(http.MethodGet, "/v1/me", newToken, nil)
	_ = meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("get me with newly issued token: status=%d", meResp.StatusCode)
	}

	listResp := f.do(http.MethodGet, "/v1/me/tokens", token, nil)
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list tokens: status=%d", listResp.StatusCode)
	}
	var list []map[string]any
	_ = json.NewDecoder(listResp.Body).Decode(&list)
	if len(list) != 2 { // the fixture's own bootstrap token + the one just issued
		t.Fatalf("list tokens = %+v, want 2", list)
	}
	for _, c := range list {
		if _, has := c["token"]; has {
			t.Fatalf("list tokens response leaks raw token: %+v", c)
		}
		if _, has := c["token_hash"]; has {
			t.Fatalf("list tokens response leaks token_hash: %+v", c)
		}
	}

	revokeResp := f.do(http.MethodDelete, "/v1/me/tokens/"+credID, token, nil)
	_ = revokeResp.Body.Close()
	if revokeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke token: status=%d", revokeResp.StatusCode)
	}

	// The revoked token can no longer authenticate.
	revokedMeResp := f.do(http.MethodGet, "/v1/me", newToken, nil)
	_ = revokedMeResp.Body.Close()
	if revokedMeResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("get me with revoked token: status=%d, want 401", revokedMeResp.StatusCode)
	}

	afterResp := f.do(http.MethodGet, "/v1/me/tokens", token, nil)
	defer func() { _ = afterResp.Body.Close() }()
	var after []map[string]any
	_ = json.NewDecoder(afterResp.Body).Decode(&after)
	var found bool
	for _, c := range after {
		if c["id"] == credID {
			found = true
			if c["revoked_at"] == nil {
				t.Errorf("revoked credential %+v, want revoked_at set", c)
			}
		}
	}
	if !found {
		t.Fatalf("revoked credential %s missing from list %+v", credID, after)
	}
}

// TestRevokeTokenRejectsAnotherPrincipalsToken is the self-only boundary
// check: alice must not be able to revoke bob's token, and the failure
// must look exactly like the token not existing.
func TestRevokeTokenRejectsAnotherPrincipalsToken(t *testing.T) {
	f := newFixture(t)
	aliceToken := f.newUserWithToken("alice-tokens@example.test")
	bobToken := f.newUserWithToken("bob-tokens@example.test")

	body, _ := json.Marshal(map[string]any{"client_type": "cli"})
	resp := f.do(http.MethodPost, "/v1/me/tokens", bobToken, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("issue bob's token: status=%d", resp.StatusCode)
	}
	var issued map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&issued)
	bobCredID, _ := issued["credential_id"].(string)

	revokeResp := f.do(http.MethodDelete, "/v1/me/tokens/"+bobCredID, aliceToken, nil)
	defer func() { _ = revokeResp.Body.Close() }()
	if revokeResp.StatusCode != http.StatusNotFound {
		t.Fatalf("alice revoking bob's token: status=%d, want 404", revokeResp.StatusCode)
	}
}

func TestIssueTokenRejectsPastExpiry(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("expiry-tokens@example.test")

	body, _ := json.Marshal(map[string]any{"client_type": "cli", "expires_in_seconds": -3600})
	resp := f.do(http.MethodPost, "/v1/me/tokens", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("issue token with past expiry: status=%d body=%s", resp.StatusCode, b)
	}
}
