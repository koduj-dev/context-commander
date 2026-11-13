package http_test

import (
	"encoding/json"
	"net/http"
	"testing"

	cchttp "github.com/koduj-dev/context-commander/adapters/http"
	"github.com/koduj-dev/context-commander/internal/clientip"
	"github.com/koduj-dev/context-commander/internal/ratelimit"
)

// TestFailedAuthenticationIsRecordedAndReadableByAdmin walks the whole
// path the CRA opt-in describes and an incident investigation needs: a bad
// token is refused, the refusal is written down, and an installation admin
// can read it back over the API without touching SQL.
func TestFailedAuthenticationIsRecordedAndReadableByAdmin(t *testing.T) {
	f := newSecurityFixture(t)
	admin := f.newInstallationAdminToken("admin@example.test")

	// A well-formed token naming a credential that does not exist — the
	// shape that reaches the database lookup before being rejected, and the
	// row a foreign-keyed audit table could never hold.
	ghost := "cc_" + "6f1c2d3e-4a5b-4c6d-8e9f-0a1b2c3d4e5f" + "_not-the-real-secret"
	resp := f.do(http.MethodGet, "/v1/me", ghost, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d, want 401", resp.StatusCode)
	}

	var events []struct {
		Kind         string `json:"kind"`
		CredentialID string `json:"credential_id"`
		Reason       string `json:"reason"`
	}
	raw := f.mustDo(http.MethodGet, "/v1/installation/security-events", admin, nil, http.StatusOK, "read security events")
	var envelope struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if err := json.Unmarshal(envelope.Items, &events); err != nil {
		t.Fatalf("decode items: %v", err)
	}

	var found bool
	for _, e := range events {
		if e.Kind == "authentication_failed" && e.CredentialID == "6f1c2d3e-4a5b-4c6d-8e9f-0a1b2c3d4e5f" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the failed authentication was not recorded; events = %+v", events)
	}
}

// TestSecurityEventsRejectNonAdmin: the timeline names principals and the
// projects they were refused on, across every project in the instance. An
// ordinary authenticated user must not see it.
func TestSecurityEventsRejectNonAdmin(t *testing.T) {
	f := newSecurityFixture(t)
	ordinary := f.newUserWithToken("ordinary@example.test")

	resp := f.do(http.MethodGet, "/v1/installation/security-events", ordinary, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403", resp.StatusCode)
	}
}

// TestRateLimitedCredentialGets429 pins that the limit applies per
// credential and is reported as 429 with a Retry-After, not as a generic
// error the client cannot act on.
func TestRateLimitedCredentialGets429(t *testing.T) {
	// Burst of 3, effectively no refill: the fourth request must be refused.
	f := newSecurityFixtureWithLimits(t, 0.001, 3)
	token := f.newUserWithToken("noisy@example.test")

	for i := range 3 {
		resp := f.do(http.MethodGet, "/v1/me", token, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d inside the burst: status=%d", i, resp.StatusCode)
		}
	}

	resp := f.do(http.MethodGet, "/v1/me", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("request past the burst: status=%d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("429 carries no Retry-After header, so a client has nothing to back off by")
	}

	// A different credential is unaffected — the property a per-IP limiter
	// could not provide behind a reverse proxy.
	other := f.newUserWithToken("quiet@example.test")
	resp2 := f.do(http.MethodGet, "/v1/me", other, nil)
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("a second credential got status=%d after the first exhausted its budget", resp2.StatusCode)
	}
}

// Behind a trusted proxy, the public endpoints are limited per client
// address read from X-Forwarded-For; a spoofed entry to the left of the
// proxy's own does not buy a fresh bucket, and another client is unaffected.
func TestUnauthenticatedEndpointsAreLimitedPerClientBehindATrustedProxy(t *testing.T) {
	proxies, err := clientip.Parse([]string{"127.0.0.1", "::1"})
	if err != nil {
		t.Fatal(err)
	}
	f := newFixtureWithDeps(t, boolPtr(true), ratelimit.Limits{PerIP: 0.001, IPBurst: 2}, func(d *cchttp.Deps) {
		d.ClientIP = proxies
	})
	get := func(xff string) int {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, f.server.URL+"/v1/auth/methods", nil)
		req.Header.Set("X-Forwarded-For", xff)
		resp, err := f.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if get("198.51.100.7") != http.StatusOK || get("6.6.6.6, 198.51.100.7") != http.StatusOK {
		t.Fatal("the burst was refused")
	}
	if got := get("1.1.1.1, 198.51.100.7"); got != http.StatusTooManyRequests {
		t.Fatalf("third request from the same client: %d, want 429", got)
	}
	if got := get("198.51.100.8"); got != http.StatusOK {
		t.Fatalf("another client: %d, want 200", got)
	}
	if f.securityEvents("unauthenticated requests from one address exceeded their rate limit") != 1 {
		t.Fatal("the refusal was not recorded")
	}
}

// Without trusted proxies, the loopback peer is the reverse proxy, and it is
// not limited per address: one bucket for everyone would let any caller lock
// out the rest.
func TestTheProxyIsNotLimitedWithoutTrustedProxies(t *testing.T) {
	f := newFixtureWith(t, nil, ratelimit.Limits{PerIP: 0.001, IPBurst: 1})
	for i := 0; i < 3; i++ {
		if r := f.get("/v1/auth/methods"); r.status != http.StatusOK {
			t.Fatalf("request %d: %d", i, r.status)
		}
	}
}
