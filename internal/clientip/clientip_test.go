package clientip

import (
	"net/http"
	"testing"
)

func TestClient(t *testing.T) {
	r, err := Parse([]string{"127.0.0.1", "10.0.0.0/8", " ", "::1"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		remote string
		xff    []string
		want   string
	}{
		{"untrusted peer: the header is ignored", "203.0.113.9:4000", []string{"1.2.3.4"}, "203.0.113.9"},
		{"trusted peer: the client it reports", "127.0.0.1:4000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"a spoofed entry left of the real one is skipped", "127.0.0.1:4000", []string{"6.6.6.6, 198.51.100.7"}, "198.51.100.7"},
		{"chained trusted proxies are walked past", "127.0.0.1:4000", []string{"198.51.100.7, 10.1.2.3"}, "198.51.100.7"},
		{"several headers read as one list", "127.0.0.1:4000", []string{"6.6.6.6", "198.51.100.7"}, "198.51.100.7"},
		{"garbage stops the walk at the last trusted hop", "127.0.0.1:4000", []string{"198.51.100.7, nonsense"}, "127.0.0.1"},
		{"no header: the proxy itself", "127.0.0.1:4000", nil, "127.0.0.1"},
		{"IPv6 peer", "[::1]:4000", []string{"2001:db8::5"}, "2001:db8::5"},
		{"IPv4-mapped peer", "[::ffff:127.0.0.1]:4000", []string{"198.51.100.7"}, "198.51.100.7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				req.Header.Add("X-Forwarded-For", v)
			}
			if got := r.Client(req).String(); got != tc.want {
				t.Fatalf("Client = %s, want %s", got, tc.want)
			}
		})
	}

	var none *Resolver
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := none.Client(req).String(); got != "127.0.0.1" {
		t.Fatalf("nil resolver trusted a header: %s", got)
	}
	if _, err := Parse([]string{"not-an-ip"}); err == nil {
		t.Fatal("a bad entry was accepted")
	}
}
