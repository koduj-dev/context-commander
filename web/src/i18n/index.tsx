import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { en, type MessageKey } from "./en";

/**
 * Internationalisation, built in from the first commit rather than retrofitted.
 *
 * No library. What a library would give us here is a plural engine and a
 * loader, and both are a few lines on top of `Intl`, which the platform
 * already ships and which knows the plural rules of every locale it supports
 * — including the ones with more than two forms (Czech has three, Polish
 * four). What a library would cost is a second opinion about formatting that
 * disagrees with `Intl` somewhere subtle.
 *
 * English is the source. `MessageKey` is derived from it, so a key that does
 * not exist in English is a compile error rather than a string that shows up
 * in production reading "prefs.timeZone".
 */

export const LOCALES = ["en", "de", "es", "fr", "pl", "zh-CN"] as const;
export type Locale = (typeof LOCALES)[number];

/**
 * Each language named in itself. A reader who cannot read the current
 * interface cannot find their own language in a list written in it.
 */
export const LOCALE_NAMES: Record<Locale, string> = {
  en: "English",
  de: "Deutsch",
  es: "Español",
  fr: "Français",
  pl: "Polski",
  "zh-CN": "简体中文",
};

type Catalog = Partial<Record<MessageKey, string>>;

// Loaded on demand: the English catalogue ships with the app because it is
// also the fallback, and nothing else is fetched until someone picks it.
const loaders: Record<Locale, () => Promise<Catalog>> = {
  en: async () => en,
  de: async () => (await import("./de")).de,
  es: async () => (await import("./es")).es,
  fr: async () => (await import("./fr")).fr,
  pl: async () => (await import("./pl")).pl,
  // The export cannot be named for its tag — `zh-CN` is not an identifier.
  "zh-CN": async () => (await import("./zh-CN")).zhCN,
};

export type Values = Record<string, string | number>;

/**
 * Renders one message.
 *
 * Supports `{name}` substitution and ICU-style plurals in the form
 * `{count, plural, one {# thing} other {# things}}`, which is the only ICU
 * construct these screens actually use. `#` becomes the number formatted for
 * the locale — 1,284 or 1 284 depending on who is reading.
 *
 * Deliberately not a general ICU implementation. A half-implemented one that
 * silently mis-renders a construct nobody tested is worse than one that
 * covers what is used and is obviously missing anything else.
 */
export function render(template: string, values: Values, locale: Locale): string {
  const plural = new Intl.PluralRules(locale);
  const number = new Intl.NumberFormat(locale);

  // Plurals first: their bodies may themselves contain {placeholders}.
  const withPlurals = template.replace(
    /\{(\w+),\s*plural,\s*((?:[^{}]|\{[^{}]*\})*)\}/g,
    (_match, name: string, body: string) => {
      const raw = values[name];
      const n = typeof raw === "number" ? raw : Number(raw ?? 0);
      const forms = new Map<string, string>();
      const formRe = /(=\d+|zero|one|two|few|many|other)\s*\{([^{}]*)\}/g;
      for (let m = formRe.exec(body); m; m = formRe.exec(body)) {
        forms.set(m[1]!, m[2]!);
      }
      const chosen = forms.get(`=${n}`) ?? forms.get(plural.select(n)) ?? forms.get("other") ?? "";
      return chosen.replace(/#/g, number.format(n));
    },
  );

  return withPlurals.replace(/\{(\w+)\}/g, (match, name: string) => {
    const v = values[name];
    if (v === undefined) return match;
    return typeof v === "number" ? number.format(v) : v;
  });
}

type I18n = {
  locale: Locale;
  setLocale: (l: Locale) => void;
  /** Translate. Falls back to English for any key this locale has not translated. */
  t: (key: MessageKey, values?: Values) => string;
};

const Ctx = createContext<I18n | null>(null);

const STORAGE_KEY = "cc.locale";

function initialLocale(): Locale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved && (LOCALES as readonly string[]).includes(saved)) return saved as Locale;
  } catch {
    // A browser with storage disabled still gets a working UI.
  }
  // Matched in two passes, because a supported locale may carry a region and
  // a browser preference may too, and the two need not agree. `zh-CN` is the
  // catalogue; a browser asking for `zh`, `zh-Hans` or `zh-SG` wants it.
  // Comparing only full tags would have sent every one of those to English.
  const prefs = navigator.languages ?? [];
  for (const pref of prefs) {
    const exact = LOCALES.find((l) => l.toLowerCase() === pref.toLowerCase());
    if (exact) return exact;
  }
  for (const pref of prefs) {
    const base = pref.split("-")[0]?.toLowerCase();
    if (!base) continue;
    const byLanguage = LOCALES.find((l) => l.split("-")[0]!.toLowerCase() === base);
    if (byLanguage) return byLanguage;
  }
  return "en";
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(initialLocale);
  const [catalog, setCatalog] = useState<Catalog>(en);

  // The initial locale comes from storage or the browser, and the document
  // has to say so too: a page whose `lang` disagrees with its text is
  // pronounced wrong by a screen reader and hyphenated wrong by the browser.
  // Setting it only inside setLocale left a reader who had already chosen
  // German with a document claiming to be English until they chose again —
  // and with English text, since the catalogue was never loaded either.
  useEffect(() => {
    document.documentElement.lang = locale;
    if (locale !== "en") void loaders[locale]().then(setCatalog);
    // Once, for whatever locale the app started in; setLocale keeps the
    // document and the catalogue in step from then on.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const setLocale = useCallback((next: Locale) => {
    void loaders[next]().then((loaded) => {
      setCatalog(loaded);
      setLocaleState(next);
      document.documentElement.lang = next;
      try {
        localStorage.setItem(STORAGE_KEY, next);
      } catch {
        // Preference is lost on reload; the UI still works.
      }
    });
  }, []);

  const t = useCallback(
    (key: MessageKey, values: Values = {}) => {
      // Fall back per string, never per screen: half a page in two languages
      // is worse than one sentence in the wrong one, but a blank is worse
      // than both.
      const template = catalog[key] ?? en[key];
      return render(template, values, locale);
    },
    [catalog, locale],
  );

  const value = useMemo(() => ({ locale, setLocale, t }), [locale, setLocale, t]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useI18n(): I18n {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useI18n must be used inside <I18nProvider>");
  return ctx;
}

/** Shorthand for the common case of translating without needing the locale. */
export function useT() {
  return useI18n().t;
}
