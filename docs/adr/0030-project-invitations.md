# 0030. Project invitations, accepted at sign-in

## Status

Accepted. Refines [ADR 0024](0024-sign-in-rules-managed-by-an-administrator.md),
whose rules grant organizations but never projects.

## Context

A sign-in rule makes a new account a member of organizations. Project access
is project membership only (ADR 0006), so that account still reached no
project until an owner added it by principal id. An owner can only add a
principal that already exists, which means waiting for the person's first
sign-in and then finding their id.

Letting a rule grant projects was considered. The administrator would pick
projects from a list of every project on the installation, which reverses the
maintainer's 2026-10-27 decision that the admin tier does not see project
paths. On 2026-11-13 the maintainer chose the GitHub/GitLab model instead:
invite an address in advance, and the invitation is accepted when that
person first signs in.

## Decision

- Anyone holding `member.manage` on a project invites an email address with a
  role (`project_invitations`, migration 000041). OWNER additionally needs
  `member.grant_owner`, as for a direct add. An invitation expires after 14
  days by default, 90 at most. It can be revoked. Inviting the same address
  again replaces the live invitation.
- **Acceptance is automatic, at every browser sign-in** (`startSession`): a
  new account accepts on its first sign-in, an existing one on its next. The
  `SECURITY DEFINER` function `cc_accept_invitations` reads the address from
  the principal's own `users` row, never from the caller, so nothing can
  accept an invitation meant for someone else. A principal that is already a
  member keeps its role, because an invitation adds access and never changes
  a role someone set since. Each membership it adds is audited.
- **The inviter is not told whether the address has an account.** Adding an
  existing user immediately would answer "added" for some addresses and
  "invited" for others, which would let anyone who can invite enumerate the
  installation's users.
- **An invitation admits its address at OIDC sign-in and permits creating the
  account**, even when the sign-in rules would not admit it and sign-up is
  off. Like an address rule, it names one address and is someone's consent
  to an account for it. Only a verified address is ever compared (ADR 0020).
- Invitations are listed and revoked over HTTP and in the project's Members
  tab, by the same people who may invite. They are not offered to agents
  over MCP.

## Consequences

- An invitation is a way into the installation, granted by project owners
  rather than administrators. That is how GitHub and GitLab work, and
  revocation and expiry bound it. An installation that wants only
  administrators to decide who gets in should not grant `member.manage`
  widely.
- Someone who signs in only with a PAT (an agent on a user's token) accepts
  nothing until that user signs in through the browser once.
- Organization invitations are not covered. Organizations are reached
  through sign-in rules.
