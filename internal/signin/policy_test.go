package signin

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct {
		in       string
		kind     Kind
		value    string
		rejected bool
	}{
		{in: " Example.COM ", kind: KindDomain, value: "example.com"},
		{in: "Ana@Example.com", kind: KindAddress, value: "ana@example.com"},
		{in: "4net.tv", kind: KindDomain, value: "4net.tv"},
		{in: "", rejected: true},
		{in: "localhost", rejected: true},       // one label: not a domain anyone signs in from
		{in: "*.example.com", rejected: true},   // reads as "and every subdomain"
		{in: ".example.com", rejected: true},    // same
		{in: "@example.com", rejected: true},    // no local part
		{in: "ana@", rejected: true},            // no domain
		{in: "ana@localhost", rejected: true},   // domain must have two labels
		{in: "a b@example.com", rejected: true}, // whitespace in the local part
		{in: "-bad.example.com", rejected: true},
		{in: "exa_mple.com", rejected: true},
	}
	for _, tc := range cases {
		kind, value, err := Normalize(tc.in)
		if tc.rejected {
			if err == nil {
				t.Errorf("Normalize(%q) = %s %q, want an error", tc.in, kind, value)
			}
			continue
		}
		if err != nil || kind != tc.kind || value != tc.value {
			t.Errorf("Normalize(%q) = %s %q %v, want %s %q", tc.in, kind, value, err, tc.kind, tc.value)
		}
	}
}

func TestDecide(t *testing.T) {
	domain := Rule{Kind: KindDomain, Value: "4net.tv"}
	address := Rule{Kind: KindAddress, Value: "guest@gmail.com"}

	cases := []struct {
		name string
		in   Input
		want Decision
	}{
		{
			name: "nothing listed admits everyone, and no rule matched",
			in:   Input{Email: "anyone@anywhere.test"},
			want: Decision{Admitted: true, DomainTrusted: true},
		},
		{
			name: "a domain rule admits its domain, case-insensitively",
			in:   Input{Email: "Ana@4NET.tv", Rules: []Rule{domain}},
			want: Decision{Admitted: true, ByRule: true, DomainTrusted: true},
		},
		{
			name: "a domain rule does not admit a subdomain",
			in:   Input{Email: "ana@mail.4net.tv", Rules: []Rule{domain}},
			want: Decision{DomainTrusted: true},
		},
		{
			name: "an address rule admits exactly that address",
			in:   Input{Email: "guest@gmail.com", Rules: []Rule{domain, address}},
			want: Decision{Admitted: true, ByRule: true, DomainTrusted: true},
		},
		{
			name: "an address rule alone closes the door to everyone else",
			in:   Input{Email: "other@gmail.com", Rules: []Rule{address}},
			want: Decision{DomainTrusted: true},
		},
		{
			name: "a flag domain admits, but is not a rule: no account by rule",
			in:   Input{Email: "ana@example.com", FlagDomains: []string{"example.com"}, Rules: []Rule{domain}},
			want: Decision{Admitted: true, DomainTrusted: true},
		},
		{
			name: "flag and rules are one list: a rule admits beside a flag that does not",
			in:   Input{Email: "ana@4net.tv", FlagDomains: []string{"example.com"}, Rules: []Rule{domain}},
			want: Decision{Admitted: true, ByRule: true, DomainTrusted: true},
		},
		{
			name: "Google: a Workspace account in a listed domain is admitted",
			in:   Input{Email: "ana@4net.tv", HostedDomain: "4net.tv", RequireHostedDomain: true, Rules: []Rule{domain}},
			want: Decision{Admitted: true, ByRule: true, DomainTrusted: true},
		},
		{
			name: "Google: a personal account under a work address is refused",
			in:   Input{Email: "ana@4net.tv", RequireHostedDomain: true, Rules: []Rule{domain}},
			want: Decision{},
		},
		{
			name: "Google: an account in someone else's Workspace is refused",
			in:   Input{Email: "ana@4net.tv", HostedDomain: "evil.test", RequireHostedDomain: true, Rules: []Rule{domain}},
			want: Decision{},
		},
		{
			name: "Google: an address rule needs no Workspace, and grants by address only",
			in:   Input{Email: "guest@gmail.com", RequireHostedDomain: true, Rules: []Rule{domain, address}},
			want: Decision{Admitted: true, ByRule: true},
		},
		{
			name: "Google without the check: the address decides, as for any issuer",
			in:   Input{Email: "ana@4net.tv", Rules: []Rule{domain}},
			want: Decision{Admitted: true, ByRule: true, DomainTrusted: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(tc.in); got != tc.want {
				t.Fatalf("Decide = %+v, want %+v", got, tc.want)
			}
		})
	}
}
