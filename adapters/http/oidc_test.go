package http_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

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
	started directory.AuthRequest
	// starts is every attempt begun, so a callback for an earlier one can
	// be told apart from a forged one.
	starts []directory.AuthRequest
	// callback is what the adapter handed Exchange last.
	callback directory.Callback
	identity directory.Identity
	err      error
	// signup is what AllowsSignup reports. The existing tests are about the
	// flow, not the policy, so they opt in; the policy has its own tests.
	signup bool
}

const fakeCode = "code-the-provider-issued"

func (p *fakeOIDC) Provider() string   { return "fake" }
func (p *fakeOIDC) Label() string      { return "Fake SSO" }
func (p *fakeOIDC) AllowsSignup() bool { return p.signup }

func (p *fakeOIDC) AuthCodeURL(req directory.AuthRequest) (string, error) {
	p.started = req
	p.starts = append(p.starts, req)
	return "https://idp.example.test/authorize?state=" + url.QueryEscape(req.State), nil
}

func (p *fakeOIDC) Exchange(_ context.Context, cb directory.Callback, req directory.AuthRequest) (directory.Identity, error) {
	p.callback = cb
	// The adapter must hand back exactly an attempt it started: the nonce
	// and verifier are what the real provider checks.
	known := false
	for _, s := range p.starts {
		known = known || s == req
	}
	if cb.Code != fakeCode || !known {
		return directory.Identity{}, directory.ErrSignInRejected
	}
	return p.identity, p.err
}

// testCookieKey stands in for the key serve derives from the root key.
var testCookieKey = []byte("0123456789abcdef0123456789abcdef")

func newOIDCFixture(t *testing.T, p *fakeOIDC) *fixture {
	t.Helper()
	// Recording on: several of these tests are about which outcomes write
	// a security event and which do not.
	return newFixtureWithDeps(t, boolPtr(true), ratelimit.Limits{}, func(d *cchttp.Deps) {
		d.OIDC = []directory.RedirectAuthenticator{p}
		d.OIDCCookieKey = testCookieKey
	})
}

// signedAttempt builds an attempt cookie the way the server does, so a test
// can present one the server never issued — for a provider since removed, or
// one that has outlived its lifetime.
func signedAttempt(t *testing.T, fields map[string]any) *http.Cookie {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(fields["s"].(string)))
	name := "cc_oidc_" + hex.EncodeToString(sum[:8])
	payload := base64.RawURLEncoding.EncodeToString(raw)
	m := hmac.New(sha256.New, testCookieKey)
	m.Write([]byte(name))
	m.Write([]byte{0})
	m.Write([]byte(payload))
	return &http.Cookie{Name: name, Value: payload + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))} //nolint:gosec // G124: a request cookie in a test; attributes are the server's to set
}

// securityEvents counts recorded refusals with this reason.
func (f *fixture) securityEvents(reason string) int {
	f.t.Helper()
	var n int
	err := db.WithoutPrincipal(f.t.Context(), f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(f.t.Context(), `SELECT count(*) FROM security_events WHERE reason = $1`, reason).Scan(&n)
	})
	if err != nil {
		f.t.Fatalf("count security events: %v", err)
	}
	return n
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

// attemptCookie is the attempt cookie a response set, by its prefix — the
// name is derived from the state.
func (n navigation) attemptCookie() *http.Cookie {
	for _, c := range n.cookies {
		if strings.HasPrefix(c.Name, "cc_oidc_") && c.MaxAge > 0 {
			return c
		}
	}
	return nil
}

// cleared reports whether the response expired the named cookie.
func (n navigation) cleared(name string) bool {
	c := n.cookie(name)
	return c != nil && c.MaxAge < 0
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
func beginOIDC(t *testing.T, f *fixture, p *fakeOIDC, held ...*http.Cookie) (*http.Cookie, string) {
	t.Helper()
	c, state, _ := beginOIDCResp(t, f, p, held...)
	return c, state
}

func beginOIDCResp(t *testing.T, f *fixture, p *fakeOIDC, held ...*http.Cookie) (*http.Cookie, string, navigation) {
	t.Helper()
	resp := f.navigate("/v1/auth/oidc/fake/start", held...)
	if resp.status != http.StatusFound {
		t.Fatalf("start: status %d, want a redirect to the provider", resp.status)
	}
	loc, err := url.Parse(resp.location)
	if err != nil || loc.Host != "idp.example.test" {
		t.Fatalf("start redirected to %q, want the provider", resp.location)
	}
	c := resp.attemptCookie()
	if c == nil {
		t.Fatal("start set no cc_oidc_ cookie")
	}
	if !c.HttpOnly || c.Path != "/v1/auth/oidc" || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("attempt cookie: HttpOnly=%v Path=%q SameSite=%v, want HttpOnly, /v1/auth/oidc, Lax",
			c.HttpOnly, c.Path, c.SameSite)
	}
	return c, p.started.State, resp
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
	p := &fakeOIDC{signup: true, identity: directory.Identity{
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
	if !resp.cleared(attempt.Name) {
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
	if n := f.securityEvents("oidc callback state did not match the attempt this browser started"); n != 1 {
		t.Fatalf("%d state-mismatch events recorded, want 1", n)
	}
	// The browser's real attempt is still there for its own callback.
	if resp.cleared(attempt.Name) {
		t.Error("a forged callback cleared the browser's genuine attempt")
	}
}

func TestOIDCCallbackWithoutAnAttemptStartsNothing(t *testing.T) {
	p := &fakeOIDC{identity: directory.Identity{Provider: "fake", Subject: "s", Email: "x@example.test"}}
	f := newOIDCFixture(t, p)
	resp := f.navigate("/v1/auth/oidc/callback?state=anything&code=" + fakeCode)
	if got := signInError(t, resp); got != "oidc_expired" {
		t.Fatalf("sign_in_error = %q, want oidc_expired", got)
	}
	// Nothing to match against is an expired attempt, not an attack.
	if n := f.securityEvents("oidc callback state did not match the attempt this browser started"); n != 0 {
		t.Fatalf("%d state-mismatch events for a browser holding no attempt, want 0", n)
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
		{"domain not allowed", "&code=" + fakeCode, directory.ErrDomainNotAllowed, "oidc_domain_not_allowed"},
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

func TestOIDCCallbackForAProviderNoLongerConfigured(t *testing.T) {
	// An attempt started before a restart that removed its provider. The
	// cookie is well-formed and the state matches; there is just nobody left
	// to redeem the code with.
	f := newOIDCFixture(t, &fakeOIDC{})
	attempt := signedAttempt(t, map[string]any{"p": "gone", "t": time.Now().Unix(), "s": "st", "n": "nonce", "v": "verifier"})
	resp := f.navigate("/v1/auth/oidc/callback?state=st&code="+fakeCode, attempt)
	if got := signInError(t, resp); got != "oidc_expired" {
		t.Fatalf("sign_in_error = %q, want oidc_expired", got)
	}
	if resp.cookie("cc_session") != nil {
		t.Fatal("a session was started for a provider that is not configured")
	}
}

// With sign-up off — the default — a person the provider authenticates but
// who has no account here is told so, and nothing is created. This is the
// difference between "our Keycloak" and "anyone with a Google account".
func TestOIDCWithoutSignupCreatesNoAccount(t *testing.T) {
	p := &fakeOIDC{identity: directory.Identity{
		Provider: "fake", Subject: "stranger", Email: "stranger@example.test", DisplayName: "Stranger",
	}}
	f := newOIDCFixture(t, p)
	attempt, state := beginOIDC(t, f, p)
	resp := f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(state)+"&code="+fakeCode, attempt)

	if got := signInError(t, resp); got != "oidc_no_account" {
		t.Fatalf("sign_in_error = %q, want oidc_no_account", got)
	}
	if resp.cookie("cc_session") != nil {
		t.Fatal("a session was started for someone with no account")
	}
	err := db.WithoutPrincipal(t.Context(), f.ownerPool, func(tx pgx.Tx) error {
		_, err := identity.NewStore().GetUserByEmail(t.Context(), tx, "stranger@example.test")
		return err
	})
	if !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("a user was created with sign-up off (lookup: %v)", err)
	}
}

// Sign-up off still lets in the people an administrator created: the account
// exists under the same address, so the first sign-in links to it.
func TestOIDCWithoutSignupLinksAnAccountAnAdminCreated(t *testing.T) {
	p := &fakeOIDC{identity: directory.Identity{
		Provider: "fake", Subject: "sub-ivan", Email: "ivan@example.test", DisplayName: "Ivan",
	}}
	f := newOIDCFixture(t, p)
	err := db.WithoutPrincipal(t.Context(), f.ownerPool, func(tx pgx.Tx) error {
		store := identity.NewStore()
		// Mixed case on purpose: the administrator typed it one way and the
		// provider sends it another.
		user, err := store.CreateUser(t.Context(), tx, "Ivan@Example.test", "Ivan")
		if err != nil {
			return err
		}
		_, err = store.CreatePrincipalForUser(t.Context(), tx, user.ID)
		return err
	})
	if err != nil {
		t.Fatalf("create the account: %v", err)
	}

	attempt, state := beginOIDC(t, f, p)
	resp := f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(state)+"&code="+fakeCode, attempt)
	if got := signInError(t, resp); got != "" {
		t.Fatalf("sign-in failed with %q", got)
	}
	if resp.cookie("cc_session") == nil {
		t.Fatal("no session for an account the administrator created")
	}
}
