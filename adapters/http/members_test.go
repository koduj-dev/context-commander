package http_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// principalIDViaMe resolves a token's own principal_id through GET /v1/me
// — the project-scoped /members endpoints take a principal_id, unlike the
// organization ones (see userIDByEmail's doc comment), and a test adding a
// *different* person as a member needs their principal id from somewhere
// other than the token that issued it.
func (f *fixture) principalIDViaMe(token string) string {
	f.t.Helper()
	resp := f.do(http.MethodGet, "/v1/me", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		f.t.Fatalf("get me: status=%d", resp.StatusCode)
	}
	var me map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&me)
	id, _ := me["principal_id"].(string)
	if id == "" {
		f.t.Fatal("principal_id is empty")
	}
	return id
}

// TestListMembersReturnsEveryoneIncludingAReaderCaller checks the specific
// gap this endpoint fills: CapMemberRead (unlike CapMemberManage) is
// granted to every role, so a READER — who cannot add or remove anyone —
// must still be able to see the full member list, including their own row
// and the resolved email of a member who has one.
func TestListMembersReturnsEveryoneIncludingAReaderCaller(t *testing.T) {
	f := newFixture(t)
	ownerToken := f.newUserWithToken("owner-listmembers@example.test")
	readerToken := f.newUserWithToken("reader-listmembers@example.test")
	f.createProject(ownerToken, "listmembers/proj", "List Members Proj")

	readerPrincipalID := f.principalIDViaMe(readerToken)
	addBody, _ := json.Marshal(map[string]string{"principal_id": readerPrincipalID, "role": "READER"})
	resp := f.do(http.MethodPost, "/v1/projects/listmembers/proj/members", ownerToken, addBody)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("add reader as member: status=%d", resp.StatusCode)
	}

	// The READER's own token can list members, even though it cannot add one.
	resp = f.do(http.MethodGet, "/v1/projects/listmembers/proj/members", readerToken, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("list members as reader: status=%d body=%s", resp.StatusCode, b)
	}
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list) != 2 {
		t.Fatalf("members = %+v, want 2", list)
	}

	var ownerRow, readerRow map[string]any
	for _, m := range list {
		switch m["role"] {
		case "OWNER":
			ownerRow = m
		case "READER":
			readerRow = m
		}
	}
	if ownerRow == nil || ownerRow["email"] != "owner-listmembers@example.test" {
		t.Errorf("owner row = %+v, want email owner-listmembers@example.test", ownerRow)
	}
	if readerRow == nil || readerRow["principal_id"] != readerPrincipalID {
		t.Errorf("reader row = %+v, want principal_id %s", readerRow, readerPrincipalID)
	}
}

// TestListMembersRejectsNonMember is the authz-boundary check: a caller who
// is not a member of the project at all must not learn who its members are.
func TestListMembersRejectsNonMember(t *testing.T) {
	f := newFixture(t)
	ownerToken := f.newUserWithToken("owner2-listmembers@example.test")
	outsiderToken := f.newUserWithToken("outsider-listmembers@example.test")
	f.createProject(ownerToken, "listmembers2/proj", "List Members Proj 2")

	resp := f.do(http.MethodGet, "/v1/projects/listmembers2/proj/members", outsiderToken, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("list members as outsider: status=%d, want 404 or 403", resp.StatusCode)
	}
}

// TestAdminCannotGrantOwnerOverHTTP is the end-to-end regression test for
// the privilege escalation the /members endpoint used to allow: the handler
// checked only CapMemberManage, which ADMIN holds, so an ADMIN could POST a
// principal they control with role OWNER and inherit ownership through it.
// internal/members.Service now requires CapMemberGrantOwner (OWNER-only) on
// top; this test pins the behavior at the transport the hole was reachable
// from.
func TestAdminCannotGrantOwnerOverHTTP(t *testing.T) {
	f := newFixture(t)
	ownerToken := f.newUserWithToken("owner-escalation@example.test")
	adminToken := f.newUserWithToken("admin-escalation@example.test")
	accompliceToken := f.newUserWithToken("accomplice-escalation@example.test")
	f.createProject(ownerToken, "escalation/proj", "Escalation Proj")

	adminPrincipalID := f.principalIDViaMe(adminToken)
	accomplicePrincipalID := f.principalIDViaMe(accompliceToken)

	addBody, _ := json.Marshal(map[string]string{"principal_id": adminPrincipalID, "role": "ADMIN"})
	resp := f.do(http.MethodPost, "/v1/projects/escalation/proj/members", ownerToken, addBody)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("owner adds admin: status=%d", resp.StatusCode)
	}

	// The escalation attempt: ADMIN adds a principal it controls as OWNER.
	addBody, _ = json.Marshal(map[string]string{"principal_id": accomplicePrincipalID, "role": "OWNER"})
	resp = f.do(http.MethodPost, "/v1/projects/escalation/proj/members", adminToken, addBody)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin grants OWNER: status=%d, want 403", resp.StatusCode)
	}

	// ADMIN keeps ordinary member management.
	addBody, _ = json.Marshal(map[string]string{"principal_id": accomplicePrincipalID, "role": "CONTRIBUTOR"})
	resp = f.do(http.MethodPost, "/v1/projects/escalation/proj/members", adminToken, addBody)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin grants CONTRIBUTOR: status=%d, want 204", resp.StatusCode)
	}

	// No OWNER row was written despite the 403 — a check that denies but
	// writes anyway would still be an escalation.
	resp = f.do(http.MethodGet, "/v1/projects/escalation/proj/members", ownerToken, nil)
	defer func() { _ = resp.Body.Close() }()
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	owners := 0
	for _, m := range list {
		if m["role"] == "OWNER" {
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("OWNER rows = %d, want 1 (only the original owner); members = %+v", owners, list)
	}
}

// TestAddMemberRejectsUnknownRole pins the 400 an unrecognized role gets.
// The check moved from the handler into internal/members.Service, so this
// guards that the domain error still maps back to invalid_request rather
// than a 500.
func TestAddMemberRejectsUnknownRole(t *testing.T) {
	f := newFixture(t)
	ownerToken := f.newUserWithToken("owner-badrole-http@example.test")
	targetToken := f.newUserWithToken("target-badrole-http@example.test")
	f.createProject(ownerToken, "badrole/proj", "Bad Role Proj")

	addBody, _ := json.Marshal(map[string]string{"principal_id": f.principalIDViaMe(targetToken), "role": "SUPERUSER"})
	resp := f.do(http.MethodPost, "/v1/projects/badrole/proj/members", ownerToken, addBody)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("unknown role: status=%d body=%s, want 400", resp.StatusCode, b)
	}
}
