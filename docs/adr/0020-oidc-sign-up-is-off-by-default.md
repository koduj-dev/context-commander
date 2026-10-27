# 0020. OIDC sign-up is off by default; domains can be restricted

## Status

Accepted. Refines [ADR 0019](0019-browser-authentication-and-directory-identity.md)
for OpenID Connect; LDAP is unchanged.

## Context

ADR 0019 gave both directory backends the same shape: authenticate, look up
`(provider, subject)`, and on first success create the `users` row — just-in-time
provisioning. For LDAP that is right. A company's directory is closed by
nature: being in it is the company's decision, and everyone in it is someone
the company has already chosen.

An OpenID Connect issuer is not necessarily closed. The same implementation
that serves a company's own Keycloak serves Google, where the set of people
who can authenticate is everyone with a Google account. With just-in-time
provisioning, pointing this installation at Google made it an open
self-registration service: any stranger signs in, gets a local user, and a
local user may create organizations and projects. Nothing in the code or the
flags said so, and the `--oidc-label` help even suggested "Google". An
independent review of the OIDC work raised it on 2026-10-27, and the
maintainer chose the default.

## Decision

**A first OIDC sign-in does not create an account unless the operator says
so.** `--oidc-allow-signup` turns creation on; without it, provisioning runs
with `ProvisionPolicy{CreateMissing: false}` and reaches only:

1. an identity already linked by `(provider, subject)`, or
2. an account an administrator created under the same address, which the
   first sign-in links to.

Anyone else is refused with `ErrNoAccount`, nothing is written — no user, no
link — and the sign-in screen tells them an administrator has to create their
account. That is the deployment ADR 0019 describes ("a company installs this
and hands out access"): access is handed out, not taken.

**`--oidc-allowed-domains` restricts sign-in to verified addresses in the
listed domains**, on every sign-in rather than only the first, so narrowing
the list takes effect for people already linked. Exact match on the part
after the last `@`; no subdomains, no suffix matching — `example.com` admits
neither `mail.example.com` nor `evilexample.com`. The check runs after the
`email_verified` check, never before: an unverified address can claim any
domain it likes.

The two are independent. A company on Google Workspace typically wants both:
sign-up on, limited to its own domain.

## Consequences

- The safe configuration is the one you get by not thinking about it. Open
  registration requires a flag with "signup" in its name, and the server logs
  whether it is on at every start.
- Inviting someone is creating their account (`POST /v1/installation/users`)
  under the address they will sign in with. Nothing new had to be built for
  it, and the link is recorded on their first sign-in.
- Google's `hd` claim is not used. A verified address's domain says the same
  thing for Workspace accounts and also works for every other provider.
- LDAP keeps creating accounts. If a directory that is not closed ever needs
  supporting there, the policy type is already shared.
