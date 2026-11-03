# Signing in through OpenID Connect

`ccd serve` can offer single sign-on through one OpenID Connect provider:
authorization code flow with PKCE, endpoints and keys from the provider's
discovery document. The decisions behind it are in
[ADR 0019](adr/0019-browser-authentication-and-directory-identity.md),
[ADR 0020](adr/0020-oidc-sign-up-is-off-by-default.md) and
[ADR 0023](adr/0023-oidc-hardening.md). This page is the operator's half: what
to register at the provider and which flags to pass.

## What every provider needs

At the provider, register a **confidential web client** with one redirect URI:

    https://<your public address>/v1/auth/oidc/callback

Then start the server with:

| Flag | Value |
|---|---|
| `--oidc-issuer` (or `$CC_OIDC_ISSUER`) | The issuer exactly as the provider's `/.well-known/openid-configuration` states it. Discovery refuses a document whose `issuer` differs, even by a trailing slash. |
| `--oidc-client-id` | The client id. |
| `--oidc-client-secret-file` (or `$CC_OIDC_CLIENT_SECRET`) | The client secret. There is no flag for the secret itself, because an argument shows up in `ps`. |
| `--oidc-redirect-url` | The redirect URI above, byte for byte as registered. It is not derived from the request, because behind a reverse proxy the request's host is whatever the proxy chose. |
| `--oidc-label` | What the sign-in button says. |

The issuer and the redirect must be `https`, except on a loopback address.

Who gets in:

- **`--oidc-allow-signup`** lets a first sign-in create an account. It is off
  by default. Without it, a person gets in only if they are already linked, or
  if an administrator created an account under the same address.
- **`--oidc-allowed-domains example.com`** accepts only verified addresses in
  the listed domains. This is checked on every sign-in, matches exactly and
  includes no subdomains.

An address the provider has not marked `email_verified` is refused, because
the first sign-in links to an existing account by address.

### Things the server checks for you

- **The response's issuer (RFC 9207).** If the provider sends `iss` with the
  authorization response, it must be this issuer. If the provider's discovery
  document advertises `authorization_response_iss_parameter_supported`, a
  response without `iss` is refused. Keycloak advertises it.
- **The issuer beside each link.** Every account linked through OIDC records
  the issuer it was linked under. If you point `--oidc-issuer` at a different
  issuer but keep `--oidc-name`, people linked under the old issuer are
  refused. They are not silently matched to the new issuer's subjects, because
  a `sub` is unique only within the issuer that minted it. The log names the
  fix: restore the old issuer, or give the new one its own `--oidc-name`.
  Links made before this check existed record the issuer configured at their
  first sign-in after the upgrade.
- **The provider being down** is reported as "unavailable", not as a failed
  sign-in. That includes the provider's signing keys failing to load, for
  example right after a key rotation.

## Microsoft Entra ID

Entra ID does not send `email_verified`, so it needs one more switch, and that
switch names Entra on purpose.

At the app registration:

1. **Authentication → Web → Redirect URIs**: the callback above.
2. **Certificates & secrets**: create a client secret.
3. **Token configuration → Add optional claim → ID**: add `email` and
   **`xms_edov`**.

Then:

    --oidc-issuer https://login.microsoftonline.com/<tenant id>/v2.0
    --oidc-entra-verified-domain

`--oidc-entra-verified-domain` accepts an address when the ID token's
`xms_edov` claim is true. The server refuses to start with it for an issuer
that is not on an Entra host.

**Why the switch matters.** Entra's `email` claim is a directory attribute,
and Microsoft documents it as unverified. A tenant you do not control can put
any address on one of its own users. If that user signs in as a guest in your
tenant, Entra puts that address in the token. Accepting it would let the other
tenant's administrator sign in here as whichever of your users has that
address. This is the attack known as "nOAuth". `xms_edov` closes it. Entra sets
it to true only when the tenant the account lives in has verified the
address's domain, or when the address belongs to a Microsoft account, a Google
account or a one-time-passcode guest. Facebook and SAML/WS-Fed guests never
get it. If the claim is not configured, every address reads as unverified and
nobody gets in. That is the safe way to fail.

If you also want to keep guests from other organisations out, set
`--oidc-allowed-domains` to your own domains.

If the token does carry `email_verified`, that claim decides, and an explicit
false is never overruled.

This has been tested against tokens shaped like Entra's from a fake provider,
not against a live Entra tenant.

## Google

Google can be used for a company on Google Workspace. It is also an issuer
that **every Google account on the internet** can sign in with, so the two
"who gets in" flags matter more here than anywhere else.

At the Google Cloud console, under **APIs & Services → Credentials**, create
an **OAuth client ID** of type **Web application**. Add the callback above
under **Authorized redirect URIs**. Google allows `http` only for `localhost`.

    --oidc-issuer https://accounts.google.com
    --oidc-client-id <…>.apps.googleusercontent.com
    --oidc-client-secret-file /etc/context-commander/oidc-client-secret
    --oidc-redirect-url https://cc.example.com/v1/auth/oidc/callback
    --oidc-label Google
    --oidc-allowed-domains example.com

The default scopes (`openid profile email`) are what Google needs. It sends
`email_verified` as a boolean.

- **Leave `--oidc-allow-signup` off** unless `--oidc-allowed-domains` is set.
  With sign-up on and no domain list, anyone with a Google account can create
  an account here.
- **`--oidc-allowed-domains` checks the address, not the Workspace.** Google's
  `hd` claim, which names the Workspace an account belongs to, is not read
  ([ADR 0020](adr/0020-oidc-sign-up-is-off-by-default.md)). In most cases the
  address's domain is equivalent, with one known exception. Someone who once
  had a mailbox at `example.com` can keep a *personal* Google account
  registered under that address, and Google still reports it as verified after
  the Workspace account is gone. That person's first sign-in through the
  personal account links by address to the local account. Removing someone
  from Workspace is therefore not enough. Also remove or disable their account
  here. Checking `hd` would close this, and it is listed in `NEXT.md`.

This configuration has not been tested against Google itself. The flow, the
checks and the flags are the ones tested against Keycloak. The Google-specific
lines above come from Google's documentation, not from a test.

## Keycloak, for development

`docker compose -f deploy/docker-compose.yml --profile oidc up -d` starts a
Keycloak with a realm, a client and seeded users. See
[`deploy/oidc/README.md`](../deploy/oidc/README.md).
