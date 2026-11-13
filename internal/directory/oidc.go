package directory

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	// ErrSignInRejected means the provider did not confirm the sign-in: the
	// code was refused, the ID token failed verification, or its nonce did
	// not match the one this server issued. Never a transport problem — that
	// is ErrUnavailable — and never something to retry automatically.
	ErrSignInRejected = errors.New("directory: provider did not confirm the sign-in")

	// ErrEmailNotVerified means the provider released an address it has not
	// verified. Refused rather than provisioned, because provisioning links
	// to an existing local account by email (identity.ProvisionDirectoryUser,
	// case 2): accepting an unverified address would let anyone who can type
	// a colleague's address into their profile at the provider sign in as
	// that colleague here.
	ErrEmailNotVerified = errors.New("directory: provider has not verified the email address")

	// ErrDomainNotAllowed means the address is verified but neither its
	// domain nor the address itself is one this installation accepts
	// (docs/adr/0020, docs/adr/0024). Returned by the caller that applies
	// the allowlist, not by Exchange.
	ErrDomainNotAllowed = errors.New("directory: email domain is not allowed to sign in")
)

// OIDCConfig describes one OpenID Connect provider. Discovery
// (`.well-known/openid-configuration`) supplies the endpoints and keys, so the
// issuer is the only address an operator gives (docs/adr/0019).
type OIDCConfig struct {
	// Name is the provider key stored in user_identities and used in the
	// sign-in URL. "oidc" unless an installation has more than one.
	Name string

	// Label is what the sign-in button says — "Keycloak", "Google", the
	// company's own name for its single sign-on.
	Label string

	// IssuerURL is the provider's issuer exactly as its discovery document
	// states it. go-oidc refuses a document whose issuer differs, which is
	// the check that stops one tenant's tokens being accepted for another.
	IssuerURL string

	// ClientID and ClientSecret are this installation's registration at the
	// provider. A confidential client: the secret never reaches a browser.
	ClientID     string
	ClientSecret string

	// RedirectURL is this server's callback, exactly as registered at the
	// provider — /v1/auth/oidc/callback on the public address. Not derived
	// from the request: behind a reverse proxy the request's own host is
	// whatever the proxy chose, and a provider matches redirect URIs byte for
	// byte.
	RedirectURL string

	// Scopes requested. Default openid, profile, email. Asking for less is
	// legitimate (a provider that releases email under another scope) but an
	// ID token without an address cannot be provisioned — see ErrNoEmail.
	Scopes []string

	// CACertFile is a PEM bundle to verify the provider's certificate
	// against, for a provider behind an internal CA. Empty uses the system
	// pool. As with LDAP there is no way to skip verification: the keys that
	// sign every ID token arrive over this connection.
	CACertFile string

	// Timeout bounds each request to the provider — discovery, key fetch,
	// code exchange.
	Timeout time.Duration

	// AllowSignup lets a first sign-in create a local account. Off by
	// default: an issuer like Google authenticates anyone on the internet,
	// and creating accounts for all of them is open self-registration. Off,
	// a person still gets in through an account that is already linked or
	// one an administrator created under the same address (docs/adr/0020).
	AllowSignup bool

	// AllowedDomains, when not empty, restricts every sign-in through this
	// provider to verified addresses in these domains — checked on each
	// sign-in, not only the first, so narrowing the list takes effect for
	// people already linked. Exact match on the part after @, no
	// subdomains: "example.com" does not admit "mail.example.com".
	// An administrator's rules (internal/signin) extend the same list; the
	// caller applies both, through Admission.
	AllowedDomains []string

	// IgnoreGoogleHostedDomain turns off the Workspace check for a Google
	// issuer. On by default there: Google keeps a personal account verified
	// under a work address after its owner leaves the Workspace, so for a
	// Google issuer a listed domain admits only accounts whose `hd` claim is
	// also listed (docs/adr/0024). Meaningless for any other issuer.
	IgnoreGoogleHostedDomain bool

	// EntraVerifiedDomain accepts Microsoft Entra ID's `xms_edov` claim in
	// place of `email_verified`, which Entra never sends. True means the
	// address's domain is verified by the tenant the account lives in — the
	// thing a guest whose home tenant merely typed someone else's address
	// cannot have (docs/adr/0023). Refused at startup for any issuer that is
	// not Entra's, so it cannot become a way to switch the check off for
	// every provider. The app registration must release the claim: without
	// it every address reads as unverified, which is the safe failure.
	EntraVerifiedDomain bool
}

const (
	defaultOIDCName    = "oidc"
	defaultOIDCLabel   = "Single sign-on"
	defaultOIDCTimeout = 10 * time.Second
)

// OIDC signs people in through one OpenID Connect provider, using the
// authorization code flow with PKCE.
type OIDC struct {
	cfg    OIDCConfig
	client *http.Client

	// Discovery happens on first use rather than at startup, and is retried
	// after a failure: a provider that is down when this server starts must
	// cost a sign-in method for as long as it is down, not the whole server.
	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
	// requireIss is the provider advertising RFC 9207: from then on an
	// authorization response without `iss` is refused, not excused.
	requireIss bool

	// A failed discovery is remembered briefly. Discovery runs under mu, and
	// without this every anonymous /start during an outage would queue
	// behind the lock for a full client timeout and then make its own
	// outbound request — a slow provider would be a lever for tying up this
	// server's goroutines. now is replaceable for tests.
	failedAt  time.Time
	failedErr error
	now       func() time.Time
}

// discoveryRetryAfter is how long a failed discovery is answered from memory
// before the provider is asked again.
const discoveryRetryAfter = 5 * time.Second

// NewOIDC validates the configuration. It makes no network call — see the
// comment on OIDC.mu — so a typo in a flag still stops the server, but an
// unreachable provider does not.
func NewOIDC(cfg OIDCConfig) (*OIDC, error) {
	if cfg.IssuerURL == "" {
		return nil, errors.New("directory: oidc issuer url is required")
	}
	if err := requireHTTPURL("issuer", cfg.IssuerURL); err != nil {
		return nil, err
	}
	if cfg.ClientID == "" {
		return nil, errors.New("directory: oidc client id is required")
	}
	if cfg.ClientSecret == "" {
		return nil, errors.New("directory: oidc client secret is required")
	}
	if cfg.RedirectURL == "" {
		return nil, errors.New("directory: oidc redirect url is required — the callback exactly as registered at the provider")
	}
	if err := requireHTTPURL("redirect", cfg.RedirectURL); err != nil {
		return nil, err
	}

	if cfg.Name == "" {
		cfg.Name = defaultOIDCName
	}
	if strings.ContainsAny(cfg.Name, "/?#") {
		return nil, fmt.Errorf("directory: oidc name %q must not contain / ? or # — it is a URL path segment", cfg.Name)
	}
	if cfg.Label == "" {
		cfg.Label = defaultOIDCLabel
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	if !containsString(cfg.Scopes, oidc.ScopeOpenID) {
		// Without it the provider answers with plain OAuth and no ID token,
		// and the failure would surface on the first sign-in instead of here.
		return nil, errors.New("directory: oidc scopes must include openid")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultOIDCTimeout
	}
	domains := make([]string, 0, len(cfg.AllowedDomains))
	for _, d := range cfg.AllowedDomains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if strings.Contains(d, "@") || strings.HasPrefix(d, ".") {
			return nil, fmt.Errorf("directory: oidc allowed domain %q must be a bare domain like example.com", d)
		}
		domains = append(domains, d)
	}
	cfg.AllowedDomains = domains
	if cfg.EntraVerifiedDomain && !isEntraIssuer(cfg.IssuerURL) {
		return nil, fmt.Errorf("directory: the Entra verified-domain switch is for a Microsoft Entra ID issuer (https://login.microsoftonline.com/<tenant>/v2.0), not %q", cfg.IssuerURL)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.CACertFile != "" {
		pem, err := os.ReadFile(cfg.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("directory: read oidc ca certificate: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("directory: %s holds no PEM certificate", cfg.CACertFile)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}

	return &OIDC{
		cfg:    cfg,
		client: &http.Client{Transport: transport, Timeout: cfg.Timeout},
		now:    time.Now,
	}, nil
}

// requireHTTPURL insists on https, except on a loopback host.
//
// Plain http to the issuer is not a weaker form of the same thing: discovery
// and the signing keys arrive over that connection, so whoever is on the path
// can serve their own keys, mint an ID token for any subject with any
// verified address, and — through provisioning's link-by-email — sign in as
// any local account. Cleartext to the redirect hands the code to the same
// observer, and PKCE does not help there, because the verifier travels in the
// same browser's cookie. Loopback is exempt because the dev Keycloak
// (deploy/oidc) speaks http, and a packet that never leaves the host has no
// path to be on.
func requireHTTPURL(what, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("directory: oidc %s url: %w", what, err)
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("directory: oidc %s url must be an absolute http(s) URL, got %q", what, raw)
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("directory: oidc %s url must be https unless it is on this host, got %q", what, raw)
	}
	return nil
}

// entraHosts are the authorities Microsoft Entra ID issues tokens from: the
// public cloud and the national clouds.
var entraHosts = map[string]bool{
	"login.microsoftonline.com":        true,
	"login.microsoftonline.us":         true,
	"login.chinacloudapi.cn":           true,
	"login.partner.microsoftonline.cn": true,
}

// isEntraIssuer reports whether raw is an Entra issuer. The multi-tenant
// aliases (common, organizations, consumers) need no check of their own:
// their discovery documents name a templated issuer, which go-oidc refuses
// to match, so a host check is the whole test.
func isEntraIssuer(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && entraHosts[strings.ToLower(u.Hostname())]
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// Provider returns the configured name, for user_identities and the sign-in
// URL.
func (o *OIDC) Provider() string { return o.cfg.Name }

// Issuer returns the configured issuer, which every identity this provider
// asserts is stored beside.
func (o *OIDC) Issuer() string { return o.cfg.IssuerURL }

// Label returns what the sign-in button should say.
func (o *OIDC) Label() string { return o.cfg.Label }

// AllowsSignup reports whether a first sign-in may create a local account.
func (o *OIDC) AllowsSignup() bool { return o.cfg.AllowSignup }

// Admission reports the configured domains and whether the Workspace check
// applies.
func (o *OIDC) Admission() Admission {
	return Admission{
		Domains:             append([]string(nil), o.cfg.AllowedDomains...),
		RequireHostedDomain: o.isGoogle() && !o.cfg.IgnoreGoogleHostedDomain,
	}
}

// googleIssuer is the only issuer whose `hd` claim is read. go-oidc also
// accepts tokens naming "accounts.google.com" for it.
const googleIssuer = "https://accounts.google.com"

func (o *OIDC) isGoogle() bool { return o.cfg.IssuerURL == googleIssuer }

// AuthRequest is the per-attempt secret state of one sign-in: the state that
// ties the callback to the browser that started it, the nonce that ties the
// ID token to this attempt, and the PKCE verifier that ties the code to it.
// The caller keeps it between the redirect out and the callback; nothing here
// stores it.
type AuthRequest struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
}

// NewAuthRequest draws fresh values for one attempt. oauth2.GenerateVerifier
// is 32 random bytes, base64url — the same strength serves for all three.
func NewAuthRequest() AuthRequest {
	return AuthRequest{
		State:    oauth2.GenerateVerifier(),
		Nonce:    oauth2.GenerateVerifier(),
		Verifier: oauth2.GenerateVerifier(),
	}
}

// StateMatches compares the state a callback presented with the one this
// attempt was started with, in constant time.
func (a AuthRequest) StateMatches(got string) bool {
	return a.State != "" && subtle.ConstantTimeCompare([]byte(a.State), []byte(got)) == 1
}

// discover returns the provider's endpoints and verifier, fetching them on
// first use.
func (o *OIDC) discover() (*oauth2.Config, *oidc.IDTokenVerifier, bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.oauth != nil {
		return o.oauth, o.verifier, o.requireIss, nil
	}
	if o.failedErr != nil && o.now().Sub(o.failedAt) < discoveryRetryAfter {
		return nil, nil, false, o.failedErr
	}

	// Background, not the request's context: go-oidc keeps this context for
	// every later key-set refresh, and a request context is cancelled the
	// moment its response is written — after which key rotation at the
	// provider would make every sign-in fail until restart.
	ctx := oidc.ClientContext(context.Background(), o.client)
	provider, err := oidc.NewProvider(ctx, o.cfg.IssuerURL)
	if err != nil {
		o.failedErr = fmt.Errorf("%w: oidc discovery at %s: %v", ErrUnavailable, o.cfg.IssuerURL, err) //nolint:errorlint // the cause is context, ErrUnavailable is the class
		o.failedAt = o.now()
		return nil, nil, false, o.failedErr
	}
	var meta providerMetadata
	if err := provider.Claims(&meta); err != nil {
		// NewProvider has parsed this document already, so this is not
		// expected; it is an answer this server cannot use either way.
		return nil, nil, false, fmt.Errorf("%w: oidc discovery at %s: %v", ErrUnavailable, o.cfg.IssuerURL, err) //nolint:errorlint // as above
	}
	o.failedErr = nil
	o.oauth = &oauth2.Config{
		ClientID:     o.cfg.ClientID,
		ClientSecret: o.cfg.ClientSecret,
		RedirectURL:  o.cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       o.cfg.Scopes,
	}
	o.verifier = o.newVerifier(ctx, meta)
	o.requireIss = meta.IssParameterSupported
	return o.oauth, o.verifier, o.requireIss, nil
}

// newVerifier is built by hand rather than with provider.Verifier only so the
// key set can be wrapped (keyFetchSet). The same key set, on the same
// background context, and the same algorithms provider.Verifier uses.
func (o *OIDC) newVerifier(ctx context.Context, meta providerMetadata) *oidc.IDTokenVerifier {
	keys := oidc.NewRemoteKeySet(ctx, meta.JWKSURL)
	return oidc.NewVerifier(o.cfg.IssuerURL, keyFetchSet{inner: keys}, &oidc.Config{
		ClientID:             o.cfg.ClientID,
		SupportedSigningAlgs: signingAlgs(meta.SigningAlgs),
	})
}

// providerMetadata is what discovery says beyond the endpoints go-oidc reads
// for itself.
type providerMetadata struct {
	JWKSURL     string   `json:"jwks_uri"`
	SigningAlgs []string `json:"id_token_signing_alg_values_supported"`
	// RFC 9207 section 3.
	IssParameterSupported bool `json:"authorization_response_iss_parameter_supported"`
}

// verifiableAlgs is go-oidc's own list of the algorithms it verifies
// (oidc.supportedAlgorithms, unexported), through which provider.Verifier
// filters the advertised ones. Anything else — HS256, none — a provider may
// advertise but must not be allowed to sign with.
var verifiableAlgs = map[string]bool{
	oidc.RS256: true, oidc.RS384: true, oidc.RS512: true,
	oidc.ES256: true, oidc.ES384: true, oidc.ES512: true,
	oidc.PS256: true, oidc.PS384: true, oidc.PS512: true,
	oidc.EdDSA: true,
}

// signingAlgs filters the advertised algorithms. Empty means go-oidc's
// default, RS256 alone — the one every provider must support.
func signingAlgs(advertised []string) []string {
	var out []string
	for _, a := range advertised {
		if verifiableAlgs[a] {
			out = append(out, a)
		}
	}
	return out
}

// keyFetchSet tells a key set that could not be fetched apart from a
// signature that did not verify.
//
// go-oidc reports both as one untyped error: the verifier formats whatever
// the key set returned with %v, so neither errors.Is nor errors.As can see
// through it. The key set is called with the verifier's context, though,
// and is the last place the distinction exists. RemoteKeySet prefixes a
// failed fetch — transport error, non-2xx answer, a body that is not a key
// set — with "fetching keys"; keys that fetched and did not verify say
// "failed to verify". TestOIDC_KeyFetchFailureIsUnavailable pins the prefix,
// so a go-oidc upgrade that changes it fails a test rather than quietly
// turning an outage back into a rejected sign-in.
type keyFetchSet struct{ inner oidc.KeySet }

// keyFetchErr is the context key under which a caller of Verify leaves a
// slot for a fetch failure.
type keyFetchErr struct{}

func (k keyFetchSet) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	payload, err := k.inner.VerifySignature(ctx, jwt)
	if err != nil && strings.HasPrefix(err.Error(), "fetching keys") {
		if slot, ok := ctx.Value(keyFetchErr{}).(*error); ok {
			*slot = err
		}
	}
	return payload, err
}

// AuthCodeURL is where to send the browser to sign in.
func (o *OIDC) AuthCodeURL(req AuthRequest) (string, error) {
	cfg, _, _, err := o.discover()
	if err != nil {
		return "", err
	}
	return cfg.AuthCodeURL(req.State, oidc.Nonce(req.Nonce), oauth2.S256ChallengeOption(req.Verifier)), nil
}

// idClaims are the ID token claims provisioning reads.
//
// EmailVerified is `any` because providers disagree on its type: the spec and
// most providers send a boolean, some (older AWS Cognito among them) send the
// string "true". Only an explicit true counts.
type idClaims struct {
	Email             string `json:"email"`
	EmailVerified     any    `json:"email_verified"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	// Entra ID's "email domain owner verified" (docs/adr/0023). Read only
	// under OIDCConfig.EntraVerifiedDomain.
	XMSEdov any `json:"xms_edov"`
	// Google's hosted domain. Read only for Google's issuer.
	HD string `json:"hd"`
}

// emailVerified decides whether the address may be trusted. email_verified,
// whenever the provider sends it, decides — an explicit false is never
// overruled. Only when it is absent, and only for an Entra issuer the
// operator opted in, does xms_edov stand in for it.
func (o *OIDC) emailVerified(c idClaims) bool {
	if c.EmailVerified != nil || !o.cfg.EntraVerifiedDomain {
		return verified(c.EmailVerified)
	}
	return verified(c.XMSEdov)
}

func verified(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true")
	default:
		return false
	}
}

// Callback is what the provider sent back to the redirect URI.
type Callback struct {
	Code string
	// Issuer is the RFC 9207 `iss` parameter, empty when none was sent.
	Issuer string
}

// checkResponseIssuer is the RFC 9207 defence against provider mix-up: an
// authorization response that names an issuer must name this one, and once
// a provider advertises the parameter, a response without it is refused —
// otherwise a response stripped of it would pass. Checked before the code is
// redeemed, so a code meant for another provider is never sent to this
// one's token endpoint together with this attempt's PKCE verifier.
func (o *OIDC) checkResponseIssuer(iss string, required bool) error {
	if iss == "" {
		if required {
			return fmt.Errorf("%w: the provider advertises the RFC 9207 iss parameter and the response carried none", ErrSignInRejected)
		}
		return nil
	}
	if iss != o.cfg.IssuerURL {
		// Not quoted: the value came from the browser, and this is logged.
		return fmt.Errorf("%w: the authorization response names a different issuer", ErrSignInRejected)
	}
	return nil
}

// Exchange completes a sign-in: it checks the response's issuer, redeems the
// code with the PKCE verifier, verifies the ID token (signature, issuer,
// audience, expiry — go-oidc — and the nonce, here), and reports who the
// provider says this is.
//
// The state has already been checked by the caller; it lives in the
// caller's cookie, not here.
func (o *OIDC) Exchange(ctx context.Context, cb Callback, req AuthRequest) (Identity, error) {
	cfg, verifier, requireIss, err := o.discover()
	if err != nil {
		return Identity{}, err
	}
	if err := o.checkResponseIssuer(cb.Issuer, requireIss); err != nil {
		return Identity{}, err
	}
	ctx = oidc.ClientContext(ctx, o.client)

	token, err := cfg.Exchange(ctx, cb.Code, oauth2.VerifierOption(req.Verifier))
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			// The provider answered and said no: a reused or expired code, a
			// verifier that does not match. Not an outage.
			return Identity{}, fmt.Errorf("%w: token endpoint: %v", ErrSignInRejected, err) //nolint:errorlint // see above
		}
		return Identity{}, fmt.Errorf("%w: token endpoint: %v", ErrUnavailable, err) //nolint:errorlint // see above
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return Identity{}, fmt.Errorf("%w: the token response carried no id_token", ErrSignInRejected)
	}
	var fetchErr error
	idToken, err := verifier.Verify(context.WithValue(ctx, keyFetchErr{}, &fetchErr), raw)
	if err != nil {
		if fetchErr != nil {
			// The token may be perfectly good; the keys to check it could
			// not be had — the provider is down, typically right after a
			// rotation put an unknown kid in front of us. An outage, not a
			// refusal, and nothing to record as an attack.
			return Identity{}, fmt.Errorf("%w: signing keys: %v", ErrUnavailable, fetchErr) //nolint:errorlint // see above
		}
		return Identity{}, fmt.Errorf("%w: id token: %v", ErrSignInRejected, err) //nolint:errorlint // see above
	}
	// go-oidc leaves the nonce to the caller. Without this check an ID token
	// issued for some other attempt — captured, or replayed from a log —
	// would be accepted for this one.
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(req.Nonce)) != 1 || req.Nonce == "" {
		return Identity{}, fmt.Errorf("%w: id token nonce does not match this sign-in", ErrSignInRejected)
	}

	var claims idClaims
	if err := idToken.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("%w: id token claims: %v", ErrSignInRejected, err) //nolint:errorlint // see above
	}
	if idToken.Subject == "" {
		return Identity{}, fmt.Errorf("%w: id token has no subject", ErrSignInRejected)
	}
	if claims.Email == "" {
		return Identity{}, fmt.Errorf("%w: provider %s released no email for subject %s", ErrNoEmail, o.cfg.Name, idToken.Subject)
	}
	if !o.emailVerified(claims) {
		// The subject, not the address: this error is logged, and an address
		// the provider has not verified may not even belong to the person.
		return Identity{}, fmt.Errorf("%w: subject %s at provider %s", ErrEmailNotVerified, idToken.Subject, o.cfg.Name)
	}

	// The domain is checked by the caller, after this verification, never
	// before: an unverified address could claim any domain it likes.
	hd := ""
	if o.isGoogle() {
		hd = strings.ToLower(claims.HD)
	}

	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	return Identity{
		Provider:    o.cfg.Name,
		Subject:     idToken.Subject,
		Email:       claims.Email,
		DisplayName: name,
		// The configured issuer, not the token's spelling of it: go-oidc
		// accepts Google's tokens as either https://accounts.google.com or
		// accounts.google.com, and a link keyed on whichever one a given
		// token used would read as a different issuer on the next sign-in.
		// Verification has already established they are the same.
		Issuer:       o.cfg.IssuerURL,
		HostedDomain: hd,
	}, nil
}
