# 0023. OIDC hardening: Entra's verified domains, a signed attempt cookie, and the issuer beside the link

## Status

Accepted. Refines [ADR 0019](0019-browser-authentication-and-directory-identity.md)
and [ADR 0020](0020-oidc-sign-up-is-off-by-default.md) for OpenID Connect.

## Context

OIDC sign-in shipped on 2026-10-27 together with a review that listed what it
left open (`NEXT.md`, "Directory sign-in"). None of the findings was
exploitable as configured that day. They came in two groups:

- **What blocks a provider.** Microsoft Entra ID cannot sign anyone in,
  because it never sends `email_verified` and an unverified address is
  refused. A second provider was blocked until the callback could defend
  against provider mix-up.
- **What is weaker than it needs to be.** The `cc_oidc` attempt cookie was
  unsigned. A key-fetch failure was reported as a rejected sign-in. Links were
  keyed on the operator's provider name and not on the issuer. A second
  `/start` replaced the first attempt. A sign-in left the browser's previous
  session alive. State mismatches were recorded with no limit of their own.

The findings are decided here together because they share code, and several
of them share one argument: what this server can prove, and what it would only
be trusting.

## Decision

### Entra ID: `xms_edov`, behind a switch named for Entra

`--oidc-entra-verified-domain` accepts an address when the ID token's
`xms_edov` claim is true and the token carries no `email_verified`. The server
refuses to start with the switch for an issuer that is not on an Entra host
(`login.microsoftonline.com` and the national clouds). If the token does carry
`email_verified`, that claim decides, and an explicit false is never
overruled.

`email_verified` exists because provisioning links to an existing account by
address (ADR 0019). Entra's `email` claim is a directory attribute. Microsoft
documents it as unverified and mutable. Another tenant's administrator can put
any address on one of their users, and when that user signs in as a guest the
address is in the token. That is the "nOAuth" class. `xms_edov` is Microsoft's
answer to it. The claim is true only when the tenant the account lives in has
verified the address's domain, or when the address belongs to a Microsoft
account, a Google account or a one-time-passcode guest. It is never true for
Facebook or SAML/WS-Fed guests. The operator has to add the claim to the app
registration. Until they do, every address reads as unverified, which fails
safe.

Rejected:

- **A generic `--oidc-trust-email`.** It turns the check off for whatever
  issuer is configured, including one that releases addresses its users typed
  themselves. A switch whose correct use depends on which provider it is used
  with should name that provider.
- **Trusting members only (`acct = 0`).** It turns away guests whose address
  is in fact verified, and it trusts the `email` attribute rather than any
  verification of it.
- **`verified_primary_email`.** It is sourced from Exchange, is empty in
  tenants without it, and is a list rather than a statement about the address
  in `email`.

### The attempt cookie is signed, with a key derived from the root key

Each attempt cookie is `payload.mac`, where the MAC is HMAC-SHA256 over the
cookie's name and payload. The payload now includes the start time, so the
ten-minute lifetime is enforced by the server and not only by the browser's
`Max-Age`. A cookie that does not verify is refused before it is parsed, and
the refusal is recorded as a security event.

The key is `HKDF-SHA256(root key, info = "context-commander/oidc-attempt-cookie/v1")`
(`crypto.DeriveKey`). This is the first key derived from the root key. Until
now the root key has had one job, wrapping project keys (ADR 0005). It is not
used directly as a MAC key, because a key with two jobs can have one played
against the other. A distinct HKDF info string per purpose is the standard way
to get independent keys without storing more secrets. When the root key is
rotated, attempts in flight read as expired, which is acceptable for something
that lives ten minutes.

Rejected:

- **A random key per process.** Every restart would expire every attempt in
  flight, and two replicas behind one address could never verify each other's
  cookies.
- **A new secret flag.** It is one more secret for the operator to generate,
  store and rotate, and it protects nothing the root key does not already
  protect.

### One cookie per attempt

Each attempt has its own cookie, `cc_oidc_<first 8 bytes of SHA-256(state), hex>`.
A second `/start` no longer overwrites the first. At most three attempts are
kept, and `/start` drops the oldest beyond that and any that fail to verify.
The callback clears only its own cookie. If a callback's state matches none of
the browser's attempts while it holds others, that is the shape of a login
CSRF, and it is recorded as a state mismatch. If the browser holds no attempt
at all, the attempt has simply expired and nothing is recorded.

### Provider mix-up: RFC 9207

If the authorization response carries `iss`, it must equal the configured
issuer. If the provider's discovery document advertises
`authorization_response_iss_parameter_supported`, a response without `iss` is
refused. Without that second rule, a response stripped of the parameter would
pass. The check runs before the code is redeemed, so a code meant for another
provider never reaches this provider's token endpoint together with this
attempt's PKCE verifier. Keycloak advertises the parameter and sends it.

### The issuer is stored beside the link

`user_identities.issuer` was added in migration 000034. A new OIDC link
records the issuer. A link whose recorded issuer differs from the one
asserting it now is refused with `identity.ErrIssuerMismatch`. The person
sees "sign-in failed", and the log tells the operator to restore the old
issuer or give the new one its own `--oidc-name`.

`NULL` means no issuer is recorded. LDAP links stay `NULL`. OIDC links made
before the migration are also `NULL`, because the configured issuer is not in
the database and nothing in SQL can backfill it. Such a link adopts the issuer
configured at its first sign-in after the upgrade, and is fixed from then on.
The alternative, refusing legacy links, would lock out everyone who had
already signed in. The exposure this leaves is narrow: an operator who changes
the issuer and upgrades in the same step hands legacy links to the new issuer,
which is exactly the situation before this ADR.

Rejected: **adding the issuer to the primary key.** A re-pointed provider
would then quietly link its subjects to existing accounts by address, case 2
of provisioning. The operator's mistake would become new links that nobody
decided to make, where it should be a refusal they can see.

### A key-fetch failure is an outage

go-oidc reports "the key set could not be fetched" and "no key verified the
signature" as the same untyped error, formatted with `%v`. The verifier is now
built by hand around a key set wrapper. The wrapper sees the key set's own
error before it is formatted, and records a fetch failure in a slot the caller
put in the context. A fetch failure becomes `ErrUnavailable`: the person sees
"the provider could not be reached", and no security event is written. The
wrapper recognises a fetch failure by go-oidc's own message prefix
(`fetching keys`). A test against a provider whose key endpoint fails pins
that prefix, so an upgrade that changes it fails the test instead of turning
outages back into rejections. The algorithms the verifier accepts are still
filtered the way `provider.Verifier` filters them.

### A sign-in ends the browser's previous session

`startSession` revokes the session in the request's `cc_session` cookie once
the new session exists. Every sign-in uses `startSession`: password,
directory, token, first-run setup and OIDC. Only a credential of type
`session` is revoked, so a PAT pasted into the cookie by hand is left alone.
Without this, the previous token stays valid for the rest of its twelve hours,
held by nobody the person can see.

### State-mismatch events get no bound of their own

The cheap bound is a global one, and it was considered and rejected. Once it
was exhausted it would either refuse callbacks, which lets anyone lock
everyone out of OIDC, or skip recording events, which lets anyone silence the
security log. `internal/security` rules out the second ("off-switch, not
sampling"). A mismatch now requires an attempt this server signed, so a
planted cookie can no longer produce one. Anyone can still produce them by
calling `/start` first. A per-IP bound would cover them and failed password
sign-ins alike, and it waits on the trusted-proxy item in the backlog, as
before.

## Consequences

- Entra ID can sign people in once the app registration releases `xms_edov`.
  This is tested only against tokens shaped like Entra's from a fake
  provider, not against a live tenant.
- Adding a second provider no longer has mix-up as a blocker. What remains is
  the configuration surface: the flags configure one provider.
- Rotating the root key now also expires OIDC attempts in flight. Any future
  derived key follows the same pattern: a new `crypto.Purpose` with its own
  versioned info string.
- `RedirectAuthenticator.Exchange` takes a `directory.Callback` (code and
  `iss`), and `directory.Identity` carries the issuer.
- An attempt cookie from before this change is ignored. Its name no longer
  has the prefix the server looks for, and it expires on its own within ten
  minutes.
