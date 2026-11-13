// Package clientip decides which address a request came from (docs/adr/0029).
//
// The supported deployment is a loopback bind behind a TLS-terminating
// reverse proxy, so the TCP peer of every request is the proxy. The client's
// address is in X-Forwarded-For, which anyone can also send: a header is
// only evidence when the peer that delivered it is a proxy the operator
// named. So the header is read only from a trusted peer, and from the right:
// each trusted proxy appends the address it received from, and the first
// untrusted address from the right is the last hop nobody we trust vouched
// past — the client, or the closest thing to it that can be known.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Resolver holds the trusted proxies. The zero value trusts nobody, which
// makes Client the TCP peer.
type Resolver struct {
	trusted []netip.Prefix
}

// Parse reads a list of CIDRs or bare addresses ("127.0.0.1",
// "10.0.0.0/8", "::1").
func Parse(entries []string) (*Resolver, error) {
	r := &Resolver{}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if strings.Contains(e, "/") {
			p, err := netip.ParsePrefix(e)
			if err != nil {
				return nil, fmt.Errorf("clientip: trusted proxy %q: %w", e, err)
			}
			r.trusted = append(r.trusted, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(e)
		if err != nil {
			return nil, fmt.Errorf("clientip: trusted proxy %q is neither an address nor a CIDR", e)
		}
		a = a.Unmap()
		r.trusted = append(r.trusted, netip.PrefixFrom(a, a.BitLen()))
	}
	return r, nil
}

// Configured reports whether any proxy is trusted.
func (r *Resolver) Configured() bool { return r != nil && len(r.trusted) > 0 }

// Trusts reports whether a is a trusted proxy.
func (r *Resolver) Trusts(a netip.Addr) bool {
	if r == nil {
		return false
	}
	a = a.Unmap()
	for _, p := range r.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// Client is the address the request came from: the TCP peer, unless the
// peer is trusted, in which case the rightmost untrusted X-Forwarded-For
// entry. An unparseable entry stops the walk at the last trusted hop, so a
// garbled header cannot be used to pick an address. The zero Addr means the
// peer itself could not be read.
func (r *Resolver) Client(req *http.Request) netip.Addr {
	peer := peerAddr(req.RemoteAddr)
	if !peer.IsValid() || !r.Trusts(peer) {
		return peer
	}
	hops := forwardedFor(req.Header.Values("X-Forwarded-For"))
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(hops[i])
		if err != nil {
			return client
		}
		a = a.Unmap()
		client = a
		if !r.Trusts(a) {
			return a
		}
	}
	return client
}

func peerAddr(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}

// forwardedFor flattens every X-Forwarded-For header, in order: proxies may
// append a header rather than extend one.
func forwardedFor(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
