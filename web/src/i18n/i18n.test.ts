import { describe, expect, it } from "vitest";
import { render } from "./index";
import { en } from "./en";
import { de } from "./de";
import { es } from "./es";
import { fr } from "./fr";
import { pl } from "./pl";
import { zhCN } from "./zh-CN";
import { GLOSSARY } from "./glossary";

describe("render", () => {
  it("substitutes a named placeholder", () => {
    expect(render("Signed in as {name}", { name: "Ada" }, "en")).toBe("Signed in as Ada");
  });

  it("formats a numeric placeholder for the locale", () => {
    expect(render("{count} rows", { count: 1284 }, "en")).toBe("1,284 rows");
    expect(render("{count} rows", { count: 1284 }, "de")).toBe("1.284 rows");
  });

  it("leaves an unsupplied placeholder alone rather than printing undefined", () => {
    expect(render("Hello {name}", {}, "en")).toBe("Hello {name}");
  });

  // The regression this suite exists for: a body pattern that allowed `{`
  // matched only as far as the first form, so every count that was not `one`
  // rendered empty and trailed the rest of the template.
  it("picks the plural form for the count, not just the first one", () => {
    const msg = en["projects.count"];
    expect(render(msg, { count: 1 }, "en")).toBe("1 project");
    expect(render(msg, { count: 4 }, "en")).toBe("4 projects");
    expect(render(msg, { count: 0 }, "en")).toBe("0 projects");
  });

  it("renders a plural that is followed by more text", () => {
    const msg = en["registry.results"];
    expect(render(msg, { count: 2, query: "review" }, "en")).toBe("2 results for “review”");
  });

  it("honours an exact-value form", () => {
    const msg = "{count, plural, =0 {nothing} one {# thing} other {# things}}";
    expect(render(msg, { count: 0 }, "en")).toBe("nothing");
    expect(render(msg, { count: 3 }, "en")).toBe("3 things");
  });

  it("uses each locale's own plural rules", () => {
    expect(render(de["projects.count"]!, { count: 1 }, "de")).toBe("1 Projekt");
    expect(render(de["projects.count"]!, { count: 5 }, "de")).toBe("5 Projekte");
    expect(render(es["members.count"]!, { count: 1 }, "es")).toBe("1 miembro");
    expect(render(es["members.count"]!, { count: 7 }, "es")).toBe("7 miembros");
  });
});

describe("catalogues", () => {
  const catalogues = { de, es, fr, pl, "zh-CN": zhCN };

  it("translate every key the English source defines", () => {
    const keys = Object.keys(en);
    for (const [locale, catalogue] of Object.entries(catalogues)) {
      expect(
        Object.keys(catalogue).filter((k) => !keys.includes(k)),
        locale,
      ).toEqual([]);
      expect(Object.keys(catalogue).length, locale).toBe(keys.length);
    }
  });
});

/**
 * Plural categories are where a renderer breaks silently.
 *
 * Polish has four and Chinese has one; English, German, Spanish and French
 * all have two, so none of the original three exercised either edge. A
 * renderer that assumed an `one` form exists returns an empty string for
 * every Chinese count, and one that collapses `few` into `other` writes
 * "5 projekty" where Polish requires "5 projektów". Both look like a
 * translation mistake and are not.
 */
describe("plural categories", () => {
  it("picks all four Polish forms", () => {
    const msg = pl["projects.count"]!;
    expect(render(msg, { count: 1 }, "pl")).toBe("1 projekt");
    expect(render(msg, { count: 3 }, "pl")).toBe("3 projekty");
    expect(render(msg, { count: 5 }, "pl")).toBe("5 projektów");
    // The teens are `many`, not `few` — the trap in Polish, since 12 ends in
    // a digit that would otherwise select `few`.
    expect(render(msg, { count: 12 }, "pl")).toBe("12 projektów");
    expect(render(msg, { count: 22 }, "pl")).toBe("22 projekty");
  });

  it("renders Chinese from its single form", () => {
    const msg = zhCN["projects.count"]!;
    for (const count of [0, 1, 2, 5, 100]) {
      const out = render(msg, { count }, "zh-CN");
      expect(out, `count=${count}`).toContain("个项目");
      expect(out, `count=${count}`).not.toBe("");
    }
    // Grouping still applies: Chinese uses the same thousands separator.
    expect(render(msg, { count: 1284 }, "zh-CN")).toBe("1,284 个项目");
  });

  it("renders French, which counts 0 as singular", () => {
    const msg = fr["projects.count"]!;
    expect(render(msg, { count: 0 }, "fr")).toBe("0 projet");
    expect(render(msg, { count: 1 }, "fr")).toBe("1 projet");
    expect(render(msg, { count: 2 }, "fr")).toBe("2 projets");
  });
});

/**
 * Established vocabulary must survive translation.
 *
 * Checked here as well as in i18n:check because the script only runs in CI
 * and a developer editing a catalogue sees this first.
 */
describe("glossary", () => {
  const catalogues: Record<string, Partial<Record<keyof typeof en, string>>> = {
    de,
    es,
    fr,
    pl,
    "zh-CN": zhCN,
  };
  const withoutProductName = (s: string) => s.replace(/context commander/gi, "");

  it("keeps every term in every locale", () => {
    for (const [locale, catalogue] of Object.entries(catalogues)) {
      for (const term of GLOSSARY) {
        const want = (term.forms?.[locale] ?? term.en).toLowerCase();
        for (const [key, source] of Object.entries(en)) {
          if (!withoutProductName(source).toLowerCase().includes(term.en)) continue;
          const translated = catalogue[key as keyof typeof en];
          if (translated === undefined) continue;
          expect(
            withoutProductName(translated).toLowerCase().includes(want),
            `${locale} "${key}" should keep "${want}" for "${term.en}"`,
          ).toBe(true);
        }
      }
    }
  });
});
