package directory

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// fakeIdP is a provider these tests can break on purpose — a key-set
// endpoint that fails, a token signed with the wrong key, claims Keycloak
// will not emit (Entra's xms_edov). Everything a real provider proves about
// verification is tested against Keycloak in oidc_integration_test.go; this
// exists for the failure modes a real provider cannot be made to have.
type fakeIdP struct {
	t   *testing.T
	srv *httptest.Server

	mu sync.Mutex
	// key is what the key-set endpoint publishes; signer signs the next
	// token. The same key unless a test is forging.
	key, signer *rsa.PrivateKey
	kid         string
	jwksDown    bool
	jwksURL     string // overrides the published jwks_uri when set
	issParam    bool
	claims      map[string]any
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdP{t: t, key: key, signer: key, kid: "k1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", idp.discovery)
	mux.HandleFunc("/jwks", idp.jwks)
	mux.HandleFunc("/token", idp.token)
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

func (p *fakeIdP) set(f func(p *fakeIdP)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f(p)
}

func (p *fakeIdP) discovery(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	jwks := p.jwksURL
	if jwks == "" {
		jwks = p.srv.URL + "/jwks"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                p.srv.URL,
		"authorization_endpoint":                p.srv.URL + "/auth",
		"token_endpoint":                        p.srv.URL + "/token",
		"jwks_uri":                              jwks,
		"id_token_signing_alg_values_supported": []string{"RS256", "HS256"},
		"authorization_response_iss_parameter_supported": p.issParam,
	})
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (p *fakeIdP) jwks(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.jwksDown {
		http.Error(w, "down for maintenance", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": p.kid,
		"n": b64(p.key.N.Bytes()),
		"e": b64(big.NewInt(int64(p.key.E)).Bytes()),
	}}})
}

func (p *fakeIdP) token(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	claims := map[string]any{
		"iss": p.srv.URL, "aud": "cc", "sub": "subject-1",
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"email": "ana@example.test",
	}
	for k, v := range p.claims {
		claims[k] = v
	}
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": p.kid})
	body, _ := json.Marshal(claims)
	signingInput := b64(header) + "." + b64(body)
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.signer, crypto.SHA256, sum[:])
	if err != nil {
		p.t.Error(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "at", "token_type": "Bearer", "expires_in": 300,
		"id_token": signingInput + "." + b64(sig),
	})
}

func (p *fakeIdP) oidc(t *testing.T) *OIDC {
	t.Helper()
	o, err := NewOIDC(OIDCConfig{
		Name: "fake", IssuerURL: p.srv.URL, ClientID: "cc", ClientSecret: "s",
		RedirectURL: "http://127.0.0.1:12001/v1/auth/oidc/callback",
		Timeout:     2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// signIn runs one exchange with the token's nonce matching the attempt.
func (p *fakeIdP) signIn(o *OIDC, cb Callback, extra map[string]any) (Identity, error) {
	req := NewAuthRequest()
	claims := map[string]any{"nonce": req.Nonce, "email_verified": true}
	for k, v := range extra {
		claims[k] = v
	}
	p.set(func(p *fakeIdP) { p.claims = claims })
	if cb.Code == "" {
		cb.Code = "code"
	}
	return o.Exchange(context.Background(), cb, req)
}

func TestFakeIdPSignsIn(t *testing.T) {
	idp := newFakeIdP(t)
	o := idp.oidc(t)
	id, err := idp.signIn(o, Callback{}, nil)
	if err != nil {
		t.Fatalf("the fixture itself does not work: %v", err)
	}
	if id.Issuer != idp.srv.URL || id.Subject != "subject-1" {
		t.Fatalf("identity = %+v", id)
	}
}

// A key set that cannot be fetched is an outage, not a refused sign-in: the
// token may be perfectly good. The case that matters is a rotation — keys
// cached, a token arrives under a kid not among them, and the provider is
// down when asked for the new set.
func TestOIDC_KeyFetchFailureIsUnavailable(t *testing.T) {
	t.Run("key set endpoint answers 503", func(t *testing.T) {
		idp := newFakeIdP(t)
		idp.set(func(p *fakeIdP) { p.jwksDown = true })
		_, err := idp.signIn(idp.oidc(t), Callback{}, nil)
		if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrSignInRejected) {
			t.Fatalf("want ErrUnavailable alone, got %v", err)
		}
	})

	t.Run("key set endpoint unreachable", func(t *testing.T) {
		idp := newFakeIdP(t)
		idp.set(func(p *fakeIdP) { p.jwksURL = "http://127.0.0.1:1/jwks" })
		_, err := idp.signIn(idp.oidc(t), Callback{}, nil)
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("want ErrUnavailable, got %v", err)
		}
	})

	t.Run("rotation while the provider is down", func(t *testing.T) {
		idp := newFakeIdP(t)
		o := idp.oidc(t)
		if _, err := idp.signIn(o, Callback{}, nil); err != nil {
			t.Fatalf("first sign-in, which caches the keys: %v", err)
		}
		rotated, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		idp.set(func(p *fakeIdP) { p.signer, p.kid, p.jwksDown = rotated, "k2", true })
		_, err = idp.signIn(o, Callback{}, nil)
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("want ErrUnavailable, got %v", err)
		}
	})

	// And the contrast that keeps the above honest: keys that fetched fine
	// and do not verify the token are a rejection, never an outage.
	t.Run("a forged signature is still rejected", func(t *testing.T) {
		idp := newFakeIdP(t)
		forger, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		idp.set(func(p *fakeIdP) { p.signer = forger })
		_, err = idp.signIn(idp.oidc(t), Callback{}, nil)
		if !errors.Is(err, ErrSignInRejected) || errors.Is(err, ErrUnavailable) {
			t.Fatalf("want ErrSignInRejected alone, got %v", err)
		}
	})
}

// Google's tokens may spell the issuer with or without the scheme, and
// go-oidc accepts both for https://accounts.google.com. The identity must
// carry the configured issuer either way, or the link stored under one
// spelling would read as an issuer change on the next sign-in.
func TestOIDC_IdentityCarriesTheConfiguredIssuerNotTheTokensSpelling(t *testing.T) {
	const google = "https://accounts.google.com"
	idp := newFakeIdP(t)
	o, err := NewOIDC(OIDCConfig{
		Name: "google", IssuerURL: google, ClientID: "cc", ClientSecret: "s",
		RedirectURL: "http://127.0.0.1:12001/v1/auth/oidc/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Discovery would go to Google; wire this provider to the fake's
	// endpoints instead, with the verifier built exactly as discover builds
	// it — expecting Google's issuer.
	fake := idp.oidc(t)
	if _, _, _, err := fake.discover(); err != nil {
		t.Fatal(err)
	}
	o.oauth = fake.oauth
	o.verifier = o.newVerifier(oidc.ClientContext(context.Background(), o.client),
		providerMetadata{JWKSURL: idp.srv.URL + "/jwks", SigningAlgs: []string{"RS256"}})

	for _, spelling := range []string{google, "accounts.google.com"} {
		id, err := idp.signIn(o, Callback{}, map[string]any{"iss": spelling})
		if err != nil {
			t.Fatalf("token issued as %q: %v", spelling, err)
		}
		if id.Issuer != google {
			t.Fatalf("token issued as %q: identity issuer = %q, want the configured %q", spelling, id.Issuer, google)
		}
	}
}

// HS256 is advertised by this fake and must not be accepted: the filter
// provider.Verifier applied has to survive building the verifier by hand.
func TestSigningAlgsDropsWhatGoOIDCCannotVerify(t *testing.T) {
	got := signingAlgs([]string{"RS256", "HS256", "none", "ES256"})
	if strings.Join(got, ",") != "RS256,ES256" {
		t.Fatalf("signingAlgs = %v", got)
	}
}

func TestOIDC_ResponseIssuer(t *testing.T) {
	t.Run("not advertised: absent is accepted, wrong is not", func(t *testing.T) {
		idp := newFakeIdP(t)
		o := idp.oidc(t)
		if _, err := idp.signIn(o, Callback{}, nil); err != nil {
			t.Fatalf("absent iss from a provider that does not advertise it: %v", err)
		}
		if _, err := idp.signIn(o, Callback{Issuer: idp.srv.URL}, nil); err != nil {
			t.Fatalf("correct iss: %v", err)
		}
		if _, err := idp.signIn(o, Callback{Issuer: "https://other.example.test"}, nil); !errors.Is(err, ErrSignInRejected) {
			t.Fatalf("wrong iss: want ErrSignInRejected, got %v", err)
		}
	})
	t.Run("advertised: absent is refused", func(t *testing.T) {
		idp := newFakeIdP(t)
		idp.set(func(p *fakeIdP) { p.issParam = true })
		o := idp.oidc(t)
		if _, err := idp.signIn(o, Callback{}, nil); !errors.Is(err, ErrSignInRejected) {
			t.Fatalf("absent iss: want ErrSignInRejected, got %v", err)
		}
		if _, err := idp.signIn(o, Callback{Issuer: idp.srv.URL}, nil); err != nil {
			t.Fatalf("correct iss: %v", err)
		}
	})
}

// Entra sends no email_verified. With the switch, xms_edov stands in for it
// — and only when email_verified is absent; without the switch nothing
// changes.
func TestOIDC_EntraVerifiedDomain(t *testing.T) {
	idp := newFakeIdP(t)
	plain := idp.oidc(t)
	entra := idp.oidc(t)
	// NewOIDC refuses the switch for this (non-Entra) issuer, which is the
	// point of it; set it past that check to exercise the claim logic.
	entra.cfg.EntraVerifiedDomain = true

	// Each case's claims are the token's, besides nonce and email: no
	// email_verified unless the case lists one, as Entra sends none.
	cases := []struct {
		name   string
		o      *OIDC
		claims map[string]any
		want   error
	}{
		{"switch off: xms_edov is ignored", plain, map[string]any{"xms_edov": true}, ErrEmailNotVerified},
		{"entra: verified domain", entra, map[string]any{"xms_edov": true}, nil},
		{"entra: the claim as a string", entra, map[string]any{"xms_edov": "true"}, nil},
		{"entra: domain not verified", entra, map[string]any{"xms_edov": false}, ErrEmailNotVerified},
		{"entra: claim not released", entra, map[string]any{}, ErrEmailNotVerified},
		{"entra: an explicit false is never overruled", entra, map[string]any{"email_verified": false, "xms_edov": true}, ErrEmailNotVerified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := NewAuthRequest()
			claims := map[string]any{"nonce": req.Nonce}
			for k, v := range tc.claims {
				claims[k] = v
			}
			idp.set(func(p *fakeIdP) { p.claims = claims })
			_, err := tc.o.Exchange(context.Background(), Callback{Code: "code"}, req)
			if tc.want == nil && err != nil {
				t.Fatalf("want success, got %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}
