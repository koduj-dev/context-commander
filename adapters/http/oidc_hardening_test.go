package http_test

import (
	"encoding/base64"
	"encoding/json"
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

// The attempt cookie is signed: one edited in the browser, or planted by a
// sibling subdomain, is refused before anything in it is used.
func TestOIDCCallbackRefusesATamperedAttempt(t *testing.T) {
	p := &fakeOIDC{signup: true, identity: directory.Identity{Provider: "fake", Subject: "s", Email: "x@example.test"}}
	f := newOIDCFixture(t, p)
	attempt, state := beginOIDC(t, f, p)

	payload, sig, _ := strings.Cut(attempt.Value, ".")
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["v"] = "a-verifier-the-attacker-knows"
	raw, _ = json.Marshal(fields)
	cases := map[string]string{
		"edited payload, original signature": base64.RawURLEncoding.EncodeToString(raw) + "." + sig,
		"no signature at all":                base64.RawURLEncoding.EncodeToString(raw),
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			forged := &http.Cookie{Name: attempt.Name, Value: value} //nolint:gosec // G124: a request cookie in a test
			resp := f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(state)+"&code="+fakeCode, forged)
			if got := signInError(t, resp); got != "oidc_expired" {
				t.Fatalf("sign_in_error = %q, want oidc_expired", got)
			}
			if resp.cookie("cc_session") != nil {
				t.Fatal("a tampered attempt started a session")
			}
		})
	}
	if n := f.securityEvents("oidc attempt cookie failed its signature check"); n != len(cases) {
		t.Fatalf("%d tamper events recorded, want %d", n, len(cases))
	}
}

// The sibling-subdomain login CSRF: the attacker starts an attempt, signs in
// at the provider as themselves, and plants their own genuine, correctly
// signed attempt cookie in the victim's browser. A sibling cannot set a
// __Host- cookie — the browser enforces that, which no Go test can — so what
// is tested here is the server's half: with Secure cookies the attempt lives
// only under the __Host- name, and the same signed value under any name a
// sibling could set is not read.
func TestOIDCAttemptCookieCannotBeTransplantedFromASibling(t *testing.T) {
	p := &fakeOIDC{signup: true, identity: directory.Identity{
		Provider: "fake", Subject: "sub-attacker", Email: "attacker@example.test", DisplayName: "Attacker",
	}}
	f := newFixtureWithDeps(t, boolPtr(true), ratelimit.Limits{}, func(d *cchttp.Deps) {
		d.OIDC = []directory.RedirectAuthenticator{p}
		d.OIDCCookieKey = testCookieKey
		d.SessionCookieSecure = true
	})

	// The attacker's own attempt; beginOIDC checks it is __Host-, Path=/,
	// Secure and has no Domain.
	attempt, state := beginOIDC(t, f, p)
	if !strings.HasPrefix(attempt.Name, "__Host-cc_oidc_") {
		t.Fatalf("attempt cookie %q is not __Host- prefixed with Secure cookies on", attempt.Name)
	}

	planted := &http.Cookie{Name: strings.TrimPrefix(attempt.Name, "__Host-"), Value: attempt.Value} //nolint:gosec // G124: a request cookie in a test
	resp := f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(state)+"&code="+fakeCode, planted)
	if got := signInError(t, resp); got != "oidc_expired" {
		t.Fatalf("sign_in_error = %q, want oidc_expired", got)
	}
	if resp.cookie("cc_session") != nil {
		t.Fatal("an attempt cookie a sibling subdomain could set started a session")
	}

	// The genuine cookie, under its own name, still works: the code is
	// unspent because the refusal never reached the provider.
	resp = f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(state)+"&code="+fakeCode, attempt)
	if got := signInError(t, resp); got != "" {
		t.Fatalf("the genuine attempt failed with %q", got)
	}
}

// Signed is not enough: the start time is under the MAC, so an attempt the
// browser kept past its Max-Age is still refused.
func TestOIDCCallbackRefusesAnExpiredAttempt(t *testing.T) {
	p := &fakeOIDC{signup: true, identity: directory.Identity{Provider: "fake", Subject: "s", Email: "x@example.test"}}
	f := newOIDCFixture(t, p)
	old := signedAttempt(t, map[string]any{
		"p": "fake", "t": time.Now().Add(-11 * time.Minute).Unix(), "s": "st", "n": "nonce", "v": "verifier",
	})
	resp := f.navigate("/v1/auth/oidc/callback?state=st&code="+fakeCode, old)
	if got := signInError(t, resp); got != "oidc_expired" {
		t.Fatalf("sign_in_error = %q, want oidc_expired", got)
	}
}

// A second /start — another tab — leaves the first attempt intact, so either
// callback can still finish. Only the oldest is dropped past the bound.
func TestOIDCSecondStartKeepsTheFirstAttempt(t *testing.T) {
	p := &fakeOIDC{signup: true, identity: directory.Identity{
		Provider: "fake", Subject: "sub-tabs", Email: "tabs@example.test", DisplayName: "Tabs",
	}}
	f := newOIDCFixture(t, p)

	first, firstState := beginOIDC(t, f, p)
	second, _, resp := beginOIDCResp(t, f, p, first)
	if resp.cleared(first.Name) {
		t.Fatal("the second start cleared the first attempt")
	}
	if first.Name == second.Name {
		t.Fatal("two attempts share a cookie name")
	}

	// The first tab comes back while both are held.
	cb := f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(firstState)+"&code="+fakeCode, first, second)
	if got := signInError(t, cb); got != "" {
		t.Fatalf("the first attempt failed with %q after a second was started", got)
	}
	if cb.cleared(second.Name) {
		t.Error("finishing one attempt cleared another")
	}

	// The bound: holding three, a fourth start drops the oldest and only it.
	// Started a second apart, because the order is by start time.
	time.Sleep(1100 * time.Millisecond)
	third, _, _ := beginOIDCResp(t, f, p, second)
	time.Sleep(1100 * time.Millisecond)
	_, _, resp = beginOIDCResp(t, f, p, first, second, third)
	if !resp.cleared(first.Name) {
		t.Error("a start past the bound did not drop the oldest attempt")
	}
	if resp.cleared(second.Name) || resp.cleared(third.Name) {
		t.Error("a start past the bound dropped more than the oldest attempt")
	}
}

// The RFC 9207 iss parameter reaches the provider, which is where it is
// compared (internal/directory tests the comparison against Keycloak).
func TestOIDCCallbackPassesTheIssParameter(t *testing.T) {
	p := &fakeOIDC{signup: true, identity: directory.Identity{Provider: "fake", Subject: "s", Email: "iss@example.test"}}
	f := newOIDCFixture(t, p)
	attempt, state := beginOIDC(t, f, p)
	f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(state)+"&code="+fakeCode+
		"&iss="+url.QueryEscape("https://idp.example.test"), attempt)
	if p.callback.Issuer != "https://idp.example.test" || p.callback.Code != fakeCode {
		t.Fatalf("provider was handed %+v", p.callback)
	}
}

// A link made under one issuer is not reachable through another that took
// over the provider's name.
func TestOIDCRefusesALinkMadeUnderAnotherIssuer(t *testing.T) {
	p := &fakeOIDC{signup: true, identity: directory.Identity{
		Provider: "fake", Subject: "sub-shared", Email: "new-person@example.test", Issuer: "https://new.example.test",
	}}
	f := newOIDCFixture(t, p)
	err := db.WithoutPrincipal(t.Context(), f.ownerPool, func(tx pgx.Tx) error {
		store := identity.NewStore()
		user, err := store.CreateUser(t.Context(), tx, "old-person@example.test", "Old Person")
		if err != nil {
			return err
		}
		if _, err := store.CreatePrincipalForUser(t.Context(), tx, user.ID); err != nil {
			return err
		}
		return store.LinkDirectoryIdentity(t.Context(), tx, "fake", "https://old.example.test", "sub-shared", user.ID)
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt, state := beginOIDC(t, f, p)
	resp := f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(state)+"&code="+fakeCode, attempt)
	if got := signInError(t, resp); got != "oidc_failed" {
		t.Fatalf("sign_in_error = %q, want oidc_failed", got)
	}
	if resp.cookie("cc_session") != nil {
		t.Fatal("the new issuer's subject was signed in as the old issuer's user")
	}
}

// Signing in through OIDC in a browser that already holds a session ends
// that session rather than leaving it valid beside the new one.
func TestOIDCSignInEndsThePreviousSession(t *testing.T) {
	p := &fakeOIDC{signup: true, identity: directory.Identity{
		Provider: "fake", Subject: "sub-again", Email: "again@example.test", DisplayName: "Again",
	}}
	f := newOIDCFixture(t, p)

	attempt, state := beginOIDC(t, f, p)
	first := f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(state)+"&code="+fakeCode, attempt).cookie("cc_session")
	if first == nil {
		t.Fatal("no first session")
	}

	attempt, state = beginOIDC(t, f, p)
	second := f.navigate("/v1/auth/oidc/callback?state="+url.QueryEscape(state)+"&code="+fakeCode, attempt, first).cookie("cc_session")
	if second == nil || second.Value == first.Value {
		t.Fatal("no new session")
	}
	if status := f.withCookie(http.MethodGet, "/v1/me", first, true); status != http.StatusUnauthorized {
		t.Fatalf("the replaced session still authenticates: %d", status)
	}
	if status := f.withCookie(http.MethodGet, "/v1/me", second, true); status != http.StatusOK {
		t.Fatalf("the new session does not authenticate: %d", status)
	}
}

// Password sign-in in a browser that already holds a session ends the old
// one, as OIDC sign-in does — and leaves a PAT in the same cookie alone.
func TestPasswordSignInEndsThePreviousSession(t *testing.T) {
	f := newFixture(t)
	const (
		email    = "twice@example.test"
		password = "correct horse battery staple"
	)
	pat := f.newUserWithToken(email)
	f.setLocalPassword(email, password)
	body := `{"email":"` + email + `","password":"` + password + `"}`

	signIn := func(cookie *http.Cookie) *http.Cookie {
		t.Helper()
		req := f.request(http.MethodPost, "/v1/auth/password", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "cc-web")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp := f.send(req)
		if resp.status != http.StatusOK || resp.sessionCookie() == nil {
			t.Fatalf("sign-in: status %d, body %s", resp.status, resp.body)
		}
		return resp.sessionCookie()
	}

	first := signIn(nil)
	second := signIn(first)
	if status := f.withCookie(http.MethodGet, "/v1/me", first, true); status != http.StatusUnauthorized {
		t.Fatalf("the replaced session still authenticates: %d", status)
	}
	if status := f.withCookie(http.MethodGet, "/v1/me", second, true); status != http.StatusOK {
		t.Fatalf("the new session does not authenticate: %d", status)
	}

	// Only a session is ended. A PAT someone put in the cookie by hand is
	// not this flow's to revoke.
	patCookie := &http.Cookie{Name: "cc_session", Value: pat} //nolint:gosec // G124: a request cookie in a test
	signIn(patCookie)
	if status := f.withCookie(http.MethodGet, "/v1/me", patCookie, true); status != http.StatusOK {
		t.Fatalf("signing in revoked a PAT: %d", status)
	}
}
