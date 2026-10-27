package http_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"

	cchttp "github.com/koduj-dev/context-commander/adapters/http"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/directory"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/ratelimit"
)

// fakeOIDC stands in for the provider. What it cannot stand in for — that an
// ID token's signature, issuer, audience and nonce are verified — is tested
// against a real Keycloak in internal/directory. What is tested here is the
// adapter: the attempt cookie, the state check, and what each outcome tells
// the browser.
type fakeOIDC struct {
	started  directory.AuthRequest
	identity directory.Identity
	err      error
}

const fakeCode = "code-the-provider-issued"

func (p *fakeOIDC) Provider() string { return "fake" }
func (p *fakeOIDC) Label() string    { return "Fake SSO" }

func (p *fakeOIDC) AuthCodeURL(req directory.AuthRequest) (string, error) {
	p.started = req
	return "https://idp.example.test/authorize?state=" + url.QueryEscape(req.State), nil
}

func (p *fakeOIDC) Exchange(_ context.Context, code string, req directory.AuthRequest) (directory.Identity, error) {
	// The adapter must hand back exactly the attempt it started: the nonce
	// and verifier are what the real provider checks.
	if code != fakeCode || req != p.started {
		return directory.Identity{}, directory.ErrSignInRejected
	}
	return p.identity, p.err
}

func newOIDCFixture(t *testing.T, p *fakeOIDC) *fixture {
	t.Helper()
	return newFixtureWithDeps(t, nil, ratelimit.Limits{}, func(d *cchttp.Deps) {
		d.OIDC = []directory.RedirectAuthenticator{p}
	})
}

// navigation is where a browser was sent, with the body already read and
// closed — the same reasoning as reply in auth_test.go.
type navigation struct {
	status   int
	location string
	cookies  []*http.Cookie
}

func (n navigation) cookie(name string) *http.Cookie {
	for _, c := range n.cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// navigate is a browser following a link, minus following the redirect:
// every assertion below is about where it is sent.
func (f *fixture) navigate(path string, cookies ...*http.Cookie) navigation {
	f.t.Helper()
	client := *f.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req := f.request(http.MethodGet, path, "")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		f.t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return navigation{status: resp.StatusCode, location: resp.Header.Get("Location"), cookies: resp.Cookies()}
}

// begin starts an attempt and returns the cookie it set and the state the
// provider would echo back.
func beginOIDC(t *testing.T, f *fixture, p *fakeOIDC) (*http.Cookie, string) {
	t.Helper()
	resp := f.navigate("/v1/auth/oidc/fake/start")
	if resp.status != http.StatusFound {
		t.Fatalf("start: status %d, want a redirect to the provider", resp.status)
	}
	loc, err := url.Parse(resp.location)
	if err != nil || loc.Host != "idp.example.test" {
		t.Fatalf("start redirected to %q, want the provider", resp.location)
	}
	c := resp.cookie("cc_oidc")
	if c == nil {
		t.Fatal("start set no cc_oidc cookie")
	}
	if !c.HttpOnly || c.Path != "/v1/auth/oidc" || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cc_oidc cookie: HttpOnly=%v Path=%q SameSite=%v, want HttpOnly, /v1/auth/oidc, Lax",
			c.HttpOnly, c.Path, c.SameSite)
	}
	return c, p.started.State
}

func signInError(t *testing.T, resp navigation) string {
	t.Helper()
	if resp.status != http.StatusFound {
		t.Fatalf("status %d, want a redirect back to the app", resp.status)
	}
	loc, err := url.Parse(resp.location)
	if err != nil || loc.Path != "/" {
		t.Fatalf("redirected to %q, want the app", resp.location)
	}
	return loc.Query().Get("sign_in_error")
}

func TestOIDCSignInProvisionsAndStartsASession(t *testing.T) {
	p := &fakeOIDC{identity: directory.Identity{
		Provider: "fake", Subject: "sub-123", Email: "olga@example.test", DisplayName: "Olga",
	}}
	f := newOIDCFixture(t, p)

	attempt, state := beginOIDC(t, f, p)
	resp := f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(state)+"&code="+fakeCode, attempt)

	if got := signInError(t, resp); got != "" {
		t.Fatalf("sign-in failed with %q", got)
	}
	session := resp.cookie("cc_session")
	if session == nil || session.Value == "" {
		t.Fatal("callback started no session")
	}
	if c := resp.cookie("cc_oidc"); c == nil || c.MaxAge >= 0 {
		t.Error("the attempt cookie was not cleared — a second callback could replay it")
	}
	if status := f.withCookie(http.MethodGet, "/v1/me", session, true); status != http.StatusOK {
		t.Fatalf("GET /v1/me with the new session: %d", status)
	}

	// Provisioned and linked by subject, not by the address.
	err := db.WithoutPrincipal(t.Context(), f.ownerPool, func(tx pgx.Tx) error {
		userID, err := identity.NewStore().UserForDirectoryIdentity(t.Context(), tx, "fake", "sub-123")
		if err != nil {
			return err
		}
		user, err := identity.NewStore().GetUserByEmail(t.Context(), tx, "olga@example.test")
		if err != nil {
			return err
		}
		if user.ID != userID {
			return errors.New("the linked identity points at a different user")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("provisioned user: %v", err)
	}
}

func TestOIDCCallbackRefusesAStateItDidNotIssue(t *testing.T) {
	p := &fakeOIDC{identity: directory.Identity{Provider: "fake", Subject: "s", Email: "x@example.test"}}
	f := newOIDCFixture(t, p)

	attempt, _ := beginOIDC(t, f, p)
	// A callback carrying someone else's state — the shape of a login CSRF,
	// where an attacker's own code is pushed into a victim's browser.
	resp := f.navigate("/v1/auth/oidc/callback?state=not-the-one&code="+fakeCode, attempt)
	if got := signInError(t, resp); got != "oidc_expired" {
		t.Fatalf("sign_in_error = %q, want oidc_expired", got)
	}
	if resp.cookie("cc_session") != nil {
		t.Fatal("a session was started for a callback whose state did not match")
	}
}

func TestOIDCCallbackWithoutAnAttemptStartsNothing(t *testing.T) {
	p := &fakeOIDC{identity: directory.Identity{Provider: "fake", Subject: "s", Email: "x@example.test"}}
	f := newOIDCFixture(t, p)
	resp := f.navigate("/v1/auth/oidc/callback?state=anything&code=" + fakeCode)
	if got := signInError(t, resp); got != "oidc_expired" {
		t.Fatalf("sign_in_error = %q, want oidc_expired", got)
	}
}

func TestOIDCCallbackTellsThePersonWhatWentWrong(t *testing.T) {
	cases := []struct {
		name  string
		query string
		err   error
		want  string
	}{
		{"cancelled at the provider", "&error=access_denied", nil, "oidc_cancelled"},
		{"provider error", "&error=server_error", nil, "oidc_failed"},
		{"provider down", "&code=" + fakeCode, directory.ErrUnavailable, "oidc_unavailable"},
		{"no email", "&code=" + fakeCode, directory.ErrNoEmail, "oidc_no_email"},
		{"unverified email", "&code=" + fakeCode, directory.ErrEmailNotVerified, "oidc_email_unverified"},
		{"rejected code", "&code=forged", nil, "oidc_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &fakeOIDC{
				identity: directory.Identity{Provider: "fake", Subject: "s", Email: "x@example.test"},
				err:      tc.err,
			}
			f := newOIDCFixture(t, p)
			attempt, state := beginOIDC(t, f, p)
			resp := f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(state)+tc.query, attempt)
			if got := signInError(t, resp); got != tc.want {
				t.Fatalf("sign_in_error = %q, want %q", got, tc.want)
			}
			if resp.cookie("cc_session") != nil {
				t.Fatal("a failed sign-in started a session")
			}
		})
	}
}

func TestOIDCUnknownProviderIsNotFound(t *testing.T) {
	f := newOIDCFixture(t, &fakeOIDC{})
	resp := f.navigate("/v1/auth/oidc/nobody/start")
	if resp.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.status)
	}
}

func TestAuthMethodsListsOIDCProviders(t *testing.T) {
	f := newOIDCFixture(t, &fakeOIDC{})
	var got struct {
		Methods []struct {
			Kind  string `json:"kind"`
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"methods"`
	}
	decodeInto(t, f.get("/v1/auth/methods"), &got)
	for _, m := range got.Methods {
		if m.Kind == "oidc" {
			if m.ID != "fake" || m.Label != "Fake SSO" {
				t.Fatalf("oidc method = %+v, want id fake, label Fake SSO", m)
			}
			return
		}
	}
	t.Fatalf("no oidc method offered: %+v", got.Methods)
}
