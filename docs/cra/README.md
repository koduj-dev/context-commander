# Cyber Resilience Act

The material Context Commander keeps for Regulation (EU) 2024/2847, the
Cyber Resilience Act (CRA). The maintainer decided on 2026-11-16 to prepare
it as if the product is in scope.

> **This is not legal advice and not a conformity assessment.** It is the
> manufacturer's working file: what the regulation asks of a product like
> this one, and where in the repository each answer lives. Before a version
> is placed on the market under the CRA, the manufacturer has to complete
> the placeholders marked **TODO(manufacturer)**, sign the declaration of
> conformity, and keep this file for ten years.

## Dates

| Date | What applies |
|---|---|
| 2026-09-11 | Reporting of actively exploited vulnerabilities and severe incidents (Art. 14), for products already on the market too |
| 2027-12-11 | Everything else: essential requirements, conformity assessment, CE marking, technical documentation |

## Is Context Commander in scope?

The CRA covers products with digital elements made available on the EU
market in the course of a commercial activity. Free and open-source software
developed or supplied outside a commercial activity is excluded (Recital 18,
Art. 3(48)), and an open-source *steward*, a legal person that supports such
software without placing it on the market itself, has lighter duties of its
own (Art. 24).

Which of these applies depends on facts about the maintainer, not about the
code: whether the software is monetised (sold, offered with paid support
that is the only way to use it, or used to process personal data for profit
beyond its security), and whether a company places it on the market. **The
maintainer has decided to treat it as in scope.** This file is written for
that case, with Context Commander's maintainer as the *manufacturer*.

**Category.** Context Commander's core function is sharing project context
between coding agents. It authenticates users (passwords, LDAP, OIDC) as
part of that, but it is not identity management or privileged-access
management software in the sense of Annex III, whose core function is that.
It is therefore a product in the default category, and the manufacturer may
assess conformity itself (internal control, module A, Annex VIII).
**TODO(manufacturer):** confirm this classification when the
Commission's implementing act describing the Annex III categories is
published.

## The files

| File | What it is |
|---|---|
| [technical-documentation.md](technical-documentation.md) | The technical documentation, in the order of Annex VII |
| [essential-requirements.md](essential-requirements.md) | Annex I, Part I: each security requirement and how the product meets it |
| [vulnerability-handling.md](vulnerability-handling.md) | Annex I, Part II and Art. 14: how vulnerabilities are found, fixed, disclosed and reported |
| [risk-assessment.md](risk-assessment.md) | The cybersecurity risk assessment (Art. 13(2)) |
| [declaration-of-conformity.md](declaration-of-conformity.md) | The EU declaration of conformity (Annex V), unsigned template |
| [../../SECURITY.md](../../SECURITY.md) | The public vulnerability disclosure policy and contact |

## Keeping it current

Update this file in the same pull request as the change that makes it
wrong: a new external interface, a new kind of stored data, a changed
default, a new dependency of note. The reviewer checks
[essential-requirements.md](essential-requirements.md) the way they check
an ADR.
