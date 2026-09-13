import type { ReactNode } from "react";
import { useI18n, useT } from "../i18n";
import { ApiError } from "../lib/api";
import * as timeFns from "../lib/format";

/**
 * The component kit.
 *
 * These mirror docs/mockups/assets/mockup.css one for one, which is the point:
 * the mockups are not a picture of the product, they are the same kit rendered
 * with sample data. A screen assembles these; it does not style anything.
 */

// ---------------------------------------------------------------- primitives

export type Tone = "ok" | "warn" | "danger" | "info" | "active" | "shared" | "neutral";

const TONE_CLASS: Record<Tone, string> = {
  ok: "b-ok",
  warn: "b-warn",
  danger: "b-danger",
  info: "b-info",
  active: "b-active",
  shared: "b-shared",
  neutral: "",
};

/**
 * Status. Always a word and a glyph — a badge that carries its meaning in
 * colour alone disappears in greyscale and for a colour-blind reader.
 */
export function Badge({ children, tone = "neutral" }: { children: ReactNode; tone?: Tone }) {
  return (
    <span className={`badge ${TONE_CLASS[tone]}`.trim()}>
      <span className="dot" aria-hidden="true" />
      {children}
    </span>
  );
}

// Spelled out so scripts/check-design-sync.mjs can see the classes, and so a
// scope the server invents later renders as a plain rail rather than as an
// element with an undefined class and no styling at all.
const SCOPE_CLASS: Record<string, string> = {
  installation: "scope installation",
  organization: "scope organization",
  project: "scope project",
};

/** Where a thing lives. Violet for inherited scopes, pink for this project. */
export function Scope({ kind, children }: { kind: string; children?: ReactNode }) {
  return <span className={SCOPE_CLASS[kind] ?? "scope"}>{children ?? kind}</span>;
}

export function Mono({
  children,
  truncate,
  dim,
}: {
  children: ReactNode;
  truncate?: boolean;
  dim?: boolean;
}) {
  return (
    <span className={`mono${truncate ? " truncate" : ""}${dim ? " dim" : ""}`}>{children}</span>
  );
}

export function Cell({ primary, secondary }: { primary: ReactNode; secondary?: ReactNode }) {
  return (
    <>
      {primary}
      {secondary !== undefined && <div className="sub-line">{secondary}</div>}
    </>
  );
}

export function Kpi({
  value,
  label,
  unit,
  tone,
}: {
  value: ReactNode;
  label: ReactNode;
  unit?: string;
  tone?: "warn";
}) {
  return (
    <div className="kpi">
      <span className={`v${tone === "warn" ? " v-warn" : ""}`}>
        {value}
        {unit && <span className="unit"> {unit}</span>}
      </span>
      <span className="k">{label}</span>
    </div>
  );
}

export function Meter({ percent, tone }: { percent: number; tone?: "warn" | "danger" }) {
  const clamped = Math.max(0, Math.min(100, percent));
  return (
    <div className={`meter${tone ? " " + tone : ""}`}>
      <i style={{ width: `${clamped}%` }} />
    </div>
  );
}

export function Note({
  children,
  tone,
}: {
  children: ReactNode;
  tone?: "warn" | "danger" | "info";
}) {
  return <div className={`note${tone ? " " + tone : ""}`}>{children}</div>;
}

export function Defs({ children }: { children: ReactNode }) {
  return <dl className="defs">{children}</dl>;
}

export function Def({ term, children }: { term: ReactNode; children: ReactNode }) {
  return (
    <>
      <dt>{term}</dt>
      <dd>{children}</dd>
    </>
  );
}

type BtnProps = {
  children: ReactNode;
  kind?: "primary" | "danger";
  small?: boolean;
  disabled?: boolean;
  title?: string;
  onClick?: () => void;
  type?: "button" | "submit";
};

export function Btn({
  children,
  kind,
  small,
  disabled,
  title,
  onClick,
  type = "button",
}: BtnProps) {
  const cls = ["btn", kind && `btn-${kind}`, small && "btn-sm"].filter(Boolean).join(" ");
  return (
    <button className={cls} disabled={disabled} title={title} onClick={onClick} type={type}>
      {children}
    </button>
  );
}

export function Seg<T extends string>({
  options,
  value,
  onChange,
  label,
}: {
  options: { value: T; label: string }[];
  value: T;
  onChange: (v: T) => void;
  label: string;
}) {
  return (
    <span className="seg" role="group" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={o.value === value}
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </span>
  );
}

// ---------------------------------------------------------------- structures

export function PageHead({
  title,
  lead,
  actions,
}: {
  title: ReactNode;
  lead?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="page-head">
      <div>
        <h1>{title}</h1>
        {lead && <p>{lead}</p>}
      </div>
      {actions && <div className="actions">{actions}</div>}
    </div>
  );
}

export function Panel({
  title,
  sub,
  right,
  footer,
  tight,
  children,
}: {
  title?: ReactNode;
  sub?: ReactNode;
  right?: ReactNode;
  footer?: ReactNode;
  tight?: boolean;
  children?: ReactNode;
}) {
  return (
    <section className="panel">
      {title !== undefined && (
        <header>
          <h2>{title}</h2>
          {sub && <span className="sub">{sub}</span>}
          {right && <div className="right">{right}</div>}
        </header>
      )}
      <div className={tight ? "body tight" : "body"}>{children}</div>
      {footer && <footer>{footer}</footer>}
    </section>
  );
}

export type Column = { key: string; label?: ReactNode; align?: "r" };

export function DataTable({ columns, children }: { columns: Column[]; children: ReactNode }) {
  const anyLabel = columns.some((c) => c.label !== undefined);
  return (
    <table className="dt">
      {anyLabel && (
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.key} className={c.align ?? ""}>
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
      )}
      <tbody>{children}</tbody>
    </table>
  );
}

/**
 * The footer of any growing list.
 *
 * Deliberately "load more", not page numbers: every list in this system is
 * keyset-paginated, so the server hands back an opaque cursor meaning
 * "continue from here" and has no idea how many pages exist. A 1 · 2 · 3 … 47
 * control would be a promise the API cannot keep.
 */
export function Pager({
  shown,
  hasMore,
  onLoadMore,
  busy,
}: {
  shown: number;
  hasMore: boolean;
  onLoadMore: () => void;
  busy?: boolean;
}) {
  const t = useT();
  return (
    <div className="pager">
      <span className="count">
        {hasMore
          ? t("common.showingMore", { count: shown })
          : t("common.showingAll", { count: shown })}
      </span>
      <span className="spacer" />
      {hasMore ? (
        <Btn small onClick={onLoadMore} disabled={busy}>
          {busy ? t("common.loading") : t("common.loadMore")}
        </Btn>
      ) : (
        <span className="dim">{t("common.endOfList")}</span>
      )}
    </div>
  );
}

export function EmptyState({ title, body }: { title: ReactNode; body?: ReactNode }) {
  return (
    <div className="empty">
      <b>{title}</b>
      {body}
    </div>
  );
}

/**
 * What a failed request looks like.
 *
 * Each kind says something different and gets its own words. The one this
 * exists to prevent is rendering a refusal as an empty list — telling someone
 * their project is empty when the truth is that it is not theirs.
 */
export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const t = useT();
  const kind = error instanceof ApiError ? error.kind : "server";

  const titles: Record<string, ReturnType<typeof t>> = {
    forbidden: t("error.forbidden.title"),
    not_found: t("error.notFound.title"),
    conflict: t("error.conflict.title"),
    rate_limited: t("error.rateLimited.title"),
    unavailable: t("error.unavailable.title"),
    network: t("error.unavailable.title"),
  };
  const bodies: Record<string, ReturnType<typeof t>> = {
    forbidden: t("error.forbidden.body"),
    not_found: t("error.notFound.body"),
    conflict: t("error.conflict.body"),
    rate_limited: t("error.rateLimited.body"),
    unavailable: t("error.unavailable.body"),
    network: t("error.unavailable.body"),
  };

  const tone = kind === "forbidden" || kind === "conflict" ? "warn" : "danger";

  return (
    <div className="empty">
      <Note tone={tone}>
        <b>{titles[kind] ?? t("error.generic.title")}</b>
        <div className="mt-sm">{bodies[kind] ?? (error instanceof Error ? error.message : "")}</div>
      </Note>
      {onRetry && (
        <div className="mt">
          <Btn small onClick={onRetry}>
            {t("common.retry")}
          </Btn>
        </div>
      )}
    </div>
  );
}

export function Loading() {
  const t = useT();
  return <div className="empty">{t("common.loading")}</div>;
}

/** Renders a timestamp the way this reader writes timestamps. */
export function Time({ iso, relative }: { iso: string; relative?: boolean }) {
  const { locale } = useI18n();
  const { dateTime, relativeTime } = timeFns;
  return (
    <time dateTime={iso} title={dateTime(iso, locale)}>
      {relative ? relativeTime(iso, locale) : dateTime(iso, locale)}
    </time>
  );
}

// ---------------------------------------------------------------- layout
//
// Layout is a class, never an inline style: these four wrappers are the only
// way a screen arranges anything, so a change to the grid happens in the
// stylesheet rather than in forty files.

// Spelled out rather than interpolated: a class assembled from a template is
// invisible to scripts/check-design-sync.mjs, and an undefined layout class
// renders as an unstyled block with nothing to warn you.
const GRID_CLASS = {
  2: "grid g2",
  3: "grid g3",
  4: "grid g4",
  "2-1": "grid g-2-1",
} as const;

export function Grid({ cols, children }: { cols: keyof typeof GRID_CLASS; children: ReactNode }) {
  return <div className={GRID_CLASS[cols]}>{children}</div>;
}

export function Stack({ gap, top, children }: { gap?: "sm"; top?: boolean; children: ReactNode }) {
  const cls = ["stack", gap === "sm" && "stack-sm", top && "stack-start"].filter(Boolean).join(" ");
  return <div className={cls}>{children}</div>;
}

export function Row({ gap, children }: { gap?: "sm"; children: ReactNode }) {
  return <div className={gap === "sm" ? "row row-sm" : "row"}>{children}</div>;
}

/** The tab strip under a page head. Children are links; mark one `active`. */
export function Tabs({ label, children }: { label: string; children: ReactNode }) {
  return (
    <nav className="tabs" aria-label={label}>
      {children}
    </nav>
  );
}

// ---------------------------------------------------------------- inputs

export function TextField({
  value,
  onChange,
  placeholder,
  label,
  type = "text",
  grow,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  label: string;
  type?: "text" | "password" | "search";
  grow?: boolean;
}) {
  return (
    <input
      className={grow ? "field grow" : "field"}
      type={type}
      value={value}
      aria-label={label}
      placeholder={placeholder}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

/** A content editor. Monospaced, because what goes in it is structured text. */
export function TextArea({
  value,
  onChange,
  label,
  rows,
  disabled,
}: {
  value: string;
  onChange: (v: string) => void;
  label: string;
  rows?: number;
  disabled?: boolean;
}) {
  return (
    <textarea
      className="field"
      value={value}
      rows={rows}
      aria-label={label}
      disabled={disabled}
      spellCheck={false}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

export function SelectField<T extends string>({
  value,
  onChange,
  options,
  label,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: string }[];
  label: string;
}) {
  return (
    <select
      className="field"
      value={value}
      aria-label={label}
      onChange={(e) => onChange(e.target.value as T)}
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}

/**
 * One setting: what it is, the control, and what it currently produces.
 *
 * The preview is the point. A dropdown reading "DD-MM-YYYY" is a rule; a
 * preview reading "13-09-2026" is the answer to the question the reader
 * actually has.
 */
export function Pref({
  title,
  help,
  preview,
  was,
  children,
}: {
  title: ReactNode;
  help?: ReactNode;
  preview?: ReactNode;
  was?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="pref">
      <div className="what">
        <b>{title}</b>
        {help && <span>{help}</span>}
      </div>
      <div>{children}</div>
      {preview !== undefined && (
        <div className="preview mono">
          {was !== undefined && <span className="was">{was}</span>}
          {preview}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------- readouts

export function Feed({ children }: { children: ReactNode }) {
  return <ul className="feed">{children}</ul>;
}

export function FeedItem({ when, children }: { when: ReactNode; children: ReactNode }) {
  return (
    <li>
      <span className="when">{when}</span>
      <span className="what">{children}</span>
    </li>
  );
}

export function StatusRow({
  name,
  badge,
  detail,
}: {
  name: ReactNode;
  badge: ReactNode;
  detail?: ReactNode;
}) {
  return (
    <div className="status-row">
      <span className="name">{name}</span>
      {badge}
      {detail && <span className="detail">{detail}</span>}
    </div>
  );
}

export function Code({ children }: { children: ReactNode }) {
  return <div className="code mono">{children}</div>;
}
