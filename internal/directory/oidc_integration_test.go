package directory_test

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/koduj-dev/context-commander/internal/directory"
)

// The realm these tests rely on is deploy/oidc/context-commander-realm.json —
// the same file `docker compose --profile oidc up` imports, for the same
// reason the LDAP tests share deploy/ldap: a fixture that works by hand works
// here, and one someone breaks breaks both.
const (
	keycloakImage = "quay.io/keycloak/keycloak:26.8.0"
	oidcRealm     = "context-commander"
	oidcClientID  = "context-commander"
	// Committed in the realm file; see deploy/oidc/README.md.
	oidcSecret   = "oidc-dev-client-secret" //nolint:gosec // G101: test fixture, not a credential
	oidcPassword = "oidc-dev-password"      //nolint:gosec // G101: test fixture, not a credential
	// Registered in the realm. Nothing listens there: the provider redirects
	// the browser to it with the code, and the test reads the code out of
	// that redirect instead of following it.
	oidcRedirect = "http://localhost:12001/v1/auth/oidc/callback" //nolint:gosec // G101: a URL, not a credential
)

// Keycloak is started on first use and shared, not started by TestMain: it
// takes tens of seconds, and the LDAP tests in this package have no reason to
// wait for it.
var (
	keycloakOnce      sync.Once
	keycloakIssuer    string
	keycloakErr       error
	keycloakTerminate = func() {}
)

// stopKeycloak is called by TestMain after the run.
func stopKeycloak() { keycloakTerminate() }

func issuer(t *testing.T) string {
	t.Helper()
	keycloakOnce.Do(func() {
		keycloakIssuer, keycloakTerminate, keycloakErr = startKeycloak()
	})
	if keycloakErr != nil {
		// Not a skip, for the reason TestMain gives for slapd.
		t.Fatalf("start keycloak container: %v", keycloakErr)
	}
	return keycloakIssuer
}

func startKeycloak() (string, func(), error) {
	ctx := context.Background()
	realm, err := filepath.Abs(filepath.Join("..", "..", "deploy", "oidc", "context-commander-realm.json"))
	if err != nil {
		return "", nil, fmt.Errorf("resolve realm file: %w", err)
	}
	discovery := "/realms/" + oidcRealm + "/.well-known/openid-configuration"

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        keycloakImage,
			ExposedPorts: []string{"8080/tcp"},
			Env: map[string]string{ //nolint:gosec // G101: disposable container
				"KC_BOOTSTRAP_ADMIN_USERNAME": "admin",
				"KC_BOOTSTRAP_ADMIN_PASSWORD": "keycloak_test_only",
			},
			Cmd: []string{"start-dev", "--import-realm"},
			Files: []testcontainers.ContainerFile{{
				HostFilePath: realm,
				// The name must match the realm inside it, or Keycloak refuses
				// to start (deploy/oidc/README.md).
				ContainerFilePath: "/opt/keycloak/data/import/context-commander-realm.json",
				FileMode:          0o644,
			}},
			// The realm's own discovery document over the mapped port: it
			// exists only once the import has run, which a port check or the
			// master realm would not prove.
			WaitingFor: wait.ForHTTP(discovery).WithPort("8080/tcp").
				WithStatusCodeMatcher(func(s int) bool { return s == http.StatusOK }).
				WithStartupTimeout(3 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		return "", nil, err
	}
	stop := func() {
		if err := container.Terminate(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "terminate keycloak container: %v\n", err)
		}
	}
	endpoint, err := container.PortEndpoint(ctx, "8080/tcp", "http")
	if err != nil {
		stop()
		return "", nil, fmt.Errorf("keycloak endpoint: %w", err)
	}
	// start-dev derives the issuer from the request's host, so every request
	// in these tests must use this exact address — discovery checks that
	// the document's issuer equals the one asked for.
	return endpoint + "/realms/" + oidcRealm, stop, nil
}

func newKeycloakOIDC(t *testing.T, mutate func(*directory.OIDCConfig)) *directory.OIDC {
	t.Helper()
	cfg := directory.OIDCConfig{
		Name:         "keycloak",
		Label:        "Keycloak",
		IssuerURL:    issuer(t),
		ClientID:     oidcClientID,
		ClientSecret: oidcSecret,
		RedirectURL:  oidcRedirect,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	o, err := directory.NewOIDC(cfg)
	if err != nil {
		t.Fatalf("NewOIDC: %v", err)
	}
	return o
}

// kcLoginAction finds Keycloak's login form. Its action carries the session
// and tab ids, HTML-escaped.
var kcLoginAction = regexp.MustCompile(`<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"`)

// signInAtProvider does what a browser does between the redirect out and the
// callback: load the provider's login page, submit the password, and stop at
// the redirect back to this server. It returns that redirect's query.
func signInAtProvider(t *testing.T, authURL, username string) url.Values {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		// Follow Keycloak's own redirects, but not the one to our callback,
		// which has no listener.
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if req.URL.String() != "" && req.URL.Host == mustHost(t, oidcRedirect) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	resp, err := client.Get(authURL) //nolint:noctx // test helper
	if err != nil {
		t.Fatalf("load login page: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	m := kcLoginAction.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no login form at %s (status %d)", authURL, resp.StatusCode)
	}
	action := html.UnescapeString(string(m[1]))

	resp, err = client.PostForm(action, url.Values{ //nolint:noctx // test helper
		"username":     {username},
		"password":     {oidcPassword},
		"credentialId": {""},
	})
	if err != nil {
		t.Fatalf("submit login: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login for %s: want a redirect back to the callback, got %d", username, resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse callback redirect: %v", err)
	}
	return loc.Query()
}

func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// begin starts an attempt and signs in at the provider, returning the code.
func begin(t *testing.T, o *directory.OIDC, username string) (string, directory.AuthRequest) {
	t.Helper()
	req := directory.NewAuthRequest()
	authURL, err := o.AuthCodeURL(req)
	if err != nil {
		t.Fatalf("AuthCodeURL: %v", err)
	}
	q := signInAtProvider(t, authURL, username)
	if !req.StateMatches(q.Get("state")) {
		t.Fatalf("callback state %q does not match the attempt's", q.Get("state"))
	}
	code := q.Get("code")
	if code == "" {
		t.Fatalf("callback carried no code: %v", q)
	}
	return code, req
}

func TestOIDC_SignIn(t *testing.T) {
	o := newKeycloakOIDC(t, nil)
	code, req := begin(t, o, "alice")

	id, err := o.Exchange(context.Background(), code, req)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if id.Provider != "keycloak" {
		t.Errorf("provider = %q, want keycloak", id.Provider)
	}
	if id.Subject == "" || id.Subject == "alice" || id.Subject == "alice@example.test" {
		// The subject is Keycloak's user id, never the login or the address.
		t.Errorf("subject = %q, want the provider's opaque user id", id.Subject)
	}
	if id.Email != "alice@example.test" {
		t.Errorf("email = %q", id.Email)
	}
	if id.DisplayName != "Alice Ashworth" {
		t.Errorf("display name = %q", id.DisplayName)
	}
}

func TestOIDC_SubjectIsStableAcrossSignIns(t *testing.T) {
	o := newKeycloakOIDC(t, nil)
	first, req1 := begin(t, o, "bob")
	a, err := o.Exchange(context.Background(), first, req1)
	if err != nil {
		t.Fatal(err)
	}
	second, req2 := begin(t, o, "bob")
	b, err := o.Exchange(context.Background(), second, req2)
	if err != nil {
		t.Fatal(err)
	}
	if a.Subject != b.Subject {
		t.Fatalf("subject changed between sign-ins: %q then %q", a.Subject, b.Subject)
	}
}

func TestOIDC_RejectsAWrongNonce(t *testing.T) {
	o := newKeycloakOIDC(t, nil)
	code, req := begin(t, o, "alice")
	req.Nonce = directory.NewAuthRequest().Nonce
	_, err := o.Exchange(context.Background(), code, req)
	if !errors.Is(err, directory.ErrSignInRejected) {
		t.Fatalf("want ErrSignInRejected, got %v", err)
	}
	// The code and verifier are genuine, so the token endpoint answers; the
	// refusal must come from our own nonce check, not from anything earlier.
	if !strings.Contains(err.Error(), "nonce does not match") {
		t.Fatalf("rejected for the wrong reason: %v", err)
	}
}

func TestOIDC_RejectsAWrongPKCEVerifier(t *testing.T) {
	o := newKeycloakOIDC(t, nil)
	code, req := begin(t, o, "alice")
	// A code intercepted on its way back — from a log, a referrer, a proxy —
	// is useless without the verifier, which never travels to the provider's
	// authorization endpoint or in any URL; it sits in the HttpOnly cc_oidc
	// cookie. (A thief of that browser's cookies gets both, which is why the
	// redirect must be https off loopback.)
	req.Verifier = directory.NewAuthRequest().Verifier
	_, err := o.Exchange(context.Background(), code, req)
	if !errors.Is(err, directory.ErrSignInRejected) {
		t.Fatalf("want ErrSignInRejected, got %v", err)
	}
	// The provider must be the one refusing — proof it enforces the
	// challenge rather than our code never sending the verifier.
	if !strings.Contains(err.Error(), "token endpoint") {
		t.Fatalf("rejected for the wrong reason: %v", err)
	}
}

func TestOIDC_RejectsAReusedCode(t *testing.T) {
	o := newKeycloakOIDC(t, nil)
	code, req := begin(t, o, "alice")
	if _, err := o.Exchange(context.Background(), code, req); err != nil {
		t.Fatal(err)
	}
	_, err := o.Exchange(context.Background(), code, req)
	if !errors.Is(err, directory.ErrSignInRejected) {
		t.Fatalf("second redemption: want ErrSignInRejected, got %v", err)
	}
	if !strings.Contains(err.Error(), "token endpoint") {
		t.Fatalf("rejected for the wrong reason: %v", err)
	}
}

func TestOIDC_RefusesAnUnverifiedEmail(t *testing.T) {
	o := newKeycloakOIDC(t, nil)
	code, req := begin(t, o, "erin")
	_, err := o.Exchange(context.Background(), code, req)
	if !errors.Is(err, directory.ErrEmailNotVerified) {
		t.Fatalf("want ErrEmailNotVerified, got %v", err)
	}
}

func TestOIDC_RefusesAnIDTokenWithoutEmail(t *testing.T) {
	// Not requesting the email scope is how this realm produces an ID token
	// with no address — see deploy/oidc/README.md for why no seeded user
	// lacks one.
	o := newKeycloakOIDC(t, func(c *directory.OIDCConfig) { c.Scopes = []string{"openid", "profile"} })
	code, req := begin(t, o, "alice")
	_, err := o.Exchange(context.Background(), code, req)
	if !errors.Is(err, directory.ErrNoEmail) {
		t.Fatalf("want ErrNoEmail, got %v", err)
	}
}

func TestOIDC_UnreachableProviderIsUnavailable(t *testing.T) {
	// No container: nothing listens on port 1. This must read as an outage,
	// not as a failed sign-in.
	o, err := directory.NewOIDC(directory.OIDCConfig{
		IssuerURL:    "http://127.0.0.1:1/realms/nowhere",
		ClientID:     oidcClientID,
		ClientSecret: oidcSecret,
		RedirectURL:  oidcRedirect,
		Timeout:      2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = o.AuthCodeURL(directory.NewAuthRequest())
	if !errors.Is(err, directory.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestNewOIDC_ValidatesAtStartup(t *testing.T) {
	ok := directory.OIDCConfig{
		IssuerURL:    "https://id.example.test/realms/x",
		ClientID:     "cc",
		ClientSecret: "s",
		RedirectURL:  "https://cc.example.test/v1/auth/oidc/callback",
	}
	cases := map[string]func(*directory.OIDCConfig){
		"no issuer":          func(c *directory.OIDCConfig) { c.IssuerURL = "" },
		"relative issuer":    func(c *directory.OIDCConfig) { c.IssuerURL = "id.example.test" },
		"no client id":       func(c *directory.OIDCConfig) { c.ClientID = "" },
		"no client secret":   func(c *directory.OIDCConfig) { c.ClientSecret = "" },
		"no redirect":        func(c *directory.OIDCConfig) { c.RedirectURL = "" },
		"name with a slash":  func(c *directory.OIDCConfig) { c.Name = "a/b" },
		"scopes sans openid": func(c *directory.OIDCConfig) { c.Scopes = []string{"email"} },
		// Cleartext off this host: the signing keys and the code would travel
		// past anyone on the path.
		"http issuer off-host":   func(c *directory.OIDCConfig) { c.IssuerURL = "http://id.example.test/realms/x" },
		"http redirect off-host": func(c *directory.OIDCConfig) { c.RedirectURL = "http://cc.example.test/v1/auth/oidc/callback" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := ok
			mutate(&cfg)
			if _, err := directory.NewOIDC(cfg); err == nil {
				t.Fatal("want a configuration error")
			}
		})
	}
	if _, err := directory.NewOIDC(ok); err != nil {
		t.Fatalf("valid config refused: %v", err)
	}
	// The dev Keycloak speaks http on loopback, and must keep working.
	for _, host := range []string{"127.0.0.1:18081", "localhost:18081", "[::1]:18081"} {
		cfg := ok
		cfg.IssuerURL = "http://" + host + "/realms/context-commander"
		cfg.RedirectURL = "http://" + host + "/v1/auth/oidc/callback"
		if _, err := directory.NewOIDC(cfg); err != nil {
			t.Errorf("loopback http %s refused: %v", host, err)
		}
	}
}

func TestOIDC_AllowedDomainsApplyToTheVerifiedAddress(t *testing.T) {
	refused := newKeycloakOIDC(t, func(c *directory.OIDCConfig) { c.AllowedDomains = []string{"elsewhere.test"} })
	code, req := begin(t, refused, "alice")
	if _, err := refused.Exchange(context.Background(), code, req); !errors.Is(err, directory.ErrDomainNotAllowed) {
		t.Fatalf("alice@example.test with only elsewhere.test allowed: want ErrDomainNotAllowed, got %v", err)
	}

	admitted := newKeycloakOIDC(t, func(c *directory.OIDCConfig) { c.AllowedDomains = []string{"EXAMPLE.test"} })
	code, req = begin(t, admitted, "alice")
	if _, err := admitted.Exchange(context.Background(), code, req); err != nil {
		t.Fatalf("alice@example.test with example.test allowed: %v", err)
	}

	// The verification check comes first: erin's address is in an allowed
	// domain but unverified, and must be refused for that, not admitted.
	code, req = begin(t, admitted, "erin")
	if _, err := admitted.Exchange(context.Background(), code, req); !errors.Is(err, directory.ErrEmailNotVerified) {
		t.Fatalf("erin: want ErrEmailNotVerified, got %v", err)
	}
}
