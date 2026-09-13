# Mockups

Static HTML mockups of the Context Commander management plane. Open
[`index.html`](index.html) — no build step, no dependencies.

## Read this first

[`foundations.html`](foundations.html) is the base. Tokens, type scale,
every component and the rules the kit enforces. **Every other screen is
assembled from it**, and a screen that needs markup the kit cannot express is
a signal the kit is missing something — not a licence to improvise on that
page.

## How it is built

- **Colour** comes from [`../brand/tokens/ui-colors.css`](../brand/tokens/ui-colors.css),
  imported directly. `assets/mockup.css` contains **no hex values** — change a
  brand token and every screen follows.
- **The logo** is referenced from [`../brand/svg/`](../brand/svg/). It is not
  redrawn, recoloured or replaced.
- **Pages are generated**, not hand-written: `assets/_shell.py` is the shell
  and navigation, `assets/_kit.py` is the component library. That is what
  keeps sixteen screens from becoming sixteen experiments.
- **No inline styles.** If a layout need recurs it earns a utility class in
  the stylesheet. The only `style` attributes left are data — a swatch's
  colour, a meter's width.
- **Dark is the default**, per the brand manual; every page has a light
  toggle in the top strip.

## What these deliberately do not do

- **Not a database browser.** Artifacts, events, conversations and history
  objects appear where they answer a management question, not as one tab per
  table. See `docs/CURRENT-ARCHITECTURE.md` §6.
- **No capability the backend lacks.** Member removal, secret rotation,
  SSO/LDAP and integrations are shown absent or disabled *with the reason*,
  because promising them here is a bill the implementation has to pay.
- **No invented events.** Activity screens show only action names the server
  actually emits.

## Regenerating

```bash
cd docs/mockups
python3 assets/_shell.py   # shell + navigation
# page scripts live alongside; see git history for the generators used
```

The HTML is committed, so nothing needs to run to read them.

## Status

These are a design proposal, not an implementation plan and not the current
UI. The shipped web UI in `web/` still has the older project-tab layout; this
is what the rework is aiming at.
