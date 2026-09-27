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
import { readFile, readdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

/** Every .ts/.tsx under a directory, except the catalogues themselves. */
async function collectSources(dir) {
  const out = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...(await collectSources(full)));
    else if (/\.tsx?$/.test(entry.name) && !full.includes("i18n")) out.push(full);
  }
  return out;
}

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
  // "key.with.dots": "value" — in all three quotings JavaScript allows,
  // because the formatter picks one: a message containing a double quote is
  // rewritten to single quotes, and a message parsed in only one quoting
  // silently vanishes from the catalogue instead of failing.
  const re =
    /^\s*"([\w.]+)"\s*:\s*(?:"((?:[^"\\]|\\.)*)"|'((?:[^'\\]|\\.)*)'|`((?:[^`\\]|\\.)*)`)\s*,?\s*$/gm;
  for (let m = re.exec(src); m; m = re.exec(src)) {
    const raw = m[2] ?? m[3] ?? m[4] ?? "";
    out.set(m[1], raw.replace(/\\(["'`])/g, "$1").replace(/\\n/g, "\n"));
  }
  // Every `"key":` line must have produced a message. Without this, a value
  // the regex does not recognise — a quoting the formatter chose, a string
  // concatenation someone added — drops that key silently, and the only
  // symptom is every other locale reporting it as an orphan.
  const declared = [...src.matchAll(/^\s*"([\w.]+)"\s*:/gm)].map((m) => m[1]);
  const missed = declared.filter((k) => !out.has(k));
  if (missed.length) {
    throw new Error(
      `${file}: ${missed.length} key(s) declared but not parsed — ${missed.join(", ")}. ` +
        `The value is in a form readCatalog does not recognise.`,
    );
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

/**
 * The glossary, read as source.
 *
 * Parsed rather than imported for the same reason the catalogues are: this
 * script runs on plain node with no build step, and `src/` importing itself
 * through a bundler would make the check depend on the thing it checks.
 */
function readGlossary(file) {
  const src = readFileSync(file, "utf8");
  const out = [];
  // One object literal per term: en, an optional forms map, a note.
  for (const block of src.split(/\n  \{\n/).slice(1)) {
    const en = /en:\s*"([^"]+)"/.exec(block);
    if (!en) continue;
    const forms = {};
    const formsBlock = /forms:\s*\{([^}]*)\}/s.exec(block);
    if (formsBlock) {
      for (const m of formsBlock[1].matchAll(/"?([\w-]+)"?\s*:\s*"([^"]+)"/g)) {
        forms[m[1]] = m[2];
      }
    }
    out.push({ en: en[1], forms });
  }
  if (out.length === 0) throw new Error(`no glossary terms parsed from ${file}`);
  return out;
}

/**
 * The product name is a proper noun, not vocabulary.
 *
 * "Context Commander" stays in English in every catalogue, which would
 * otherwise trip the `context` rule in every language that translates the
 * word — so it is removed from both sides before anything is matched.
 */
const PRODUCT_NAME = /context commander/gi;

/**
 * A term "appears" if it occurs anywhere, case-insensitively.
 *
 * A plain substring, with no word boundary, and that is deliberate. German
 * compounds ("Zugriffstoken"), Polish and Czech inflect ("tokenu"), and both
 * bury the term inside a longer word — a boundary-anchored match rejected
 * translations that used the term perfectly well, which is the expensive kind
 * of wrong: it teaches whoever hits it to weaken the rule.
 *
 * What this check is actually for is catching a term that was REPLACED, and a
 * substring catches that. The cost is that "contextual" satisfies "context",
 * which is a false pass on a translation that did use the word.
 */
function mentions(text, term) {
  return text.replace(PRODUCT_NAME, "").toLowerCase().includes(term.toLowerCase());
}

const errors = [];
const notes = [];

const en = readCatalog(join(i18nDir, "en.ts"));
const glossary = readGlossary(join(i18nDir, "glossary.ts"));

for (const [, forms] of [...en].flatMap(([k, v]) => plurals(v).map((p) => [k, p[1]]))) {
  // English itself has to be well-formed, or every locale inherits the bug
  // through the fallback.
  if (!forms.has("other")) errors.push(`en: a plural block has no \`other\` form`);
}

const locales = ["de", "es", "fr", "pl", "zh-CN"];
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

    // Established vocabulary must survive translation. The field already has
    // words for these things, and a translator reaching for a
    // native-sounding alternative breaks the connection between what the
    // interface says and what the tools are called.
    for (const term of glossary) {
      if (!mentions(source, term.en)) continue;
      const want = term.forms?.[locale] ?? term.en;
      if (!mentions(translated, want)) {
        errors.push(
          `${locale}: "${key}" mentions "${term.en}" but the translation does not use ` +
            `"${want}" — see src/i18n/glossary.ts`,
        );
      }
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

/**
 * Keys nobody renders.
 *
 * Reported rather than failed: a key can legitimately be built at runtime —
 * `role.${role}`, `scope.${scope}` — and this cannot see that. But a growing
 * list here usually means something else: a screen was rebuilt and its old
 * copy stayed behind, or a feature was written into the catalogue and never
 * wired up. Both are worth noticing while it is still one or two.
 */
const usage = (
  await Promise.all((await collectSources(join(here, "..", "src"))).map((f) => readFile(f, "utf8")))
).join("");
const dynamic = /^(role|scope|trust)\./;
const unused = [...en.keys()].filter((k) => !dynamic.test(k) && !usage.includes(`"${k}"`));
if (unused.length) {
  notes.push(
    `${unused.length} key(s) defined but never rendered: ${unused.slice(0, 8).join(", ")}` +
      (unused.length > 8 ? ", …" : ""),
  );
}

for (const n of notes) console.log(`  ${n}`);

if (errors.length) {
  console.error(`\n✗ translations — ${errors.length} problem(s):\n`);
  for (const e of errors) console.error(`  ${e}`);
  process.exit(1);
}
console.log(`✓ translations — ${en.size} source messages, ${locales.length} target locale(s)`);
