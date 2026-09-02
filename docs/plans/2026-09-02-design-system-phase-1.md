# Design system + component library (spec phase 1)

**Status:** approved, not yet implemented — pick this back up next session.
**Date:** 2026-09-02

## Context

`docs/context-commander-ui-spec.md` (added by the maintainer) describes a
UI far larger than what exists — a proper brand/theme system, a shared
component library, new top-level nav, new project tabs, and an
"Administration" surface for user/agent provisioning that needs a brand-new
authorization tier (confirmed via audit: no installation-admin role exists
anywhere in `internal/auth`). Given the size, the maintainer chose to phase
this rather than build it all at once, and picked **design system +
component library first** — everything else in the spec depends on it, and
building later phases before it would mean redoing every page's styling
twice.

This phase touches **no backend, no new pages/tabs, no routes**. It's:
wire `docs/brand/tokens/*` into Tailwind so brand colors are real utility
classes, add System/Light/Dark theme switching, use the actual brand SVGs
(spec §2 is explicit: never redraw the logo), and build the handful of
`docs/context-commander-ui-spec.md` §25 components that have a real current
consumer — then retrofit the 8 existing pages + `Shell.tsx` onto both.
Components with no current consumer (VersionTimeline, EventFeed, TrustBadge,
GateDecisionBadge, ClientBadge, DangerZone — all need pages/tabs that don't
exist yet, like Events or Settings) are deliberately deferred to whichever
later phase builds those pages, not built speculatively now.

## 1. Token wiring

Copy (not import) `docs/brand/tokens/ui-colors.css` and `brand-colors.css`'s
values into a new `web/src/theme/tokens.css`, with a comment pointing back
at the `docs/brand/tokens/` source of truth. Copying, not a cross-directory
`@import`, because Vite's dev server restricts serving files outside its
project root (`web/`) by default — copying avoids widening `server.fs.allow`
for two small, stable files.

Three CSS blocks, in this order (source-order matters for the cascade):

```css
:root { /* light values, verbatim from ui-colors.css's :root block */ }

@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) { /* dark values, NEW — the source file
    only ships :root and [data-theme="dark"], no system-preference block */ }
}

[data-theme="dark"] { /* dark values, verbatim — explicit override wins
  regardless of OS preference */ }
```

`web/src/index.css` becomes:

```css
@import "tailwindcss";
@import "./theme/tokens.css";

@theme {
  --color-cc-bg: var(--cc-bg);
  --color-cc-surface-1: var(--cc-surface-1);
  /* ...one line per --cc-* token in tokens.css (surface-2/3, border,
     border-strong, text, text-secondary, text-muted, accent-primary,
     accent-primary-visual, accent-secondary, accent-secondary-visual,
     focus-ring, success, warning, danger, info, context-pink, relay-violet,
     void, paper, mist, muted) — mechanical, not enumerated further here. */
}

body { margin: 0; }
```

This makes `bg-cc-surface-1`, `text-cc-danger`, `border-cc-border` etc. real
Tailwind utilities that stay theme-reactive at runtime, because the
generated utility CSS references `var(--color-cc-danger)`, which itself
resolves through `var(--cc-danger)` — a custom property the three blocks
above keep swapping. Drop the dead `class="dark"` on `<html>` in
`web/index.html` (nothing reads it today, `ThemeContext` replaces it with
`data-theme`).

## 2. ThemeContext

New `web/src/theme/ThemeContext.tsx`, same shape as the existing
`web/src/auth/AuthContext.tsx`: `type Theme = "system" | "light" | "dark"`,
persisted at `localStorage` key `cc_theme` (same try/catch-for-private-
browsing style as `web/src/lib/auth.ts`). Exposes
`{ theme, setTheme, resolvedTheme }` — `resolvedTheme: "light"|"dark"` is
computed via `window.matchMedia("(prefers-color-scheme: dark)")` when
`theme === "system"` (subscribed to `change` so a live OS toggle updates
without reload), otherwise mirrors `theme` directly. On change, sets
`document.documentElement.dataset.theme` (or deletes it for `"system"`).

`ThemeProvider` wraps the app in `web/src/main.tsx` alongside the existing
`AuthProvider`. A small System/Light/Dark toggle (icon-only, `title`
tooltips — no new icon library, reuse the existing "no novelty font, mostly
neutral" tone) goes in `Shell.tsx`'s sidebar footer, next to sign-out.

**Test impact**: `web/src/layout/Shell.dom.test.tsx` renders `<Shell />`
directly and already `vi.mock`s `../auth/AuthContext` for `useAuth` — once
`Shell.tsx` calls `useTheme()`, this test needs the identical treatment:
`vi.mock("../theme/ThemeContext", () => ({ useTheme: () => ({ theme: "dark", setTheme: vi.fn(), resolvedTheme: "dark" }) }))`.

## 3. Logo and favicon

New `web/public/brand/` (no `public/` dir exists yet) holding copies of
`cc-logo-horizontal-on-dark.svg`, `cc-logo-horizontal-on-light.svg`, and
`cc-favicon.svg` from `docs/brand/svg/`. Vite serves `public/` at the root
path, so `Shell.tsx`'s header swaps the current plain-text "Context
Commander" for `<img src={resolvedTheme === "dark" ? "/brand/cc-logo-horizontal-on-dark.svg" : "/brand/cc-logo-horizontal-on-light.svg"} alt="Context Commander" className="h-6" />`
inside the existing `<Link to="/">`. `web/index.html` gets
`<link rel="icon" href="/brand/cc-favicon.svg" />`.

## 4. Component library

New `web/src/components/`, one file per component, function components,
inline Tailwind classNames — matching every existing page, no CSS modules,
no new dependency.

- **`PageHeader`** `{ title: string; description?: string; actions?: ReactNode }`
- **`ProjectContextHeader`** `{ namespace: string; slug: string; name: string; visibility: string; role: string }`
  — the "namespace/slug · name · your role: X" strip, built on `PageHeader`.
- **`DataTable<T>`** `{ columns: { key: string; header: string; render?: (row: T) => ReactNode }[]; rows: T[]; getRowKey: (row: T) => string; emptyState: ReactNode }`
  — adopt for `Members.tsx` and `Audit.tsx` only (genuinely multi-column
  tabular data). `Gates.tsx`/`Skills.tsx`/`Secrets.tsx` stay flex-`<li>`
  lists with an inline action button — forcing those into `DataTable`'s
  column config would add boilerplate for a shape that isn't really tabular.
- **`StatusBadge`** `{ tone: "success" | "warning" | "danger" | "info" | "neutral"; children: ReactNode }`
  — colored pill using the `cc-success/warning/danger/info` tokens; caller
  supplies the label text (e.g. "revoked", "active", "allow"), the
  component only owns color.
- **`ScopeBadge`** `{ scope: "project" | "organization" | "installation" }`
  — matches `GateBinding`/`SkillBinding`'s actual `scope` field values
  directly (no translation layer): project→neutral, organization→violet,
  installation→neutral gray, per UI-COLORS.md §4.
- **`PrincipalBadge`** `{ principalId: string; email?: string; displayName?: string }`
  — renders `displayName ?? email ?? principalId.slice(0,8)+"…"`; when
  neither `email` nor `displayName` is set (a `ServicePrincipal`, which
  never resolves to a `User` row — same absence `Members.tsx`/`Audit.tsx`
  already handle today), appends a small neutral "service" tag.
- **`EmptyState`** `{ message: string }` — replaces the repeated
  `<li className="px-3 py-2 text-neutral-500">No X yet.</li>` /
  `<p className="text-neutral-500">...</p>` pattern across every list page.
- **`ConfirmDialog`** `{ open: boolean; title: string; message: ReactNode; confirmLabel?: string; danger?: boolean; onConfirm: () => void; onCancel: () => void }`
  — controlled (parent owns `open`), rendered via `createPortal` to
  `document.body` (project tab content is inside `ProjectDetail.tsx`'s
  `overflow-y-auto` main area — portaling avoids clipping/stacking issues).
  Must not touch any page's existing `refresh()`/mutation logic — it only
  gates the button `onClick`; the actual `onDelete`/`onRevoke`/`onUnbind`
  functions move from the button's `onClick` to the dialog's `onConfirm`,
  unchanged otherwise.

## 5. Retrofit

Two tiers:

- **Structural**: `web/src/main.tsx` (wrap `ThemeProvider`), `Shell.tsx`
  (logo, theme toggle, `cc-*` tokens replacing `neutral-*`), `index.html`
  (favicon, drop `class="dark"`).
- **Pages** (`Members.tsx`, `Artifacts.tsx`, `Secrets.tsx`, `Skills.tsx`,
  `Gates.tsx`, `Audit.tsx`, `Dashboard.tsx`, `ProjectDetail.tsx`,
  `OrganizationDetail.tsx`, `AccountTokens.tsx`, `Login.tsx`): mechanical
  `neutral-*`/`red-400`/`emerald-400`/`amber-400` → `cc-*` token substitution
  throughout, plus adopting whichever of the 7 components fit naturally.
  Representative examples (the rest follow the same pattern, not enumerated
  per-file):
  - `Secrets.tsx`: `EmptyState` for "No secrets yet.", `ConfirmDialog`
    wraps the Delete button, error `<p>` moves to the `cc-danger` token.
  - `Members.tsx`: `<ul>` → `DataTable`, `PrincipalBadge` for the principal
    cell, `ScopeBadge`/`StatusBadge` where relevant.
  - `AccountTokens.tsx`: `ConfirmDialog` wraps Revoke, `StatusBadge` for
    revoked/active/no-expiry state instead of raw text.
  - `Gates.tsx`: `ConfirmDialog` wraps Unbind, `ScopeBadge` for the
    `(project)`/`(organization)` scope suffix currently shown as plain text.

## 6. Test impact

- `web/src/pages/project/Gates.test.tsx` — the "unbind a gate" case needs a
  dialog-confirm click inserted between clicking "Unbind" and asserting
  "No gates bound."
- `web/src/pages/AccountTokens.test.tsx` — the "revokes it" case needs the
  same insertion between clicking "Revoke" and asserting the revoked state.
- `web/src/layout/Shell.dom.test.tsx` — add the `ThemeContext` mock (§2).
- `web/src/pages/project/Secrets.test.tsx` **does not exist today** — add
  it now, since this phase adds Secrets' first real interaction behavior
  change (confirm-then-delete) and the mock-fetch + confirm-click pattern
  already exists to copy from `Gates.test.tsx`/`AccountTokens.test.tsx`.
- New component tests are optional/light-touch — these are presentational
  (`StatusBadge`, `ScopeBadge`, `PrincipalBadge`, `EmptyState`) or already
  exercised indirectly through the page tests above (`ConfirmDialog`,
  `DataTable`); no dedicated `ThemeContext.test.tsx` needed since nothing
  outside the (mocked-in-tests) `Shell.tsx` consumes it yet.

## Verification

- `cd web && npx tsc -b` — type-check.
- `cd web && npx vitest run` — full suite, including the updated/new tests
  above.
- `cd web && npm run build` — production build, rebuild `web/dist` (this
  repo commits `web/dist`, embedded via `go:embed` — CI's `go build` has no
  npm step, so the committed dist must be rebuilt and included in the
  commit, same as every prior feature this session).
- Manual: `ccd serve --dev` + `cd web && npm run dev`, log in, toggle
  System/Light/Dark and confirm the app actually repaints (not just the
  toggle state), confirm the real brand logo renders (not a placeholder),
  and click through Delete secret / Revoke token / Unbind gate to confirm
  the new confirm dialogs actually block the action until confirmed.
- `golangci-lint run ./...` / `go build ./...` are irrelevant here (no Go
  files touched) — skip.
