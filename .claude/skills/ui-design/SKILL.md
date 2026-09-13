---
name: ui-design
description: How the Context Commander interface is built — the design kit, the brand rules, internationalisation, and the checks that keep the app and the mockups from drifting apart. Load before writing or changing any UI code, any stylesheet, or any screen in docs/mockups.
---

# Building the Context Commander interface

## The short version

There is one design system. It lives in two files that must agree:

```
docs/mockups/assets/mockup.css   the design, reviewed as static HTML
web/src/styles/app.css           the same system, in the app
```

`web/scripts/check-design-sync.mjs` fails the build when they disagree. Run
`npm test` in `web/` before you believe anything you changed.

## Where things live

| What | Where | Rule |
|---|---|---|
| Brand colours, logo | `docs/brand/` | Source of truth. Never copy by hand, never redraw. |
| Design system | `docs/mockups/assets/mockup.css` | Every component is decided here first. |
| Mockup screens | `docs/mockups/*.html` | Generated from `assets/_shell.py` + `assets/_kit.py`. |
| App stylesheet | `web/src/styles/app.css` | A port of the above, checked mechanically. |
| App components | `web/src/ui/index.tsx` | One React component per kit class. |
| Strings | `web/src/i18n/en.ts` | English is the source. Keys added here first. |
| Formatting | `web/src/lib/format.ts` | All of it through `Intl`. Never by hand. |

The brand package reaches the app through `web/scripts/sync-brand.mjs`, which
copies what the app renders into `public/brand/` at build time.
`public/brand/` is gitignored on purpose — a committed second copy of the logo
is a second logo, and the one people notice will be the stale one.

## The rules, and why each exists

**No hex colour outside the brand tokens.** Colour comes from
`docs/brand/tokens/ui-colors.css`. A literal is a second brand that will not
follow when the first one changes. The sync check fails on any `#rrggbb` in
either stylesheet.

**No component styles itself.** No `style={{…}}` in a component, no `style="…"`
in a mockup page. If a layout need recurs, it earns a named class in the
stylesheet. The exception is data — a meter's fill width *is* the value being
displayed — and those are listed explicitly in the sync check with a reason.
One inline style is a private decision; a hundred is how a kit turns back into
a pile of pages.

**Pink is the context you are in. Violet is context you inherit.** That is the
brand's one semantic rule, and it holds everywhere: the active nav rail, the
scope rails on skills, the override badges, the storage meter. Neither colour
fills a surface — the brand manual forbids large saturated areas, and the
palette is an accent system, not a background.

**Status never rides on colour alone.** Every badge carries a glyph and a
word. It has to survive greyscale and a colour-blind reader.

**Never fake a capability the backend lacks.** If an operation does not exist,
the control is absent or disabled *with a reason in its title*. A mockup or a
screen that promises member removal, secret rotation or SSO is a bill the
implementation has to pay. `docs/CURRENT-ARCHITECTURE.md` §4 lists what does
not exist.

**A refusal is not an empty list.** 403 renders as "you do not have access",
404 as "not found", 409 as "this changed while you were reading it — re-read,
merge, retry". Rendering a refusal as an empty table tells someone their
project is empty when the truth is that it is not theirs. `ErrorState` in
`web/src/ui/index.tsx` already does this; use it.

**Pagination is "load more", never page numbers.** Every growing list is
keyset-paginated: the server returns an opaque cursor and does not know how
many pages exist. A `1 · 2 · 3 … 47` control would be a promise the API cannot
keep.

## Internationalisation

Built in from the first commit, not retrofitted. Retrofitting it is a second
rewrite.

- **English is the source.** Add keys to `web/src/i18n/en.ts` first;
  `MessageKey` is derived from it, so a key with no English original is a
  compile error rather than a string reading `prefs.timeZone` in production.
- **Never assemble a sentence from fragments.** Word order is not universal
  and fragments cannot be reordered. One key, one whole sentence, with
  `{braces}` for variables.
- **Anything countable goes through a plural form**, not "(s)". Czech has
  three plural forms and Polish four; `Intl.PluralRules` knows them.
- **Dates, times, numbers and byte sizes are not strings.** They are formatted
  from raw values in `lib/format.ts`. The server sends RFC3339 and integer
  bytes precisely so this layer can decide — 2.4 GB and 2.24 GiB are the same
  2,400,000,000 bytes and which one a reader sees is theirs to choose.
- **Localisation stops at the UI.** What leaves over HTTP and MCP stays in one
  machine format. Its reader is a model, not a person.
- **Measure the layout in the longer language.** German runs roughly 30% past
  English and these screens are dense. Nothing may be pinned to a width that
  only fits English: table headings wrap rather than clip, badge rows wrap,
  labels grow past their minimum. `docs/mockups/foundations.html` renders the
  same components in both languages side by side — if the German column
  breaks, the component is wrong, not the translation.

## Changing the design

1. Change `docs/mockups/assets/mockup.css` and regenerate the mockup pages.
2. Port the same change into `web/src/styles/app.css`.
3. Add or update the React component in `web/src/ui/index.tsx`.
4. Run `npm test` in `web/`. The sync check will tell you what you missed.

If a component exists only in the mockups, the check fails: a design nobody
built is a design nobody can trust. If it exists only in the app, it was never
reviewed.

Anything below the `foundations page only` marker in `mockup.css` documents
the kit rather than being part of it, and is not ported. **Keep that block
last** — a component added after it is silently dropped from the app.

## Where the design is already decided

`docs/mockups/` has 23 screens and `foundations.html` documents the kit.
Before designing a screen, check whether it is already there. The navigation,
the information architecture and the answer to "is this a management plane or
a database browser" are settled in `docs/CURRENT-ARCHITECTURE.md` §6: domain
primitives are not automatically navigation items.
