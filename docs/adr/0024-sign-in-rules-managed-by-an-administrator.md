# 0024. Sign-in rules are managed by an administrator, and a rule can grant organizations

## Status

Accepted. Refines [ADR 0019](0019-browser-authentication-and-directory-identity.md)
and [ADR 0020](0020-oidc-sign-up-is-off-by-default.md) for OpenID Connect.
LDAP is unchanged.

## Context

On 2026-11-13 the maintainer's team decided to run Context Commander for
real, with Google sign-in. They asked for three things that the flags could
not give them:

1. **The list lives in the administration, not in a flag.** Changing
   `--oidc-allowed-domains` means editing a unit file and restarting the
   server, and the person who decides who gets in is not always the person
   who runs the server.
2. **Domains and individual addresses.** A whole company domain, plus a
   contractor whose address is in nobody's domain.
3. **Each entry names the organizations a new account joins.** Someone from
   the company domain should arrive in the company's organization and see its
   projects. Before this, a new account was an empty account until an
   organization owner added it by hand.

Google brought a fourth. `--oidc-allowed-domains` compares the domain of the
address. A person who leaves a Google Workspace can keep a *personal* Google
account registered under their old work address, and Google keeps reporting
that address as verified. The flag admitted that account, and provisioning
then linked it by address to the person's local account. `docs/oidc.md` told
operators to disable the local account as a workaround, and `NEXT.md` listed
"check Google's `hd` claim" as open.

## Decision

### Rules

An installation administrator keeps a list of **sign-in rules**
(`signin_rules`, migration 000036). A rule is either:

- a **domain**, which admits every verified address in it, by exact match on
  the part after `@` (no subdomains, no wildcards: `*.example.com` is refused
  rather than accepted and silently not matched), or
- an **address**, which admits exactly that address.

Values are lowercased. Each rule names zero or more organizations
(`signin_rule_organizations`), at most 50.

The rules and `--oidc-allowed-domains` are **one allowlist**. Either half
admits; neither refuses what the other admits. The check moved out of
`directory.OIDC.Exchange` into the HTTP callback for that reason. `Exchange`
still verifies the address before anything compares a domain. With nothing
listed in either place, everyone the provider verifies is admitted, as
before. The first rule therefore closes the door to everyone it does not
name, and the screen says so before anyone adds one. The check runs on every
sign-in, so removing a rule takes effect for people already linked.

Managing rules takes a new installation capability, `CapSignInManage`. It
is separate from the others because it is the only installation power that
reaches inside an organization: it decides who joins one.

### A rule is consent to accounts

A matching rule permits creating an account even with `--oidc-allow-signup`
off. ADR 0020 turned sign-up off by default because an issuer like Google
authenticates everyone on the internet. A rule is an administrator naming the
people they want, which is the consent ADR 0020 was waiting for. A domain
admitted only by the flag still needs `--oidc-allow-signup` to create
accounts, as before.

### A rule grants organizations on creation only

When a rule creates an account, the account becomes a `MEMBER` of every
organization named by every rule that matches it, in the same transaction
that creates the user (`ProvisionPolicy.AfterCreate`). An account never
exists without the membership its rule promised.

The grant is made by `cc_signin_grant_memberships`, a `SECURITY DEFINER`
function. Provisioning has no principal yet, so `organization_members`' RLS
would refuse the insert. The function matches the rules against the address
itself rather than taking organization ids from its caller, so a bug in the
caller cannot grant an organization no rule names. It writes an
organization-scoped audit row for each membership it adds.

The grant applies **only to accounts the rule creates**:

- An account that already existed, linked or created by an administrator,
  keeps exactly the memberships it had. Organization membership belongs to
  the organization, and a rule added later is not a way to change it.
- Changing a rule's organizations affects accounts created from then on.
  People who joined through it stay members, because removing a member is
  the organization's decision.

ADR 0019 said a directory proves who someone is, not what they may do. That
still holds. The directory decides nothing. The grant is one an administrator
wrote down in advance.

### Google: the Workspace is checked

For the issuer `https://accounts.google.com`, the `hd` claim is read. It
names the primary domain of the Workspace an account belongs to, and a
personal account has none. **A domain admits a Google account only when its
`hd` is also listed**, as a rule or in the flag. On by default for Google.
`--oidc-google-ignore-hosted-domain` turns it off, and its help text says
what that readmits. The claim is not read from any other issuer.

- An address rule needs no Workspace. It names one person, and the
  administrator removes it when that person leaves. Address rules are how a
  contractor's `@gmail.com` account gets in.
- When an account is admitted without a listed Workspace, only address rules
  grant organizations (`domain_trusted = false`).
- A Workspace with several domains must list each domain whose addresses
  should be admitted, as well as the primary domain that `hd` names.

This closes the case `docs/oidc.md` described: the personal account carries
no `hd`, so a domain no longer admits it.

## Consequences

- An organization's owner should know that an administrator's rule can add
  members to it. The organization's audit trail shows each addition.
- Rule changes are not written to the security log. That log records refusals
  (internal/security), and a rule change is an administrative act, like a
  storage limit.
- Only one OIDC provider is configured per installation today. Rules apply to
  whichever one is configured. If several providers are ever supported, a rule
  may need to name its provider.
- Tested against tokens shaped like Google's from a fake provider, not
  against Google itself.
