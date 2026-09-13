import { describe, expect, it } from "vitest";
import { render } from "./index";
import { en } from "./en";
import { de } from "./de";
import { es } from "./es";

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
  it("translate every key the English source defines", () => {
    const keys = Object.keys(en);
    expect(Object.keys(de).filter((k) => !keys.includes(k))).toEqual([]);
    expect(Object.keys(es).filter((k) => !keys.includes(k))).toEqual([]);
  });
});
