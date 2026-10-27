# A Keycloak to develop OIDC sign-in against

Opt-in, like `ldap` and `s3`:

    docker compose -f deploy/docker-compose.yml --profile oidc up -d

It serves the realm `context-commander` on <http://127.0.0.1:18081>, imported
from `context-commander-realm.json` on first start. The admin console is
`admin` / `keycloak_dev_only`; the client is `context-commander` with the
secret `oidc-dev-client-secret`. All of that is committed on purpose — it
belongs to a container that exists for minutes, and a fixture nobody can read
is a fixture nobody can fix.

## Why Keycloak and not a mock

Verifying the ID token's signature is the security core of OIDC. A
hand-written mock would mean checking our verifier against our own idea of
what a token looks like, which proves nothing. Keycloak is a real provider
with real keys, real discovery and real rotation.

The cost is that it is a much heavier fixture than slapd — the image is
several hundred megabytes and takes tens of seconds to become ready — which
is why it sits behind a profile and why `make dev-up` still starts only
Postgres.

## Three things measured rather than assumed

**The import file must be named `<realm>-realm.json`.** Any other name and
Keycloak refuses to start at all:

    ERROR: File name / realm name mismatch. cc-realm.json, contains realm
    context-commander. File name should be context-commander-realm.json

Loud and specific, at least — but only visible in the container's log, so a
`--rm` diagnostic run destroys the evidence.

**A user with no email cannot authenticate.** Keycloak 26's declarative user
profile marks email required, so such a user is "not fully set up" and the
token endpoint answers:

    400 {"error":"invalid_grant","error_description":"Account is not fully set up"}

There is therefore no seeded user without an email. The server's
"the provider released no email" path is exercised by requesting a token
**without the `email` scope** instead, which yields an ID token carrying no
email claim. The server cannot tell the two apart, and this needs no fixture.

For that to work the client lists `email` under `optionalClientScopes`, not
`defaultClientScopes`, and this is the part worth knowing: **a default client
scope is granted whether or not it was requested.** With `email` left among
the defaults, asking for `scope=openid` still returns a full email claim —
measured, after this file first claimed otherwise. Default scopes are not
defaults in the sense of "unless you ask for something else".

**A `sub` is stable only for the life of the container.** `start-dev` keeps
the realm in an in-memory H2 database, so every recreation re-imports the
users and mints new ids. A `user_identities` row in a dev database therefore
points at a subject that no longer exists after the provider is restarted,
and that account stops being reachable through OIDC — correctly, since by the
provider's reckoning it is a different person. Expect to re-link, or to drop
those rows, after recreating this container. Nothing to fix; just not what
"stable identifier" leads you to expect locally.

**Keycloak lowercases email addresses.** A user seeded as `Dave@Example.Test`
comes back as `dave@example.test` in the ID token. So this directory cannot
produce the mixed-case address that `users_email_lower_key` exists for — the
LDAP fixture can, because slapd stores `mail` as given.

`dave` is kept for the other direction instead: the local account is created
with a mixed-case email and the provider sends lowercase, which is the same
linking path seen from the other side.

## The users

| user | password | purpose |
|---|---|---|
| `alice` | `oidc-dev-password` | just-in-time provisioning of a brand new account |
| `bob` | `oidc-dev-password` | a second account, for linking and listing |
| `dave` | `oidc-dev-password` | links to a local account whose email differs in case |
| `erin` | `oidc-dev-password` | **email not verified** — the server must refuse to provision her, because provisioning links to existing accounts by address |

Unlike a missing email, an unverified one *can* be seeded: with the realm's
`verifyEmail` off, Keycloak lets `erin` sign in and issues an ID token with
`email_verified: false`, which is exactly the case to refuse.

## Running the server against it

    printf 'oidc-dev-client-secret\n' > /tmp/oidc-secret
    ./bin/ccd serve --addr 127.0.0.1:12001 \
      --oidc-issuer http://127.0.0.1:18081/realms/context-commander \
      --oidc-client-id context-commander \
      --oidc-client-secret-file /tmp/oidc-secret \
      --oidc-redirect-url http://127.0.0.1:12001/v1/auth/oidc/callback \
      --oidc-name keycloak --oidc-label Keycloak

Port 12001 because that is what the realm registers as a redirect URI, and a
provider matches redirect URIs byte for byte: `localhost` and `127.0.0.1` are
two different callbacks, and both are registered so either works — but the
`--oidc-redirect-url` you pass must be the one the browser will actually be
sent back to.

The issuer must be spelled the way discovery reports it. `start-dev` derives
it from the request's host, so asking for `http://localhost:18081/...` and
then browsing to `127.0.0.1` gives two issuers, and the ID token is refused.

Sign-in must request `openid profile email`. Asking for less is how the
no-email path above is reached, so it is not a safe default here.
