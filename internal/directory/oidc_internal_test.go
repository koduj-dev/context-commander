package directory

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A provider that fails discovery must be asked again only after
// discoveryRetryAfter. Before this, every anonymous /start during an outage
// queued behind the discovery lock and made its own outbound request.
func TestDiscoveryFailureIsRememberedBriefly(t *testing.T) {
	var hits atomic.Int32
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer idp.Close()

	o, err := NewOIDC(OIDCConfig{
		IssuerURL:    idp.URL,
		ClientID:     "cc",
		ClientSecret: "s",
		RedirectURL:  "http://127.0.0.1:12001/v1/auth/oidc/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	o.now = func() time.Time { return clock }

	for i := range 3 {
		if _, err := o.AuthCodeURL(NewAuthRequest()); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("attempt %d: want ErrUnavailable, got %v", i, err)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("provider asked %d times within the retry window, want 1", got)
	}

	clock = clock.Add(discoveryRetryAfter + time.Second)
	if _, err := o.AuthCodeURL(NewAuthRequest()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("after the window: want ErrUnavailable, got %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("provider asked %d times after the window, want 2 — a provider that recovers must be retried", got)
	}
}

func TestAllowedDomainsMatchExactlyAndIgnoreCase(t *testing.T) {
	o, err := NewOIDC(OIDCConfig{
		IssuerURL: "https://id.example.test/realms/x", ClientID: "cc", ClientSecret: "s",
		RedirectURL:    "https://cc.example.test/v1/auth/oidc/callback",
		AllowedDomains: []string{" Example.COM ", ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		"ana@example.com":      true,
		"ANA@EXAMPLE.COM":      true,
		"ana@mail.example.com": false, // no subdomains
		"ana@example.com.evil": false, // not a suffix match either
		"ana@evilexample.com":  false,
		"example.com":          false, // no @ at all
		"a@b@example.com":      true,  // the domain is what follows the last @
	}
	for email, want := range cases {
		if got := o.domainAllowed(email); got != want {
			t.Errorf("domainAllowed(%q) = %v, want %v", email, got, want)
		}
	}

	open, _ := NewOIDC(OIDCConfig{
		IssuerURL: "https://id.example.test/realms/x", ClientID: "cc", ClientSecret: "s",
		RedirectURL: "https://cc.example.test/v1/auth/oidc/callback",
	})
	if !open.domainAllowed("anyone@anywhere.test") {
		t.Error("with no list configured, every domain must be admitted")
	}

	for _, bad := range []string{"@example.com", "ana@example.com", ".example.com"} {
		if _, err := NewOIDC(OIDCConfig{
			IssuerURL: "https://id.example.test/realms/x", ClientID: "cc", ClientSecret: "s",
			RedirectURL:    "https://cc.example.test/v1/auth/oidc/callback",
			AllowedDomains: []string{bad},
		}); err == nil {
			t.Errorf("allowed domain %q accepted; want a configuration error", bad)
		}
	}
}
