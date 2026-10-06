import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { copyToClipboard } from "../lib/clipboard";
import { useT } from "../i18n";
import { ApiError } from "../lib/api";

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

/**
 * A value with a way to take it.
 *
 * This product asks people to paste UUIDs — adding a member, looking up
 * credentials, adding someone to an organization — because nothing in the
 * API lists principals. Until that changes, selecting a truncated identifier
 * by hand is the most common thing anyone does in this interface, and it is
 * the thing most likely to go wrong silently.
 *
 * When copying cannot be done at all the button says so instead of failing
 * silently — a control that does nothing and reports nothing teaches people
 * it is broken everywhere, including where it works.
 */
export function Copyable({ value, children }: { value: string; children?: ReactNode }) {
  const t = useT();
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");

  async function copy() {
    const ok = await copyToClipboard(value);
    setState(ok ? "copied" : "failed");
    setTimeout(() => setState("idle"), ok ? 1500 : 4000);
  }

  const label =
    state === "copied"
      ? t("common.copied")
      : state === "failed"
        ? t("common.copyFailed")
        : t("common.copy");

  return (
    <span className="copy">
      {children ?? <Mono truncate>{value}</Mono>}
      <button
        type="button"
        onClick={() => void copy()}
        data-copied={state === "copied" ? "true" : undefined}
        data-failed={state === "failed" ? "true" : undefined}
        title={label}
        aria-label={label}
      >
        {state === "copied" ? "✓" : state === "failed" ? "!" : "⧉"}
      </button>
    </span>
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
  const serverSaid = error instanceof ApiError ? error.message : "";

  // A 409 means several different things: an artifact written under you, a
  // handoff claimed first, a skill that cannot be promoted any wider. Only
  // the first is a merge problem, and the editor that owns it says so
  // itself — so here a conflict reports what the server actually said
  // rather than telling someone to merge a change they never made.
  const titles: Record<string, ReturnType<typeof t>> = {
    forbidden: t("error.forbidden.title"),
    not_found: t("error.notFound.title"),
    conflict: t("error.conflict.generic"),
    rate_limited: t("error.rateLimited.title"),
    unavailable: t("error.unavailable.title"),
    network: t("error.unavailable.title"),
  };
  const bodies: Record<string, ReturnType<typeof t>> = {
    forbidden: t("error.forbidden.body"),
    not_found: t("error.notFound.body"),
    rate_limited: t("error.rateLimited.body"),
    unavailable: t("error.unavailable.body"),
    network: t("error.unavailable.body"),
  };

  const tone = kind === "forbidden" || kind === "conflict" ? "warn" : "danger";

  return (
    <div className="empty">
      <Note tone={tone}>
        <b>{titles[kind] ?? t("error.generic.title")}</b>
        <div className="mt-sm">
          {bodies[kind] ?? serverSaid ?? (error instanceof Error ? error.message : "")}
        </div>
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
  autoComplete,
  id,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  label: string;
  type?: "text" | "password" | "search" | "email";
  grow?: boolean;
  /**
   * The browser's autofill hint. Worth exposing rather than guessing from
   * `type`: a password manager needs to know whether a password field is the
   * one being entered ("current-password") or the one being chosen
   * ("new-password"), and both are type="password". Without it, a manager
   * offers to fill a "choose a new password" box with the old one.
   */
  autoComplete?: string;
  /** Set when a visible <label> points here instead of the aria-label. */
  id?: string;
}) {
  return (
    <input
      className={grow ? "field grow" : "field"}
      id={id}
      type={type}
      value={value}
      aria-label={label}
      placeholder={placeholder}
      autoComplete={autoComplete}
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

/**
 * A select, built rather than borrowed.
 *
 * The native `<select>` cannot be styled past its border: the open list is
 * drawn by the operating system, in its font and its colours, which on a dark
 * console is the one element that announces it does not belong. So this is a
 * real listbox — and being real means carrying the keyboard behaviour the
 * native control gave away for free, which is the part that gets skipped:
 * Arrow keys move, Home/End jump, Enter and Space choose, Escape closes and
 * returns focus, Tab and a click elsewhere close without choosing.
 *
 * The open list is portalled to `body` and positioned fixed. `.panel` sets
 * `overflow: hidden` for its rounded corners, so a popup in the normal flow
 * is clipped by the card it sits in — which only shows up once a select is
 * near the bottom of a panel, and most of them are.
 *
 * The API is unchanged from the native version it replaced, so all eleven
 * screens using it got this without being touched.
 */
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
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [rect, setRect] = useState<DOMRect | null>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  const selectedIndex = Math.max(
    0,
    options.findIndex((o) => o.value === value),
  );
  const current = options[selectedIndex];

  const show = () => {
    // Measured when opening, not at render: the trigger moves when the page
    // scrolls or a panel above it grows.
    setRect(triggerRef.current?.getBoundingClientRect() ?? null);
    setActive(selectedIndex);
    setOpen(true);
  };

  const hide = (restoreFocus = true) => {
    setOpen(false);
    if (restoreFocus) triggerRef.current?.focus();
  };

  const choose = (index: number) => {
    const option = options[index];
    if (option) onChange(option.value);
    hide();
  };

  // Anything that moves the trigger invalidates the measured position, and a
  // list floating away from its own control is worse than a closed one.
  useEffect(() => {
    if (!open) return;
    const close = () => {
      setOpen(false);
    };
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, [open]);

  // A click anywhere else closes without choosing — what the native control
  // does, and what everyone expects of anything that looks like one.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      const target = e.target as Node;
      if (menuRef.current?.contains(target) || triggerRef.current?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  const onKey = (e: React.KeyboardEvent) => {
    if (!open) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        show();
      }
      return;
    }
    switch (e.key) {
      case "Escape":
        e.preventDefault();
        hide();
        break;
      case "ArrowDown":
        e.preventDefault();
        setActive((i) => Math.min(i + 1, options.length - 1));
        break;
      case "ArrowUp":
        e.preventDefault();
        setActive((i) => Math.max(i - 1, 0));
        break;
      case "Home":
        e.preventDefault();
        setActive(0);
        break;
      case "End":
        e.preventDefault();
        setActive(options.length - 1);
        break;
      case "Enter":
      case " ":
        e.preventDefault();
        choose(active);
        break;
      case "Tab":
        // Deliberately not prevented: Tab should move on. Leaving the list
        // open behind the focus ring is how a popup gets orphaned.
        setOpen(false);
        break;
    }
  };

  const listId = `sel-${label.replace(/\W+/g, "-").toLowerCase()}`;

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className="field sel"
        role="combobox"
        aria-label={label}
        aria-expanded={open}
        aria-controls={listId}
        aria-activedescendant={open ? `${listId}-${active}` : undefined}
        onClick={() => (open ? hide(false) : show())}
        onKeyDown={onKey}
      >
        <span>{current?.label ?? ""}</span>
        <svg
          className="ico"
          viewBox="0 0 18 18"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          aria-hidden="true"
        >
          <path d="m5 7.5 4 4 4-4" />
        </svg>
      </button>

      {open &&
        rect &&
        createPortal(
          <div
            ref={menuRef}
            id={listId}
            className="sel-menu"
            role="listbox"
            aria-label={label}
            style={{
              left: rect.left,
              top: rect.bottom + 4,
              minWidth: rect.width,
              maxHeight: Math.max(160, window.innerHeight - rect.bottom - 24),
            }}
          >
            {options.map((o, i) => (
              <button
                key={o.value}
                id={`${listId}-${i}`}
                type="button"
                role="option"
                aria-selected={o.value === value}
                data-active={i === active ? "true" : undefined}
                onMouseEnter={() => setActive(i)}
                onClick={() => choose(i)}
              >
                {o.label}
              </button>
            ))}
          </div>,
          document.body,
        )}
    </>
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

export function Code({ children }: { children: ReactNode }) {
  return <div className="code mono">{children}</div>;
}
