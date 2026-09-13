// Copies the brand package into public/ before a build or a dev run.
//
// docs/brand is the single source of truth (see docs/brand/README.md and the
// project's brand rules). Committing a second copy inside web/ would create
// two versions of the logo that can disagree, and the one people notice
// would be the wrong one. So: copy at build time, never by hand, and keep
// public/brand out of git.
import { mkdir, readdir, copyFile, access } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const brand = join(here, "..", "..", "docs", "brand");
const out = join(here, "..", "public", "brand");

// Only what the app actually renders. Copying the whole package would ship
// the print manual and every unused lockup to every browser.
const WANTED = [
  "svg/cc-logo-horizontal-on-dark.svg",
  "svg/cc-logo-horizontal-on-light.svg",
  "svg/cc-favicon.svg",
  "tokens/ui-colors.css",
];

try {
  await access(brand);
} catch {
  console.error(`sync-brand: ${brand} not found — the brand package is required to build the UI`);
  process.exit(1);
}

await mkdir(out, { recursive: true });
for (const rel of WANTED) {
  const name = rel.split("/").pop();
  await copyFile(join(brand, rel), join(out, name));
}
console.log(`sync-brand: ${WANTED.length} files → public/brand/`);
