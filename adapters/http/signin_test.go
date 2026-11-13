package http_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/directory"
)

type signInRule struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Value         string `json:"value"`
	Organizations []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"organizations"`
}

func (f *fixture) createOrganization(token, name string) string {
	f.t.Helper()
	var org struct {
		ID string `json:"id"`
	}
	body, _ := json.Marshal(map[string]string{"name": name})
	f.mustDecode(http.MethodPost, "/v1/organizations", token, body, http.StatusCreated, "create organization "+name, &org)
	return org.ID
}

func (f *fixture) createSignInRule(admin, value string, orgs ...string) signInRule {
	f.t.Helper()
	if orgs == nil {
		orgs = []string{}
	}
	body, _ := json.Marshal(map[string]any{"value": value, "organization_ids": orgs})
	var rule signInRule
	f.mustDecode(http.MethodPost, "/v1/installation/signin-rules", admin, body, http.StatusCreated, "create rule "+value, &rule)
	return rule
}

// orgMembers lists the users of an organization by email, read as the owner
// role so RLS cannot hide a membership the grant should have made.
func (f *fixture) orgMembers(orgID string) map[string]string {
	f.t.Helper()
	out := map[string]string{}
	err := db.WithoutPrincipal(f.t.Context(), f.ownerPool, func(tx pgx.Tx) error {
		rows, err := tx.Query(f.t.Context(),
			`SELECT u.email, m.role FROM organization_members m JOIN users u ON u.id = m.user_id WHERE m.organization_id = $1`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var email, role string
			if err := rows.Scan(&email, &role); err != nil {
				return err
			}
			out[email] = role
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatalf("org members: %v", err)
	}
	return out
}

func TestSignInRulesAreAdminOnlyAndValidated(t *testing.T) {
	f := newFixture(t)
	admin := f.newInstallationAdminToken("signin-admin@example.test")
	someone := f.newUserWithToken("signin-someone@example.test")
	org := f.createOrganization(someone, "4NET")

	for _, path := range []string{"/v1/installation/signin-rules", "/v1/installation/organizations"} {
		f.wantStatus(http.MethodGet, path, someone, nil, http.StatusForbidden, "non-admin GET "+path)
	}
	body, _ := json.Marshal(map[string]any{"value": "4net.tv", "organization_ids": []string{}})
	f.wantStatus(http.MethodPost, "/v1/installation/signin-rules", someone, body, http.StatusForbidden, "non-admin create")

	// The admin picks from every organization, including ones they are not in.
	var orgs struct {
		Items []struct{ ID, Name string }
	}
	f.mustDecode(http.MethodGet, "/v1/installation/organizations", admin, nil, http.StatusOK, "list organizations", &orgs)
	if len(orgs.Items) != 1 || orgs.Items[0].ID != org || orgs.Items[0].Name != "4NET" {
		t.Fatalf("organizations = %+v, want only 4NET", orgs.Items)
	}

	domain := f.createSignInRule(admin, " 4NET.tv ", org)
	if domain.Kind != "domain" || domain.Value != "4net.tv" || len(domain.Organizations) != 1 || domain.Organizations[0].Name != "4NET" {
		t.Fatalf("created rule = %+v", domain)
	}
	address := f.createSignInRule(admin, "Guest@Gmail.com")
	if address.Kind != "address" || address.Value != "guest@gmail.com" || len(address.Organizations) != 0 {
		t.Fatalf("created address rule = %+v", address)
	}

	f.wantStatus(http.MethodPost, "/v1/installation/signin-rules", admin, body, http.StatusConflict, "duplicate rule")
	for _, bad := range []map[string]any{
		{"value": "*.4net.tv"},
		{"value": "not a domain"},
		{"value": "x.test", "organization_ids": []string{"not-a-uuid"}},
		{"value": "y.test", "organization_ids": []string{"00000000-0000-0000-0000-000000000001"}},
	} {
		raw, _ := json.Marshal(bad)
		f.wantStatus(http.MethodPost, "/v1/installation/signin-rules", admin, raw, http.StatusBadRequest, "invalid rule")
	}

	set, _ := json.Marshal(map[string]any{"organization_ids": []string{org}})
	var updated signInRule
	f.mustDecode(http.MethodPut, "/v1/installation/signin-rules/"+address.ID+"/organizations", admin, set, http.StatusOK, "set organizations", &updated)
	if len(updated.Organizations) != 1 || updated.Organizations[0].ID != org {
		t.Fatalf("updated rule = %+v", updated)
	}

	f.wantStatus(http.MethodDelete, "/v1/installation/signin-rules/"+address.ID, admin, nil, http.StatusNoContent, "delete rule")
	f.wantStatus(http.MethodDelete, "/v1/installation/signin-rules/"+address.ID, admin, nil, http.StatusNotFound, "delete again")
	var list struct{ Items []signInRule }
	f.mustDecode(http.MethodGet, "/v1/installation/signin-rules", admin, nil, http.StatusOK, "list rules", &list)
	if len(list.Items) != 1 || list.Items[0].ID != domain.ID {
		t.Fatalf("rules after delete = %+v", list.Items)
	}
}

func (f *fixture) oidcSignIn(p *fakeOIDC, id directory.Identity) navigation {
	f.t.Helper()
	p.identity = id
	attempt, state := beginOIDC(f.t, f, p)
	return f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(state)+"&code="+fakeCode, attempt)
}

func TestSignInRuleAdmitsCreatesAndGrantsWithSignupOff(t *testing.T) {
	p := &fakeOIDC{} // sign-up off: only a rule may create an account
	f := newOIDCFixture(t, p)
	admin := f.newInstallationAdminToken("rules-admin@example.test")
	owner := f.newUserWithToken("rules-owner@example.test")
	first := f.createOrganization(owner, "First")
	second := f.createOrganization(owner, "Second")
	f.createSignInRule(admin, "4net.tv", first, second)
	f.createSignInRule(admin, "guest@gmail.com", second)

	resp := f.oidcSignIn(p, directory.Identity{Provider: "fake", Subject: "ana", Email: "Ana@4net.tv", DisplayName: "Ana"})
	if got := signInError(t, resp); got != "" {
		t.Fatalf("listed domain: sign_in_error = %q", got)
	}
	if resp.cookie("cc_session") == nil {
		t.Fatal("listed domain: no session")
	}
	for _, org := range []string{first, second} {
		if role := f.orgMembers(org)["Ana@4net.tv"]; role != "MEMBER" {
			t.Fatalf("ana in %s: role %q, want MEMBER (members %v)", org, role, f.orgMembers(org))
		}
	}

	resp = f.oidcSignIn(p, directory.Identity{Provider: "fake", Subject: "guest", Email: "guest@gmail.com"})
	if got := signInError(t, resp); got != "" {
		t.Fatalf("listed address: sign_in_error = %q", got)
	}
	if _, ok := f.orgMembers(second)["guest@gmail.com"]; !ok {
		t.Fatal("listed address: not granted its organization")
	}
	if _, ok := f.orgMembers(first)["guest@gmail.com"]; ok {
		t.Fatal("listed address: granted an organization its rule does not name")
	}

	resp = f.oidcSignIn(p, directory.Identity{Provider: "fake", Subject: "eve", Email: "eve@gmail.com"})
	if got := signInError(t, resp); got != "oidc_domain_not_allowed" {
		t.Fatalf("unlisted address: sign_in_error = %q, want oidc_domain_not_allowed", got)
	}
	if resp.cookie("cc_session") != nil {
		t.Fatal("unlisted address: a session was started")
	}
}

// A rule grants on creation only. An account that already existed — here
// one an administrator created — links on its first sign-in and joins
// nothing it was not already in.
func TestSignInRuleDoesNotGrantToAnExistingAccount(t *testing.T) {
	p := &fakeOIDC{}
	f := newOIDCFixture(t, p)
	admin := f.newInstallationAdminToken("grant-admin@example.test")
	org := f.createOrganization(admin, "Org")
	f.createSignInRule(admin, "example.test", org)
	f.newUserWithToken("existing@example.test")

	resp := f.oidcSignIn(p, directory.Identity{Provider: "fake", Subject: "existing", Email: "existing@example.test"})
	if got := signInError(t, resp); got != "" {
		t.Fatalf("sign_in_error = %q", got)
	}
	if _, ok := f.orgMembers(org)["existing@example.test"]; ok {
		t.Fatal("an existing account was granted membership by a rule")
	}
}

func TestSignInGoogleWorkspaceIsRequiredForADomain(t *testing.T) {
	p := &fakeOIDC{admission: directory.Admission{RequireHostedDomain: true}}
	f := newOIDCFixture(t, p)
	admin := f.newInstallationAdminToken("hd-admin@example.test")
	f.createSignInRule(admin, "4net.tv")

	resp := f.oidcSignIn(p, directory.Identity{Provider: "fake", Subject: "personal", Email: "ana@4net.tv"})
	if got := signInError(t, resp); got != "oidc_domain_not_allowed" {
		t.Fatalf("personal account under a work address: sign_in_error = %q, want oidc_domain_not_allowed", got)
	}
	resp = f.oidcSignIn(p, directory.Identity{Provider: "fake", Subject: "workspace", Email: "ana@4net.tv", HostedDomain: "4net.tv"})
	if got := signInError(t, resp); got != "" {
		t.Fatalf("Workspace account: sign_in_error = %q", got)
	}
}

// The operator's flag list and the administrator's rules are one list.
func TestSignInFlagDomainsAndRulesAreOneList(t *testing.T) {
	p := &fakeOIDC{signup: true, admission: directory.Admission{Domains: []string{"example.com"}}}
	f := newOIDCFixture(t, p)
	admin := f.newInstallationAdminToken("union-admin@example.test")
	f.createSignInRule(admin, "contractor@elsewhere.test")

	for _, email := range []string{"ana@example.com", "contractor@elsewhere.test"} {
		if got := signInError(t, f.oidcSignIn(p, directory.Identity{Provider: "fake", Subject: email, Email: email})); got != "" {
			t.Fatalf("%s: sign_in_error = %q", email, got)
		}
	}
	if got := signInError(t, f.oidcSignIn(p, directory.Identity{Provider: "fake", Subject: "x", Email: "x@elsewhere.test"})); got != "oidc_domain_not_allowed" {
		t.Fatalf("x@elsewhere.test: sign_in_error = %q, want oidc_domain_not_allowed", got)
	}
}
