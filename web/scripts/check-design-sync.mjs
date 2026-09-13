#!/usr/bin/env node
/**
 * Keeps the app's design system and the mockups from drifting apart.
 *
 * docs/mockups is where the design is agreed and reviewed; web/src/styles is
 * where it is implemented. They are two files, which means they will diverge
 * — quietly, one small fix at a time, until a screenshot in the mockups shows
 * something the product has not looked like for months and nobody trusts
 * either one. This script is the thing that notices.
 *
 * It checks four properties:
 *
 *   1. Every component class the mockups define exists in the app. A class
 *      that only lives in the mockups is a design that was agreed and never
 *      built.
 *   2. Neither stylesheet contains a hex colour. Colour comes from the brand
 *      token file; a literal is someone bypassing the brand.
 *   3. Every class the app's components use is defined in the app stylesheet.
 *      An undefined class renders as an unstyled element, silently.
 *   4. No component file carries a `style={{…}}` attribute. One inline style
 *      is a private decision; a hundred is how a kit turns back into a pile
 *      of pages. (The handful that are genuinely data — a meter's width —
 *      are listed as exceptions below and must stay tiny.)
 *
 * Run with `npm test`; it also runs in CI.
 */
import { readFile, readdir } from "node:fs/promises";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repo = join(here, "..", "..");
const MOCKUP_CSS = join(repo, "docs", "mockups", "assets", "mockup.css");
const APP_CSS = join(here, "..", "src", "styles", "app.css");
const SRC = join(here, "..", "src");

/** Classes that exist only to frame a mockup and have no place in the app. */
const MOCKUP_ONLY = new Set([
  "mockbar",
  "tag", // the "this is a mockup" strip
  "swatches",
  "sw",
  "chip",
  "meta", // the colour specimen grid
  "specimen",
  "spec-note", // type and component specimens
]);

/** Inline styles that are data rather than styling, and why. */
const INLINE_STYLE_ALLOWED = new Map([["Meter", "the fill width IS the value being displayed"]]);

const failures = [];
const notes = [];

function classesIn(css) {
  return new Set([...css.matchAll(/\.(-?[A-Za-z_][\w-]*)/g)].map((m) => m[1]));
}

const mockupCss = await readFile(MOCKUP_CSS, "utf8");
const appCss = await readFile(APP_CSS, "utf8");

// 1 — every mockup component must exist in the app
const missing = [...classesIn(mockupCss)]
  .filter((c) => !MOCKUP_ONLY.has(c))
  .filter((c) => !appCss.includes(`.${c}`))
  .sort();
if (missing.length) {
  failures.push(
    `The mockups define ${missing.length} class(es) the app does not:\n` +
      missing.map((c) => `    .${c}`).join("\n") +
      `\n  Either implement them in src/styles/app.css, or delete them from the mockups —\n` +
      `  a design nobody built is a design nobody can trust.`,
  );
}

// 2 — colour comes from the brand tokens, never from a literal
for (const [label, css] of [
  ["docs/mockups/assets/mockup.css", mockupCss],
  ["web/src/styles/app.css", appCss],
]) {
  const hex = [...css.matchAll(/#[0-9a-fA-F]{3,8}\b/g)].map((m) => m[0]);
  if (hex.length) {
    failures.push(
      `${label} contains ${hex.length} hard-coded colour(s): ${[...new Set(hex)].join(", ")}\n` +
        `  Colour belongs in docs/brand/tokens/ui-colors.css. A literal here is a second\n` +
        `  brand that will not follow when the first one changes.`,
    );
  }
}

// 3 and 4 — walk the app source
async function* walk(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const p = join(dir, entry.name);
    if (entry.isDirectory()) yield* walk(p);
    else if (/\.tsx?$/.test(entry.name)) yield p;
  }
}

const appClasses = classesIn(appCss);
const undefinedUses = new Map();
const inlineStyles = [];

for await (const file of walk(SRC)) {
  const src = await readFile(file, "utf8");
  const rel = relative(join(here, ".."), file);

  for (const m of src.matchAll(/className=(?:"([^"]*)"|\{`([^`]*)`\})/g)) {
    const raw = (m[1] ?? m[2] ?? "").replace(/\$\{[^}]*\}/g, " ");
    for (const c of raw.split(/\s+/).filter(Boolean)) {
      if (!appClasses.has(c)) {
        if (!undefinedUses.has(c)) undefinedUses.set(c, new Set());
        undefinedUses.get(c).add(rel);
      }
    }
  }

  for (const m of src.matchAll(/style=\{\{/g)) {
    void m;
    const component = rel
      .split("/")
      .pop()
      .replace(/\.tsx?$/, "");
    const allowedFor = [...INLINE_STYLE_ALLOWED.keys()].some((k) => src.includes(`function ${k}(`));
    if (!allowedFor) inlineStyles.push(rel);
  }
}

if (undefinedUses.size) {
  failures.push(
    `Components use ${undefinedUses.size} class(es) the stylesheet does not define:\n` +
      [...undefinedUses.entries()]
        .sort()
        .map(([c, files]) => `    .${c}  (${[...files].join(", ")})`)
        .join("\n") +
      `\n  An undefined class renders as an unstyled element and nothing warns you.`,
  );
}

if (inlineStyles.length) {
  failures.push(
    `Inline styles found in: ${[...new Set(inlineStyles)].join(", ")}\n` +
      `  Give the pattern a name in src/styles/app.css instead. If it is genuinely data\n` +
      `  (a meter's width), add it to INLINE_STYLE_ALLOWED in this script with a reason.`,
  );
}

// A count worth seeing even when everything passes.
notes.push(`design kit: ${appClasses.size} classes, ${classesIn(mockupCss).size} in the mockups`);

if (failures.length) {
  console.error("\n✗ design sync\n");
  for (const f of failures) console.error("  " + f + "\n");
  process.exit(1);
}
console.log(`✓ design sync — ${notes.join("; ")}`);
