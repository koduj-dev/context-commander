# 0041. Passwords an administrator sets are temporary

## Status

Accepted, 2026-11-18. Closes "no must change at next sign-in flag" in NEXT.md.

## Context

An administrator can give someone a password: when creating their account,
or by resetting it (ADR 0019). That password was then permanent. The
administrator knew it, it had travelled over chat or email, and nothing
made its owner replace it. NEXT.md warned that half of this feature is worse
than none: a flag nothing enforces reads as a promise the product does not
keep.

## Decision

**Every password an administrator sets is temporary.** `user_passwords`
gains `must_change` (migration 000049); `POST /v1/installation/users` with a
password and `PUT /v1/installation/users/{id}/password` set it. The owner's
own change (`PUT /v1/me/password`) clears it. The first administrator's
password from first-run setup, and `ccd user set-password` run by an
operator, stay permanent: nobody else chose them.

**Enforced on every request, at no extra cost.** The credential lookup that
authenticates every request reads the flag in the same query, for browser
sessions only. A session holding a temporary password may `GET /v1/me` and
`PUT /v1/me/password`; every other request answers `403
password_change_required`, and MCP refuses it outright. A person's tokens
are not sessions and keep working: the flag is about the password, and a
token was never derived from it.

**The interface follows the server.** `/v1/me` reports
`must_change_password`, and the interface then shows the password form, a
sign-out button and nothing else, rather than a dashboard of refusals.

## Consequences

- A password that passed through an administrator's hands is used once.
- An administrator who resets a password locks the person out of
  everything but the change until they make it, which is the point.
