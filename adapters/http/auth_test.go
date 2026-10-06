package http_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	cchttp "github.com/koduj-dev/context-commander/adapters/http"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/firstrun"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/ratelimit"
)

// post sends a JSON request with the header the browser endpoints require,
// so a test has to opt OUT of being a browser rather than remember to opt in.
func (f *fixture) post(path, body string) *http.Response {
	f.t.Helper()
	req, err := http.NewRequest(http.MethodPost, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		f.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "cc-web")
	resp, err := f.server.Client().Do(req)
	if err != nil {
		f.t.Fatalf("%s %s: %v", http.MethodPost, path, err)
	}
	return resp
}

func sessionCookieFrom(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == "cc_session" {
			return c
		}
	}
	return nil
}

func TestSetupCreatesTheFirstAdministrator(t *testing.T) {
	code, err := firstrun.NewCode()
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}
	f := newFixtureWithDeps(t, nil, ratelimit.Limits{}, func(d *cchttp.Deps) { d.FirstRun = code })

	// An empty installation says so. This is the bit the UI routes on.
	resp := f.do(http.MethodGet, "/v1/setup/status", "", nil)
	var status struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	decodeInto(t, resp, &status)
	if !status.NeedsSetup {
		t.Fatal("a fresh installation did not report that it needs setup")
	}

	body := `{"code":"` + code.String() + `","email":"first@example.test","display_name":"First Admin","password":"correct horse battery staple"}`
	resp = f.post("/v1/setup", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: status %d, body %s", resp.StatusCode, readBody(t, resp))
	}
	// Setup must leave the new administrator signed in — sending them to a
	// login form immediately after creating their account is a round trip
	// for no reason.
	if sessionCookieFrom(t, resp) == nil {
		t.Error("setup did not set a session cookie")
	}

	// The three things it had to create, checked where they live rather than
	// through the API that would also pass if only one of them existed.
	var isAdmin, hasPassword bool
	err = db.WithoutPrincipal(t.Context(), f.ownerPool, func(tx pgx.Tx) error {
		user, err := identity.NewStore().GetUserByEmail(t.Context(), tx, "first@example.test")
		if err != nil {
			return err
		}
		if _, err := identity.NewStore().PrincipalForUser(t.Context(), tx, user.ID); err != nil {
			return err
		}
		if _, err := identity.NewStore().GetPasswordHash(t.Context(), tx, user.ID); err == nil {
			hasPassword = true
		}
		return tx.QueryRow(t.Context(),
			`SELECT EXISTS (SELECT 1 FROM installation_admins WHERE user_id = $1)`, user.ID).Scan(&isAdmin)
	})
	if err != nil {
		t.Fatalf("inspect the created administrator: %v", err)
	}
	if !isAdmin {
		t.Error("the first user was not granted installation admin")
	}
	if !hasPassword {
		t.Error("the first user has no local password")
	}

	// And now the endpoint is closed, by two independent checks: the code is
	// spent, and the installation has a user.
	resp = f.do(http.MethodGet, "/v1/setup/status", "", nil)
	decodeInto(t, resp, &status)
	if status.NeedsSetup {
		t.Error("setup still reports as needed after it completed")
	}
	resp = f.post("/v1/setup", body)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("replayed setup: status %d, want 409", resp.StatusCode)
	}
}

func TestSetupRefusesWithoutTheCode(t *testing.T) {
	code, err := firstrun.NewCode()
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}
	f := newFixtureWithDeps(t, nil, ratelimit.Limits{}, func(d *cchttp.Deps) { d.FirstRun = code })

	for name, attempt := range map[string]string{
		"no code":    `{"code":"","email":"a@example.test","display_name":"A","password":"correct horse battery staple"}`,
		"wrong code": `{"code":"00000000000000000000000000000000","email":"a@example.test","display_name":"A","password":"correct horse battery staple"}`,
	} {
		resp := f.post("/v1/setup", attempt)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, resp.StatusCode)
		}
	}

	// A short password is refused before the code is spent, or a typo in the
	// password would burn the one chance to use it.
	resp := f.post("/v1/setup", `{"code":"`+code.String()+`","email":"a@example.test","display_name":"A","password":"short"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("weak password: status %d, want 400", resp.StatusCode)
	}
	resp = f.post("/v1/setup", `{"code":"`+code.String()+`","email":"a@example.test","display_name":"A","password":"correct horse battery staple"}`)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("after a refused weak password the code no longer works: status %d, body %s",
			resp.StatusCode, readBody(t, resp))
	}
}

func TestSetupIsClosedWhenNoCodeWasMinted(t *testing.T) {
	// What every normal start gets: FirstRun is nil, and a nil code accepts
	// nothing. The endpoint must be shut rather than open.
	f := newFixture(t)
	f.newUserWithToken("existing@example.test")

	resp := f.post("/v1/setup", `{"code":"","email":"a@example.test","display_name":"A","password":"correct horse battery staple"}`)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status %d, want 409 (the installation already has users)", resp.StatusCode)
	}
}

func TestPasswordSignIn(t *testing.T) {
	f := newFixture(t)
	const (
		email    = "signin@example.test"
		password = "correct horse battery staple"
	)
	f.newUserWithToken(email)
	f.setLocalPassword(email, password)

	t.Run("a session cookie is issued and authenticates", func(t *testing.T) {
		resp := f.post("/v1/auth/password", `{"email":"`+email+`","password":"`+password+`"}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("sign-in: status %d, body %s", resp.StatusCode, readBody(t, resp))
		}
		cookie := sessionCookieFrom(t, resp)
		if cookie == nil {
			t.Fatal("no session cookie was set")
		}
		if !cookie.HttpOnly {
			t.Error("the session cookie is not HttpOnly — the SPA can read it, which is the thing the cookie exists to prevent")
		}
		if cookie.SameSite != http.SameSiteLaxMode {
			t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
		}
		// The token must not also come back in the body, which would hand it
		// straight to the JavaScript the cookie keeps it from.
		if strings.Contains(readBody(t, resp), cookie.Value) {
			t.Error("the response body contains the session token")
		}

		// The cookie works, and only with the CSRF header.
		if code := f.withCookie(http.MethodGet, "/v1/me", cookie, true); code != http.StatusOK {
			t.Errorf("GET /v1/me with cookie: status %d, want 200", code)
		}
		if code := f.withCookie(http.MethodGet, "/v1/me", cookie, false); code != http.StatusForbidden {
			t.Errorf("GET /v1/me with cookie and no CSRF header: status %d, want 403", code)
		}
	})

	t.Run("every failure reads identically", func(t *testing.T) {
		var messages []string
		for _, attempt := range []string{
			`{"email":"` + email + `","password":"wrong password entirely"}`,
			`{"email":"nobody@example.test","password":"wrong password entirely"}`,
			`{"email":"","password":""}`,
		} {
			resp := f.post("/v1/auth/password", attempt)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status %d, want 401 for %s", resp.StatusCode, attempt)
			}
			messages = append(messages, readBody(t, resp))
		}
		for _, m := range messages[1:] {
			if m != messages[0] {
				t.Errorf("two failures are distinguishable:\n  %s\n  %s", messages[0], m)
			}
		}
	})

	t.Run("sign-in needs the CSRF header too", func(t *testing.T) {
		// Otherwise a cross-site form can sign someone into an account the
		// attacker controls, which is a quieter problem than it sounds.
		req, err := http.NewRequest(http.MethodPost, f.server.URL+"/v1/auth/password",
			strings.NewReader(`{"email":"`+email+`","password":"`+password+`"}`))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := f.server.Client().Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status %d, want 403", resp.StatusCode)
		}
	})
}

func TestSignOutRevokesTheSession(t *testing.T) {
	f := newFixture(t)
	const (
		email    = "signout@example.test"
		password = "correct horse battery staple"
	)
	f.newUserWithToken(email)
	f.setLocalPassword(email, password)

	resp := f.post("/v1/auth/password", `{"email":"`+email+`","password":"`+password+`"}`)
	cookie := sessionCookieFrom(t, resp)
	if cookie == nil {
		t.Fatal("no session cookie")
	}

	req, err := http.NewRequest(http.MethodDelete, f.server.URL+"/v1/auth/session", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(cookie)
	req.Header.Set("X-Requested-With", "cc-web")
	resp, err = f.server.Client().Do(req)
	if err != nil {
		t.Fatalf("sign out: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("sign out: status %d", resp.StatusCode)
	}

	// The test that matters: the TOKEN is dead, not merely forgotten by this
	// client. Presented as a bearer, bypassing the cookie entirely.
	if resp := f.do(http.MethodGet, "/v1/me", cookie.Value, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("the revoked session token still authenticates: status %d", resp.StatusCode)
	}
	// Signing out again is the same end state, not an error.
	resp, err = f.server.Client().Do(req)
	if err != nil {
		t.Fatalf("sign out twice: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("sign out twice: status %d, want 204", resp.StatusCode)
	}
}

func TestSignInRateLimitIsPerIdentifier(t *testing.T) {
	f := newFixtureWithDeps(t, nil, ratelimit.Limits{PerSignIn: 0.0001, SignInBurst: 2}, nil)
	f.newUserWithToken("limited@example.test")

	attempt := func(email string) int {
		resp := f.post("/v1/auth/password", `{"email":"`+email+`","password":"wrong password entirely"}`)
		return resp.StatusCode
	}

	for i := range 2 {
		if code := attempt("limited@example.test"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401", i+1, code)
		}
	}
	if code := attempt("limited@example.test"); code != http.StatusTooManyRequests {
		t.Errorf("the third attempt: status %d, want 429", code)
	}
	// One address must not lock out another, or anyone can lock a colleague
	// out by guessing at their address.
	if code := attempt("someone-else@example.test"); code != http.StatusUnauthorized {
		t.Errorf("a different address: status %d, want 401", code)
	}
}

func TestAuthMethodsNamesWhatExists(t *testing.T) {
	f := newFixture(t)
	resp := f.do(http.MethodGet, "/v1/auth/methods", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var got struct {
		Methods []struct {
			Kind string `json:"kind"`
		} `json:"methods"`
	}
	decodeInto(t, resp, &got)
	kinds := map[string]bool{}
	for _, m := range got.Methods {
		kinds[m.Kind] = true
	}
	// The sign-in screen renders this list and nothing else, so a method
	// missing here is a method the UI stops offering.
	for _, want := range []string{"password", "token"} {
		if !kinds[want] {
			t.Errorf("%q is not among the offered methods: %v", want, kinds)
		}
	}
}

// setLocalPassword gives an existing user a password, through the store, so
// this helper fails if the schema and the domain code stop agreeing.
func (f *fixture) setLocalPassword(email, password string) {
	f.t.Helper()
	hash, err := identity.HashPassword(password)
	if err != nil {
		f.t.Fatalf("HashPassword: %v", err)
	}
	err = db.WithoutPrincipal(f.t.Context(), f.ownerPool, func(tx pgx.Tx) error {
		user, err := identity.NewStore().GetUserByEmail(f.t.Context(), tx, email)
		if err != nil {
			return err
		}
		return identity.NewStore().SetPassword(f.t.Context(), tx, user.ID, hash)
	})
	if err != nil {
		f.t.Fatalf("set password for %s: %v", email, err)
	}
}

// withCookie issues a request authenticated by a cookie, optionally without
// the CSRF header, and returns the status.
func (f *fixture) withCookie(method, path string, cookie *http.Cookie, withCSRF bool) int {
	f.t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, nil)
	if err != nil {
		f.t.Fatalf("new request: %v", err)
	}
	req.AddCookie(cookie)
	if withCSRF {
		req.Header.Set("X-Requested-With", "cc-web")
	}
	resp, err := f.server.Client().Do(req)
	if err != nil {
		f.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func decodeInto(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
