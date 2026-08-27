# Context Commander - Brand Assets

Version: 1.0
Date: 2026-08-27

## Core idea

Two independent speakers / agents form an almost closed frame around one shared
context core. The tails of the two speech bubbles point in opposite directions,
making the mark read as two distinct participants rather than one chat window.

The center is not an AI/vendor logo. It is a neutral context hub:
a focus ring, four framing brackets, and relay nodes linking both sides.

## Primary colors

- Context Pink: `#FF2D8D`
- Relay Violet: `#7A35F5`
- Void: `#0B0B12`
- Paper: `#FFFFFF`

No gradients are required. Primary assets are deliberately flat for clean SVG,
favicon, CLI, documentation and print use.

## Preferred assets

- `svg/cc-mark-on-light.svg`
- `svg/cc-mark-on-dark.svg`
- `svg/cc-logo-horizontal-on-light.svg`
- `svg/cc-logo-horizontal-on-dark.svg`
- `svg/cc-favicon.svg`

The horizontal SVG lockups ship with the wordmark converted to vector outlines. The construction font is Inter Bold; the font file is not required to render the supplied SVGs.

## Basic rules

- Do not rotate, skew, stretch or add drop shadows.
- Do not swap left/right brand colors.
- Do not close the inward-facing gaps of the two speech bubbles.
- Keep at least one context-core ring diameter of clear space around the mark.
- Below 48 px, prefer the simplified favicon variant.
- Brand violet is decorative on dark backgrounds; use white for small body text.

See `context-commander-brand-manual.pdf` for the full manual.

## Horizontal lockup note

The horizontal lockups are built so the icon remains fully contained inside the canvas and the wordmark sits to the right, vertically centered against the mark.

## Stacked lockup

Added stacked SVG lockups with the wordmark centered below the icon:

- `svg/cc-logo-stacked-on-light.svg`
- `svg/cc-logo-stacked-on-dark.svg`
- `svg/cc-logo-stacked-duotone-on-light.svg`
- `svg/cc-logo-stacked-duotone-on-dark.svg`

These are suitable for README headers, social cards, app splash screens and narrow layouts.

## Connector geometry note

The relay lines on both sides are intentionally connected toward the central context core with a more balanced, meaningfully symmetric construction. The mark should read as two participants talking through one shared context, not as a disconnected right-hand relay.

## Brand Manual v1.1

Layout cleanup: tighter horizontal SVG canvases, larger horizontal previews, corrected clear-space diagram, and compact typography / usage / implementation-note layouts.
