import { describe, expect, it } from "vitest";
import { parseInline, parseMarkdown, safeHref } from "./markdown";

describe("parseMarkdown", () => {
  it("parses headings, paragraphs and rules", () => {
    expect(parseMarkdown("# Title\n\nOne\ntwo\n\n---")).toEqual([
      { t: "h", level: 1, c: [{ t: "text", v: "Title" }] },
      { t: "p", c: [{ t: "text", v: "One two" }] },
      { t: "hr" },
    ]);
  });

  it("caps heading depth at four", () => {
    expect(parseMarkdown("###### deep")[0]).toMatchObject({ t: "h", level: 4 });
  });

  it("parses bulleted and numbered lists with nesting", () => {
    const [ul, ol] = parseMarkdown("- one\n- two\n  - inner\n\n3. three\n4. four");
    expect(ul).toMatchObject({
      t: "ul",
      items: [
        { c: [{ t: "text", v: "one" }] },
        {
          c: [{ t: "text", v: "two" }],
          sub: { t: "ul", items: [{ c: [{ t: "text", v: "inner" }] }] },
        },
      ],
    });
    expect(ol).toMatchObject({ t: "ol", start: 3 });
    expect(ol?.t === "ol" && ol.items.length).toBe(2);
  });

  it("keeps fenced code verbatim, Markdown and markup included", () => {
    expect(parseMarkdown("```go\n**not bold** <b>x</b>\n\n  indented\n```")).toEqual([
      { t: "pre", lang: "go", v: "**not bold** <b>x</b>\n\n  indented" },
    ]);
  });

  it("parses block quotes", () => {
    expect(parseMarkdown("> quoted\n> more")).toEqual([
      { t: "quote", c: [{ t: "p", c: [{ t: "text", v: "quoted more" }] }] },
    ]);
  });

  it("parses pipe tables with alignment", () => {
    const [table] = parseMarkdown("| a | b |\n|:--|--:|\n| 1 | 2 |\n| x \\| y | |");
    expect(table).toMatchObject({
      t: "table",
      align: ["l", "r"],
      head: [[{ t: "text", v: "a" }], [{ t: "text", v: "b" }]],
    });
    expect(table?.t === "table" && table.rows[1]?.[0]).toEqual([{ t: "text", v: "x | y" }]);
  });

  it("leaves raw HTML as text", () => {
    expect(parseMarkdown("<script>alert(1)</script>")).toEqual([
      { t: "p", c: [{ t: "text", v: "<script>alert(1)</script>" }] },
    ]);
  });

  it("stays fast on long runs of unclosed markers", () => {
    const hostile = ["*a", "[a", "`a", "_a ", "~~a", "<a"]
      .map((m) => m.repeat(20_000))
      .join("\n\n");
    const started = performance.now();
    parseMarkdown(hostile);
    expect(performance.now() - started).toBeLessThan(1_000);
  });

  it("never throws on hostile input", () => {
    for (const s of [
      "[",
      "[[[[[[]]",
      "*".repeat(500),
      "`",
      "| |\n|-|",
      "> ".repeat(50) + "x",
      "-\n-\n  -",
    ]) {
      expect(() => parseMarkdown(s)).not.toThrow();
    }
  });
});

describe("parseInline", () => {
  it("parses emphasis, code and strikethrough", () => {
    expect(parseInline("**b** *i* `c` ~~d~~")).toEqual([
      { t: "strong", c: [{ t: "text", v: "b" }] },
      { t: "text", v: " " },
      { t: "em", c: [{ t: "text", v: "i" }] },
      { t: "text", v: " " },
      { t: "code", v: "c" },
      { t: "text", v: " " },
      { t: "del", c: [{ t: "text", v: "d" }] },
    ]);
  });

  it("does not treat snake_case as emphasis", () => {
    expect(parseInline("call cc_accept_invitations now")).toEqual([
      { t: "text", v: "call cc_accept_invitations now" },
    ]);
  });

  it("keeps code spans literal", () => {
    expect(parseInline("`**x**`")).toEqual([{ t: "code", v: "**x**" }]);
  });

  it("links http(s) and mailto only", () => {
    expect(parseInline("[ok](https://example.com/a)")).toEqual([
      { t: "link", href: "https://example.com/a", c: [{ t: "text", v: "ok" }] },
    ]);
    expect(parseInline("[bad](javascript:alert(1))")).toEqual([{ t: "text", v: "bad" }]);
    expect(parseInline("[d](data:text/html,x)")).toEqual([{ t: "text", v: "d" }]);
  });

  it("links bare URLs without trailing punctuation", () => {
    expect(parseInline("see https://example.com/x.")).toEqual([
      { t: "text", v: "see " },
      { t: "link", href: "https://example.com/x", c: [{ t: "text", v: "https://example.com/x" }] },
      { t: "text", v: "." },
    ]);
  });

  it("honours backslash escapes", () => {
    expect(parseInline("\\*not\\*")).toEqual([{ t: "text", v: "*not*" }]);
  });
});

describe("safeHref", () => {
  it("accepts web and mail links and refuses the rest", () => {
    expect(safeHref("https://a.example")).toBe("https://a.example/");
    expect(safeHref("mailto:x@a.example")).toBe("mailto:x@a.example");
    expect(safeHref("JaVaScRiPt:alert(1)")).toBeUndefined();
    expect(safeHref("/relative")).toBeUndefined();
  });
});
