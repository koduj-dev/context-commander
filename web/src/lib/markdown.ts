/**
 * A small Markdown parser for text agents write: memory bodies, idea
 * descriptions and a planning agent's proposals.
 *
 * It produces a tree, never HTML. The tree is rendered by `<Markdown>` in
 * `ui/index.tsx` as React elements, so nothing in the source can become
 * markup: a `<script>` in a memory is the text "<script>", and there is no
 * `dangerouslySetInnerHTML` anywhere on the path. That is the whole reason
 * this exists instead of a library that turns Markdown into an HTML string
 * and then needs a sanitiser to be trusted — model output is written by
 * whoever last got text into the model's context.
 *
 * Deliberately a subset: headings, paragraphs, bulleted and numbered lists
 * (one level of nesting), fenced code, block quotes, rules, pipe tables, and
 * inline code, bold, italic, strikethrough and links. Links keep only
 * http(s) and mailto targets; anything else renders as its text. What the
 * subset does not know stays readable as the characters that were written.
 */

export type Inline =
  | { t: "text"; v: string }
  | { t: "code"; v: string }
  | { t: "strong"; c: Inline[] }
  | { t: "em"; c: Inline[] }
  | { t: "del"; c: Inline[] }
  | { t: "link"; href: string; c: Inline[] }
  | { t: "br" };

export type ListItem = { c: Inline[]; sub?: Block };

export type Block =
  | { t: "h"; level: 1 | 2 | 3 | 4; c: Inline[] }
  | { t: "p"; c: Inline[] }
  | { t: "ul"; items: ListItem[] }
  | { t: "ol"; start: number; items: ListItem[] }
  | { t: "pre"; lang: string; v: string }
  | { t: "quote"; c: Block[] }
  | { t: "hr" }
  | { t: "table"; align: ("l" | "c" | "r")[]; head: Inline[][]; rows: Inline[][][] };

/** A link target safe to put in an href, or undefined. */
export function safeHref(raw: string): string | undefined {
  const s = raw.trim();
  if (/^mailto:[^\s]+$/i.test(s)) return s;
  try {
    const u = new URL(s);
    return u.protocol === "https:" || u.protocol === "http:" ? u.href : undefined;
  } catch {
    return undefined;
  }
}

const BULLET = /^(\s*)[-*+]\s+(.*)$/;
const ORDERED = /^(\s*)(\d{1,9})[.)]\s+(.*)$/;
const FENCE = /^\s{0,3}(`{3,}|~{3,})\s*([\w+-]*)\s*$/;
const HEADING = /^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const RULE = /^\s{0,3}([-*_])(\s*\1){2,}\s*$/;
const TABLE_SEP = /^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$/;

function splitRow(line: string): string[] {
  let s = line.trim();
  if (s.startsWith("|")) s = s.slice(1);
  if (s.endsWith("|") && !s.endsWith("\\|")) s = s.slice(0, -1);
  const cells: string[] = [];
  let cur = "";
  for (let i = 0; i < s.length; i++) {
    if (s[i] === "\\" && s[i + 1] === "|") {
      cur += "|";
      i++;
    } else if (s[i] === "|") {
      cells.push(cur.trim());
      cur = "";
    } else {
      cur += s[i];
    }
  }
  cells.push(cur.trim());
  return cells;
}

function startsBlock(line: string): boolean {
  return (
    HEADING.test(line) ||
    FENCE.test(line) ||
    RULE.test(line) ||
    /^\s{0,3}>/.test(line) ||
    BULLET.test(line) ||
    ORDERED.test(line)
  );
}

/** Parses Markdown source into blocks. */
export function parseMarkdown(source: string): Block[] {
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  return parseBlocks(lines, 0);
}

function parseBlocks(lines: string[], depth: number): Block[] {
  const out: Block[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i] ?? "";
    if (line.trim() === "") {
      i++;
      continue;
    }

    const fence = FENCE.exec(line);
    if (fence) {
      const marker = fence[1] ?? "";
      const body: string[] = [];
      i++;
      while (i < lines.length && !(lines[i] ?? "").trim().startsWith(marker)) {
        body.push(lines[i] ?? "");
        i++;
      }
      i++; // the closing fence, or the end
      out.push({ t: "pre", lang: fence[2] ?? "", v: body.join("\n") });
      continue;
    }

    const heading = HEADING.exec(line);
    if (heading) {
      const level = Math.min(4, (heading[1] ?? "").length) as 1 | 2 | 3 | 4;
      out.push({ t: "h", level, c: parseInline(heading[2] ?? "") });
      i++;
      continue;
    }

    if (RULE.test(line)) {
      out.push({ t: "hr" });
      i++;
      continue;
    }

    if (/^\s{0,3}>/.test(line)) {
      const body: string[] = [];
      while (i < lines.length && /^\s{0,3}>/.test(lines[i] ?? "")) {
        body.push((lines[i] ?? "").replace(/^\s{0,3}>\s?/, ""));
        i++;
      }
      out.push({
        t: "quote",
        c: depth < 3 ? parseBlocks(body, depth + 1) : [{ t: "p", c: parseInline(body.join(" ")) }],
      });
      continue;
    }

    if (BULLET.test(line) || ORDERED.test(line)) {
      const ordered = !BULLET.test(line);
      const first = ordered ? ORDERED.exec(line)! : BULLET.exec(line)!;
      const indent = (first[1] ?? "").length;
      const start = ordered ? Number(first[2] ?? "") : 1;
      const items: { text: string[]; sub: string[] }[] = [];
      while (i < lines.length) {
        const l = lines[i] ?? "";
        const b = BULLET.exec(l);
        const o = ORDERED.exec(l);
        const m = ordered ? o : b;
        if (m && (m[1] ?? "").length <= indent + 1) {
          items.push({ text: [ordered ? (m[3] ?? "") : (m[2] ?? "")], sub: [] });
          i++;
          continue;
        }
        if (l.trim() === "") {
          // A blank line ends the list unless the next line continues it.
          const next: string | undefined = lines[i + 1];
          if (
            next !== undefined &&
            (next.startsWith(" ".repeat(indent + 2)) || (ordered ? ORDERED : BULLET).test(next))
          ) {
            i++;
            continue;
          }
          break;
        }
        const leading = l.length - l.trimStart().length;
        if (items.length > 0 && leading > indent) {
          const cur = items[items.length - 1]!;
          if (cur.sub.length > 0 || BULLET.test(l) || ORDERED.test(l)) cur.sub.push(l);
          else cur.text.push(l.trim());
          i++;
          continue;
        }
        if (items.length > 0 && !startsBlock(l) && leading <= indent) {
          // A lazy continuation of the last item's paragraph.
          items[items.length - 1]!.text.push(l.trim());
          i++;
          continue;
        }
        break;
      }
      const list: ListItem[] = items.map((it) => {
        const item: ListItem = { c: parseInline(it.text.join(" ")) };
        if (it.sub.length > 0 && depth < 3) {
          const minIndent = Math.min(
            ...it.sub.filter((s) => s.trim()).map((s) => s.length - s.trimStart().length),
          );
          const sub = parseBlocks(
            it.sub.map((s) => s.slice(minIndent)),
            depth + 1,
          );
          if (sub.length > 0) item.sub = sub[0];
        }
        return item;
      });
      out.push(ordered ? { t: "ol", start, items: list } : { t: "ul", items: list });
      continue;
    }

    if (
      line.includes("|") &&
      i + 1 < lines.length &&
      TABLE_SEP.test(lines[i + 1] ?? "") &&
      (lines[i + 1] ?? "").includes("-")
    ) {
      const head = splitRow(line);
      const align = splitRow(lines[i + 1] ?? "").map((c) =>
        c.startsWith(":") && c.endsWith(":") ? "c" : c.endsWith(":") ? "r" : "l",
      ) as ("l" | "c" | "r")[];
      i += 2;
      const rows: Inline[][][] = [];
      while (i < lines.length && (lines[i] ?? "").includes("|") && (lines[i] ?? "").trim() !== "") {
        const cells = splitRow(lines[i] ?? "");
        rows.push(head.map((_, k) => parseInline(cells[k] ?? "")));
        i++;
      }
      out.push({
        t: "table",
        align: head.map((_, k) => align[k] ?? "l"),
        head: head.map(parseInline),
        rows,
      });
      continue;
    }

    const para: string[] = [];
    while (
      i < lines.length &&
      (lines[i] ?? "").trim() !== "" &&
      (para.length === 0 || !startsBlock(lines[i] ?? ""))
    ) {
      para.push(lines[i] ?? "");
      i++;
    }
    const c: Inline[] = [];
    para.forEach((p, k) => {
      const hard = / {2,}$/.test(p) || p.endsWith("\\");
      c.push(...parseInline(p.replace(/\\$/, "").trim()));
      if (k < para.length - 1) c.push(hard ? { t: "br" } : { t: "text", v: " " });
    });
    out.push({ t: "p", c: merge(c) });
  }
  return out;
}

function merge(nodes: Inline[]): Inline[] {
  const out: Inline[] = [];
  for (const n of nodes) {
    const last = out[out.length - 1];
    if (n.t === "text" && last?.t === "text") last.v += n.v;
    else out.push(n.t === "text" ? { ...n } : n);
  }
  return out;
}

const ESCAPABLE = "\\`*_{}[]()#+-.!|~>";

/** Parses one line of inline Markdown. */
export function parseInline(src: string, depth = 0): Inline[] {
  const out: Inline[] = [];
  let text = "";
  const flush = () => {
    if (text) out.push({ t: "text", v: text });
    text = "";
  };
  let i = 0;
  while (i < src.length) {
    const ch = src[i];

    if (ch === "\\" && i + 1 < src.length && ESCAPABLE.includes(src[i + 1] ?? " ")) {
      text += src[i + 1];
      i += 2;
      continue;
    }

    if (ch === "`") {
      let ticks = 1;
      while (src[i + ticks] === "`") ticks++;
      const marker = "`".repeat(ticks);
      const end = src.indexOf(marker, i + ticks);
      if (end > 0) {
        flush();
        let v = src.slice(i + ticks, end);
        if (v.startsWith(" ") && v.endsWith(" ") && v.trim()) v = v.slice(1, -1);
        out.push({ t: "code", v });
        i = end + ticks;
        continue;
      }
      text += marker;
      i += ticks;
      continue;
    }

    if (ch === "[" && depth < 4) {
      const close = matchBracket(src, i);
      if (close > 0 && src[close + 1] === "(") {
        const paren = matchParen(src, close + 1);
        if (paren > 0) {
          const target =
            src
              .slice(close + 2, paren)
              .trim()
              .split(/\s+/)[0] ?? "";
          const label = parseInline(src.slice(i + 1, close), depth + 1);
          const href = safeHref(target.replace(/^<|>$/g, ""));
          flush();
          if (href) out.push({ t: "link", href, c: label });
          else out.push(...label);
          i = paren + 1;
          continue;
        }
      }
    }

    if (ch === "<") {
      const end = src.indexOf(">", i);
      if (end > 0) {
        const href = safeHref(src.slice(i + 1, end));
        if (href && /^(https?:|mailto:)/i.test(src.slice(i + 1, end))) {
          flush();
          out.push({ t: "link", href, c: [{ t: "text", v: src.slice(i + 1, end) }] });
          i = end + 1;
          continue;
        }
      }
    }

    if (
      (ch === "h" || ch === "H") &&
      /^https?:\/\//i.test(src.slice(i)) &&
      (i === 0 || /[\s(]/.test(src[i - 1] ?? " "))
    ) {
      const m = /^https?:\/\/[^\s<>]+/i.exec(src.slice(i))!;
      let raw = m[0] ?? "";
      while (/[.,;:!?)]$/.test(raw)) raw = raw.slice(0, -1);
      const href = safeHref(raw);
      if (href) {
        flush();
        out.push({ t: "link", href, c: [{ t: "text", v: raw }] });
        i += raw.length;
        continue;
      }
    }

    if ((ch === "*" || ch === "_" || ch === "~") && depth < 4) {
      const run = src[i + 1] === ch ? 2 : 1;
      if (ch === "~" && run !== 2) {
        text += ch;
        i++;
        continue;
      }
      const marker = ch.repeat(run);
      const prev = src[i - 1];
      const next = src[i + run];
      // Intraword underscores stay literal: snake_case_names are not emphasis.
      const opens =
        next !== undefined &&
        next !== " " &&
        !(ch === "_" && prev !== undefined && /\w/.test(prev));
      if (opens) {
        const end = findClose(src, i + run, marker, ch);
        if (end > 0) {
          flush();
          const inner = parseInline(src.slice(i + run, end), depth + 1);
          out.push(
            ch === "~"
              ? { t: "del", c: inner }
              : run === 2
                ? { t: "strong", c: inner }
                : { t: "em", c: inner },
          );
          i = end + run;
          continue;
        }
      }
      text += marker;
      i += run;
      continue;
    }

    text += ch;
    i++;
  }
  flush();
  return out;
}

function matchBracket(src: string, open: number): number {
  let level = 0;
  for (let k = open; k < src.length; k++) {
    if (src[k] === "\\") {
      k++;
      continue;
    }
    if (src[k] === "[") level++;
    else if (src[k] === "]") {
      level--;
      if (level === 0) return k;
    }
  }
  return -1;
}

function matchParen(src: string, open: number): number {
  let level = 0;
  for (let k = open; k < src.length; k++) {
    if (src[k] === "(") level++;
    else if (src[k] === ")") {
      level--;
      if (level === 0) return k;
    } else if (src[k] === " " && level === 1 && src[k + 1] !== '"') return -1;
  }
  return -1;
}

function findClose(src: string, from: number, marker: string, ch: string): number {
  let k = from;
  while (k < src.length) {
    if (src[k] === "\\") {
      k += 2;
      continue;
    }
    if (src[k] === "`") {
      const end = src.indexOf("`", k + 1);
      if (end < 0) return -1;
      k = end + 1;
      continue;
    }
    if (src.startsWith(marker, k) && src[k - 1] !== " " && k > from) {
      const after = src[k + marker.length];
      // A single marker must not be half of a double one.
      if (marker.length === 1 && after === ch) {
        k += 2;
        continue;
      }
      if (ch === "_" && after !== undefined && /\w/.test(after)) {
        k++;
        continue;
      }
      return k;
    }
    k++;
  }
  return -1;
}
