import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import * as fmt from "../lib/format";

/**
 * How this reader wants values written.
 *
 * Separate from the interface language on purpose. Language is what the
 * product says; this is how it writes numbers, dates and sizes, and the two
 * genuinely come apart — someone reading the German UI may still want
 * YYYY-MM-DD because that is what their logs use, and 2.4 GB and 2.24 GiB are
 * the same bytes with different conventions rather than different languages.
 *
 * Stored in this browser for now. The server has no user-preferences endpoint
 * yet, and the Preferences screen says so rather than implying these follow
 * the account (see .claude/skills/ui-design: never promise a capability the
 * backend lacks).
 */
export type Prefs = {
  dateOrder: fmt.DateOrder;
  hourCycle: fmt.HourCycle;
  /** Empty means the browser's own zone. */
  timeZone: string;
  showUTC: boolean;
  units: fmt.UnitSystem;
};

export const DEFAULT_PREFS: Prefs = {
  dateOrder: "locale",
  hourCycle: "locale",
  timeZone: "",
  showUTC: false,
  units: "decimal",
};

const KEY = "cc.prefs";

function load(): Prefs {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return DEFAULT_PREFS;
    // Merged over the defaults, never trusted wholesale: a stored blob from
    // an older version is missing whatever was added since.
    return { ...DEFAULT_PREFS, ...(JSON.parse(raw) as Partial<Prefs>) };
  } catch {
    return DEFAULT_PREFS;
  }
}

type Ctx = {
  prefs: Prefs;
  setPrefs: (patch: Partial<Prefs>) => void;
};

const PrefsCtx = createContext<Ctx | null>(null);

export function PrefsProvider({ children }: { children: ReactNode }) {
  const [prefs, setState] = useState<Prefs>(load);

  const setPrefs = useCallback(
    (patch: Partial<Prefs>) => {
      setState((current) => {
        const next = { ...current, ...patch };
        try {
          localStorage.setItem(KEY, JSON.stringify(next));
        } catch {
          // Preference is lost on reload; the UI still works.
        }
        return next;
      });
    },
    [setState],
  );

  const value = useMemo(() => ({ prefs, setPrefs }), [prefs, setPrefs]);
  return <PrefsCtx.Provider value={value}>{children}</PrefsCtx.Provider>;
}

export function usePrefs(): Ctx {
  const ctx = useContext(PrefsCtx);
  if (!ctx) throw new Error("usePrefs must be used inside <PrefsProvider>");
  return ctx;
}

/**
 * Formatting bound to this reader — locale from the language, everything else
 * from their preferences.
 *
 * Screens call `f.stamp(iso)` and never reach for `Intl` themselves. That is
 * what makes a preference take effect everywhere at once instead of on the
 * screens someone remembered to update.
 */
export function useFormat() {
  const { locale } = useI18n();
  const { prefs } = usePrefs();
  const tz = prefs.timeZone || undefined;

  return useMemo(
    () => ({
      locale,
      timeZone: tz,
      showUTC: prefs.showUTC,
      stamp: (iso: string) => fmt.stamp(iso, locale, prefs.dateOrder, prefs.hourCycle, tz),
      date: (iso: string) => fmt.date(iso, locale, prefs.dateOrder, tz),
      time: (iso: string) => fmt.time(iso, locale, prefs.hourCycle, tz),
      relative: (iso: string) => fmt.relativeTime(iso, locale, tz),
      utc: (iso: string) => fmt.utcStamp(iso),
      number: (n: number) => fmt.number(n, locale),
      bytes: (n: number) => fmt.bytes(n, locale, prefs.units),
    }),
    [locale, prefs.dateOrder, prefs.hourCycle, prefs.showUTC, prefs.units, tz],
  );
}
