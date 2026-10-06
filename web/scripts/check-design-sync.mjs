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
 *   5. No rule sets a flex-only property on something that is not a flex or
 *      grid container. `justify-content` on a block is the quietest failure
 *      in CSS: no error, no warning, no effect. It left the sign-in lockup
 *      against the card's left edge, and check 1 waved it through because the
 *      class existed in BOTH stylesheets — identically wrong in each.
 *      Existence is not geometry, and this is the part of geometry that can
 *      be checked without rendering anything.
 *
 * What it deliberately does NOT enforce: that every screen in the app has a
 * matching page in docs/mockups. The mockups were how the design was agreed
 * before the app existed; screens built after it are reviewed in the running
 * app, which is a better review than a static copy of one. The contract that
 * actually prevents drift is the component one above, and that is enforced.
 * The screen inventory is printed as a note so the gap stays visible rather
 * than being quietly forgotten.
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
const INLINE_STYLE_ALLOWED = new Map([
  ["Meter", "the fill width IS the value being displayed"],
  ["SelectField", "a popup's position IS the trigger's position, and no class can know it"],
]);

/** Properties that do nothing at all unless the box is a flex or grid container. */
const FLEX_ONLY = [
  "justify-content",
  "align-items",
  "align-content",
  "flex-direction",
  "flex-wrap",
  "place-items",
  "place-content",
];
const FLEX_DISPLAY = new Set(["flex", "grid", "inline-flex", "inline-grid"]);

/**
 * Modifier classes that carry a flex-only property but never appear alone:
 * the element they sit on takes its display from the class beside them, so
 * the property is live even though their own rule never declares one.
 *
 * Each names its companion rather than just switching the check off, and the
 * companion's display is then verified below. A blanket exemption would go
 * stale the day `.row` stopped being flex, and would do it silently — which
 * is the exact failure this check exists to catch.
 */
const FLEX_MODIFIERS = new Map([
  ["stack-start", "stack"],
  ["wrap", "row"],
]);

/**
 * Which component an offset in a file belongs to.
 *
 * Attributed to the nearest preceding function declaration rather than to the
 * file, because the previous version granted the allowance per FILE: one
 * `function Meter(` in src/ui/index.tsx exempted every inline style anywhere
 * in it, which is the file with the most components in the project. The
 * exemption has to name the component it is about or it is not an exemption.
 */
function componentAt(src, index) {
  let name = null;
  // Anchored to the start of a line, so a helper declared INSIDE a component
  // — `function onKeyDown(...)`, indented — does not become the thing the
  // exemption is granted to.
  for (const m of src.matchAll(/^(?:export\s+)?function\s+(\w+)\s*[(<]/gm)) {
    if (m.index > index) break;
    name = m[1];
  }
  return name;
}

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

// 5 — a flex-only property on a box that is not a flex or grid container.
// Numbered last, placed here: it reads only the two stylesheets, so it belongs
// beside the other CSS checks rather than after the source walk.
//
// Declarations are collected per TARGET ELEMENT, keyed by the selector's last
// compound — `.solo .solo-card .brand` and `.brand` style the same element,
// so a display declared on either one counts for both. That is how a kit
// layers a variant on a base and it must not be reported as a mistake.
function flexOnlyNoOps(css) {
  const stripped = css.replace(/\/\*[\s\S]*?\*\//g, "");
  // A body of `[^{}]*` cannot span a nested block, so an `@media` wrapper
  // never matches as a rule of its own while the rules inside it do — which
  // is what we want: a media query is exactly where a layout gets rewritten.
  const rules = [...stripped.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map((m) => ({
    selector: m[1].trim(),
    body: m[2],
  }));

  // The element a selector targets: its last compound, minus any state or
  // pseudo-element suffix, so `.sel-menu .opt:hover` keys on `opt`.
  const target = (selector) => {
    const last =
      selector
        .split(/\s+|\s*[>+~]\s*/)
        .filter(Boolean)
        .pop() ?? "";
    const m = /\.(-?[A-Za-z_][\w-]*)/.exec(last.split(":")[0]);
    return m ? m[1] : null;
  };

  const displays = new Map();
  for (const { selector, body } of rules) {
    const d = /(?:^|;)\s*display\s*:\s*([\w-]+)/.exec(body);
    if (!d) continue;
    for (const one of selector.split(",")) {
      const key = target(one);
      if (key) displays.set(key, (displays.get(key) ?? new Set()).add(d[1]));
    }
  }

  const offenders = [];
  for (const { selector, body } of rules) {
    const props = FLEX_ONLY.filter((prop) => new RegExp(`(^|;)\\s*${prop}\\s*:`).test(body));
    if (!props.length) continue;

    for (const one of selector.split(",")) {
      const key = target(one);
      if (!key) continue;

      const own = new Set(displays.get(key) ?? []);
      const companion = FLEX_MODIFIERS.get(key);
      if (companion) for (const v of displays.get(companion) ?? []) own.add(v);

      if ([...own].some((v) => FLEX_DISPLAY.has(v))) continue;
      offenders.push({
        selector: one.trim(),
        props,
        // A named companion that turned out not to be flex is worth saying
        // out loud: the exemption is the thing that broke, not the rule.
        companion:
          companion && !FLEX_DISPLAY.has([...(displays.get(companion) ?? [])][0])
            ? companion
            : null,
      });
    }
  }
  return offenders;
}

for (const [label, css] of [
  ["docs/mockups/assets/mockup.css", mockupCss],
  ["web/src/styles/app.css", appCss],
]) {
  const offenders = flexOnlyNoOps(css);
  if (offenders.length) {
    failures.push(
      `${label} sets a flex-only property on ${offenders.length} box(es) that are not flex or grid:\n` +
        offenders
          .map(
            (o) =>
              `    ${o.selector}  — ${o.props.join(", ")}` +
              (o.companion
                ? `  (declared a modifier of .${o.companion}, which is no longer flex)`
                : ""),
          )
          .join("\n") +
        `\n  Add the display the property needs, or drop the property. If the class is a\n` +
        `  modifier applied beside another, add it to FLEX_MODIFIERS with its companion.`,
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
    const component = componentAt(src, m.index) ?? "(top level)";
    if (!INLINE_STYLE_ALLOWED.has(component)) inlineStyles.push(`${rel} → ${component}`);
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

// Counts worth seeing even when everything passes.
notes.push(`design kit: ${appClasses.size} classes, ${classesIn(mockupCss).size} in the mockups`);

// Which screens exist in each place. Informational: a screen with no mockup
// is normal for anything built after the rebuild, and a mockup with no screen
// is a design that has not been built yet — both are worth knowing, neither
// is a failure.
const pages = (await readdir(join(SRC, "pages"))).filter(
  (f) => f.endsWith(".tsx") && !f.endsWith(".test.tsx"),
);
const mockupPages = (await readdir(join(repo, "docs", "mockups"))).filter(
  (f) => f.endsWith(".html") && f !== "index.html" && f !== "foundations.html",
);
notes.push(`screens: ${pages.length} built, ${mockupPages.length} designed in the mockups`);

// Kit components no screen renders. Reported, not failed: a component can
// legitimately sit ahead of the screen that will use it. But a dead one is
// also how a second way of doing something survives — the formatting
// component that ignored the reader's preferences lived here unnoticed
// because nothing rendered it.
const kit = await readFile(join(SRC, "ui", "index.tsx"), "utf8");
const kitComponents = [...kit.matchAll(/^export function (\w+)/gm)].map((m) => m[1]);
const screens = [];
for await (const file of walk(SRC)) {
  if (!file.endsWith(join("ui", "index.tsx"))) screens.push(file);
}
const screenSource = (await Promise.all(screens.map((f) => readFile(f, "utf8")))).join("");
const unrendered = kitComponents.filter((n) => !screenSource.includes("<" + n));
if (unrendered.length) {
  notes.push(`${unrendered.length} kit component(s) no screen renders: ${unrendered.join(", ")}`);
}

if (failures.length) {
  console.error("\n✗ design sync\n");
  for (const f of failures) console.error("  " + f + "\n");
  process.exit(1);
}
console.log(`✓ design sync — ${notes.join("; ")}`);
