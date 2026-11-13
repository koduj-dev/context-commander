package http_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/directory"
)

// projectRoleOf reads a user's role in a project as the owner role, so RLS
// cannot hide a membership the invitation should have made.
func (f *fixture) projectRoleOf(email, project string) string {
	f.t.Helper()
	var role string
	err := db.WithoutPrincipal(f.t.Context(), f.ownerPool, func(tx pgx.Tx) error {
		err := tx.QueryRow(f.t.Context(),
			`SELECT m.role FROM project_members m
			 JOIN principals p ON p.id = m.principal_id
			 JOIN users u ON u.id = p.user_id
			 JOIN projects pr ON pr.id = m.project_id
			 JOIN namespaces n ON n.id = pr.namespace_id
			 WHERE lower(u.email) = lower($1) AND n.slug || '/' || pr.slug = $2`, email, project).Scan(&role)
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	})
	if err != nil {
		f.t.Fatalf("role of %s in %s: %v", email, project, err)
	}
	return role
}

// An invited address that has no account gets one on its first OIDC
// sign-in, even with sign-up off and an allowlist that does not name it,
// and arrives a member of the project with the invited role.
func TestAnInvitationAdmitsCreatesAndJoinsOnFirstSignIn(t *testing.T) {
	p := &fakeOIDC{}
	f := newOIDCFixture(t, p)
	admin := f.newInstallationAdminToken("inv-admin@example.test")
	f.createSignInRule(admin, "elsewhere.test") // the allowlist is closed to example.test
	owner := f.newUserWithToken("inv-owner@example.test")
	body, _ := json.Marshal(map[string]string{"project": "inv-owner/app", "name": "App"})
	f.mustDo(http.MethodPost, "/v1/projects", owner, body, http.StatusCreated, "create project")

	base := "/v1/projects/inv-owner/app/invitations"
	var inv struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	f.mustDecode(http.MethodPost, base, owner, []byte(`{"email":" Ana@Example.test ","role":"CONTRIBUTOR"}`), http.StatusCreated, "invite", &inv)
	if inv.Email != "ana@example.test" || inv.Role != "CONTRIBUTOR" {
		t.Fatalf("invitation = %+v", inv)
	}
	f.wantStatus(http.MethodPost, base, owner, []byte(`{"email":"not-an-address","role":"READER"}`), http.StatusBadRequest, "bad address")
	f.wantStatus(http.MethodPost, base, owner, []byte(`{"email":"x@example.test","role":"GOD"}`), http.StatusBadRequest, "bad role")

	// Someone who signs in as another address does not pick it up.
	resp := f.oidcSignIn(p, directory.Identity{Provider: "fake", Subject: "eve", Email: "eve@example.test"})
	if got := signInError(t, resp); got != "oidc_domain_not_allowed" {
		t.Fatalf("uninvited address: %q, want oidc_domain_not_allowed", got)
	}

	resp = f.oidcSignIn(p, directory.Identity{Provider: "fake", Subject: "ana", Email: "ana@example.test"})
	if got := signInError(t, resp); got != "" {
		t.Fatalf("invited address: sign_in_error = %q", got)
	}
	if role := f.projectRoleOf("ana@example.test", "inv-owner/app"); role != "CONTRIBUTOR" {
		t.Fatalf("ana's role = %q, want CONTRIBUTOR", role)
	}
	var list struct{ Items []struct{ ID string } }
	f.mustDecode(http.MethodGet, base, owner, nil, http.StatusOK, "list after acceptance", &list)
	if len(list.Items) != 0 {
		t.Fatalf("an accepted invitation is still pending: %+v", list.Items)
	}
}

// An existing account is invited the same way and joins at its next
// browser sign-in; the inviter cannot tell the two cases apart. Only those
// who may manage members can invite, list or revoke.
func TestAnInvitationForAnExistingAccountAndWhoMayInvite(t *testing.T) {
	f := newFixture(t)
	owner := f.newUserWithToken("inv2-owner@example.test")
	reader := f.newUserWithToken("inv2-reader@example.test")
	body, _ := json.Marshal(map[string]string{"project": "inv2-owner/app", "name": "App"})
	f.mustDo(http.MethodPost, "/v1/projects", owner, body, http.StatusCreated, "create project")
	memberBody, _ := json.Marshal(map[string]string{"principal_id": f.principalIDOf(reader), "role": "READER"})
	f.mustDo(http.MethodPost, "/v1/projects/inv2-owner/app/members", owner, memberBody, http.StatusNoContent, "add reader")

	base := "/v1/projects/inv2-owner/app/invitations"
	f.wantStatus(http.MethodPost, base, reader, []byte(`{"email":"bob@example.test","role":"READER"}`), http.StatusForbidden, "reader invites")
	f.wantStatus(http.MethodGet, base, reader, nil, http.StatusForbidden, "reader lists")

	f.newUserWithToken("bob@example.test")
	f.setLocalPassword("bob@example.test", "correct horse battery staple")
	var inv struct{ ID string }
	f.mustDecode(http.MethodPost, base, owner, []byte(`{"email":"bob@example.test","role":"READER"}`), http.StatusCreated, "invite bob", &inv)
	if f.projectRoleOf("bob@example.test", "inv2-owner/app") != "" {
		t.Fatal("an existing account joined before signing in")
	}
	r := f.post("/v1/auth/password", `{"email":"bob@example.test","password":"correct horse battery staple"}`)
	if r.status != http.StatusOK {
		t.Fatalf("bob's password sign-in: %d %s", r.status, r.body)
	}
	if role := f.projectRoleOf("bob@example.test", "inv2-owner/app"); role != "READER" {
		t.Fatalf("bob's role after sign-in = %q, want READER", role)
	}

	var revoked struct{ ID string }
	f.mustDecode(http.MethodPost, base, owner, []byte(`{"email":"carol@example.test","role":"READER"}`), http.StatusCreated, "invite carol", &revoked)
	f.wantStatus(http.MethodDelete, base+"/"+revoked.ID, reader, nil, http.StatusForbidden, "reader revokes")
	f.wantStatus(http.MethodDelete, base+"/"+revoked.ID, owner, nil, http.StatusNoContent, "revoke")
	f.wantStatus(http.MethodDelete, base+"/"+revoked.ID, owner, nil, http.StatusNotFound, "revoke again")
}
