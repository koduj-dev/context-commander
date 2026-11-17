package http_test

import (
	"net/http"
	"strings"
	"testing"
)

// Every response carries the browser hardening headers, the API's and the
// interface's alike, and the policy forbids inline script and framing.
func TestSecurityHeaders(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/healthz", "/v1/auth/methods", "/v1/me"} {
		resp := f.do(http.MethodGet, path, "", nil)
		_ = resp.Body.Close()
		h := resp.Header
		csp := h.Get("Content-Security-Policy")
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "DENY" ||
			h.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(csp, "frame-ancestors 'none'") ||
			!strings.Contains(csp, "script-src 'self';") || strings.Contains(csp, "unsafe-eval") ||
			strings.Contains(csp, "https://") {
			t.Errorf("%s: headers %v", path, h)
		}
		if h.Get("Strict-Transport-Security") != "" {
			t.Errorf("%s: HSTS set on a plaintext test server", path)
		}
	}
}
