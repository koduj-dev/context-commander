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

func TestAdmissionNormalizesDomainsAndAppliesHostedDomainToGoogleOnly(t *testing.T) {
	o, err := NewOIDC(OIDCConfig{
		IssuerURL: "https://id.example.test/realms/x", ClientID: "cc", ClientSecret: "s",
		RedirectURL:    "https://cc.example.test/v1/auth/oidc/callback",
		AllowedDomains: []string{" Example.COM ", ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	adm := o.Admission()
	if len(adm.Domains) != 1 || adm.Domains[0] != "example.com" {
		t.Fatalf("Admission().Domains = %v, want [example.com]", adm.Domains)
	}
	if adm.RequireHostedDomain {
		t.Fatal("the Workspace check applies to Google only, not to every issuer")
	}

	google := func(ignore bool) Admission {
		g, err := NewOIDC(OIDCConfig{
			IssuerURL: googleIssuer, ClientID: "cc", ClientSecret: "s",
			RedirectURL:              "https://cc.example.test/v1/auth/oidc/callback",
			IgnoreGoogleHostedDomain: ignore,
		})
		if err != nil {
			t.Fatal(err)
		}
		return g.Admission()
	}
	if !google(false).RequireHostedDomain {
		t.Fatal("Google: the Workspace check must be on by default")
	}
	if google(true).RequireHostedDomain {
		t.Fatal("Google with the check turned off still requires hd")
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
