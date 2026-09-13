#!/usr/bin/env node
/**
 * Checks the translation catalogues against the English source.
 *
 * English is the origin language: `MessageKey` is derived from `en.ts`, so
 * TypeScript already refuses a key that has no English original. What
 * TypeScript cannot see is the inside of a string, and that is where
 * translations actually break:
 *
 *   - a placeholder dropped in translation ({count} missing) renders a
 *     sentence with a hole in it;
 *   - a placeholder invented in translation ({name} that English never had)
 *     renders the literal braces to the reader;
 *   - a plural with no `other` form renders empty for every count the
 *     translator did not enumerate — and which counts those are depends on
 *     the locale, so it will not reproduce for whoever wrote it.
 *
 * Missing keys are not a failure. Translation lands after the English does,
 * and the runtime falls back per string; a gate that failed the build on an
 * untranslated key would only teach people to paste English into de.ts,
 * which looks translated and never gets fixed. They are reported instead.
 */
import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const i18nDir = join(here, "..", "src", "i18n");

/**
 * Reads a catalogue without running it. These files are plain object
 * literals of string keys to string values, and parsing them as source
 * keeps this check free of a build step and of `src/` importing itself.
 */
function readCatalog(file) {
  const src = readFileSync(file, "utf8");
  const out = new Map();
  // "key.with.dots": "value", tolerating escaped quotes and multi-line
  // template literals, which long sentences in en.ts use.
  const re = /^\s*"([\w.]+)"\s*:\s*(?:"((?:[^"\\]|\\.)*)"|`((?:[^`\\]|\\.)*)`)\s*,?\s*$/gm;
  for (let m = re.exec(src); m; m = re.exec(src)) {
    out.set(m[1], (m[2] ?? m[3] ?? "").replace(/\\"/g, '"').replace(/\\n/g, "\n"));
  }
  if (out.size === 0) {
    throw new Error(`no messages parsed from ${file} — has the catalogue format changed?`);
  }
  return out;
}

/** Every `{name}` placeholder, including the selector of a plural block. */
function placeholders(message) {
  const names = new Set();
  for (const m of message.matchAll(/\{(\w+)\s*(?:,|\})/g)) names.add(m[1]);
  return names;
}

/** Every plural block, as [selector, Set(forms)]. */
function plurals(message) {
  const out = [];
  const re = /\{(\w+),\s*plural,\s*((?:[^{}]|\{[^{}]*\})*)\}/g;
  for (let m = re.exec(message); m; m = re.exec(message)) {
    const forms = new Set();
    for (const f of m[2].matchAll(/(=\d+|zero|one|two|few|many|other)\s*\{/g)) forms.add(f[1]);
    out.push([m[1], forms]);
  }
  return out;
}

const errors = [];
const notes = [];

const en = readCatalog(join(i18nDir, "en.ts"));

for (const [, forms] of [...en].flatMap(([k, v]) => plurals(v).map((p) => [k, p[1]]))) {
  // English itself has to be well-formed, or every locale inherits the bug
  // through the fallback.
  if (!forms.has("other")) errors.push(`en: a plural block has no \`other\` form`);
}

const locales = ["de", "es"];
for (const locale of locales) {
  const file = join(i18nDir, `${locale}.ts`);
  if (!existsSync(file)) {
    notes.push(`${locale}: no catalogue yet — everything falls back to English`);
    continue;
  }
  const cat = readCatalog(file);

  const orphans = [...cat.keys()].filter((k) => !en.has(k));
  for (const k of orphans) {
    errors.push(`${locale}: "${k}" has no English original — renamed or deleted upstream`);
  }

  for (const [key, translated] of cat) {
    const source = en.get(key);
    if (source === undefined) continue;

    const want = placeholders(source);
    const got = placeholders(translated);
    for (const p of want) {
      if (!got.has(p)) errors.push(`${locale}: "${key}" drops the {${p}} placeholder`);
    }
    for (const p of got) {
      if (!want.has(p)) errors.push(`${locale}: "${key}" invents a {${p}} placeholder`);
    }

    for (const [selector, forms] of plurals(translated)) {
      if (!forms.has("other")) {
        errors.push(
          `${locale}: "${key}" plural on {${selector}} has no \`other\` form — ` +
            `it renders empty for any count the translator did not enumerate`,
        );
      }
    }
  }

  const missing = [...en.keys()].filter((k) => !cat.has(k));
  const pct = Math.round(((en.size - missing.length) / en.size) * 100);
  notes.push(
    `${locale}: ${en.size - missing.length}/${en.size} translated (${pct}%)` +
      (missing.length ? `, ${missing.length} falling back to English` : ""),
  );
}

for (const n of notes) console.log(`  ${n}`);

if (errors.length) {
  console.error(`\n✗ translations — ${errors.length} problem(s):\n`);
  for (const e of errors) console.error(`  ${e}`);
  process.exit(1);
}
console.log(`✓ translations — ${en.size} source messages, ${locales.length} target locale(s)`);
