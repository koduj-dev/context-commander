# 0029. Trusted proxies, and per-IP limits on unauthenticated endpoints

## Status

Accepted. Closes the backlog item "[security] Per-IP rate limiting needs a
trusted-proxy configuration".

## Context

Authenticated traffic is limited per credential (internal/ratelimit).
Unauthenticated traffic was bounded only by global ceilings: authentications
in flight, password hashes in flight, and attempts per sign-in identifier.
Nothing limited how fast one client could call the sign-in, single sign-on
and setup endpoints. The OIDC review (ADR 0023) left one consequence open: a
mismatched state writes a security event, and nothing bounds how many.

Per-IP limits were not possible before. The supported deployment is a
loopback bind behind a reverse proxy, so every request arrives from the
proxy's address. Trusting `X-Forwarded-For` unconditionally lets anyone pick
their own address (GHSA-3fxj-6jh8-hvhx). One bucket for the proxy's address
would let any caller lock everyone out.

The team deploying this on 2026-11-13 runs it behind a proxy, with Google
sign-in open to a whole domain.

## Decision

- `--trusted-proxies` (`CC_TRUSTED_PROXIES`) names the proxies, as addresses
  or CIDRs. `internal/clientip` reads `X-Forwarded-For` only when the TCP
  peer is one of them. It walks the header from the right, past trusted
  hops, and takes the first untrusted address. An unparseable entry stops the
  walk at the last trusted hop. A client can prepend whatever it likes; it
  cannot get past the entry its own proxy appended.
- The public route group is limited per client address:
  `--rate-limit-unauthenticated-per-minute` (default 60) and
  `--rate-limit-unauthenticated-burst` (default 30). IPv6 addresses share a
  bucket per /64. A refusal is a 429 with `Retry-After` and a `rate_limited`
  security event.
- A loopback peer with no trusted proxies configured is not limited. It is
  the proxy, and limiting it is the shared bucket this ADR exists to avoid.
  The trusted proxies themselves are never limited either.
- Authenticated endpoints keep their per-credential limit and get no per-IP
  limit. Once identity is proven, the credential is the better key, and
  several agents behind one NAT are normal.
- `middleware.RealIP` stays out. The resolved address is used for the limit
  and is not written back into `r.RemoteAddr`, so nothing else starts
  trusting the header by accident.

## Consequences

- An operator who forgets `--trusted-proxies` behind a proxy gets the
  previous behaviour: no per-IP limit. Nothing becomes worse than before.
  The docs say to set it.
- The proxy must append to `X-Forwarded-For` or overwrite it. A proxy that
  passes a client's header through unchanged makes the client's leftmost
  entry its address. nginx's `$proxy_add_x_forwarded_for` appends.
- Security events still record no address. Whether they should is a separate
  question about personal data in the log.
