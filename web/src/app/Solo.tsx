import type { ReactNode } from "react";
import { LOCALES, LOCALE_NAMES, useI18n, useT } from "../i18n";
import { useTheme } from "./theme";
import { SelectField } from "../ui";

/**
 * The layout for a screen that renders without a principal: sign-in, and
 * first-run setup.
 *
 * It must not reuse the app shell. `.app` is a two-column grid whose first
 * track is the 232px sidebar, so a page with no sidebar in it lands in that
 * track and arrives squeezed to the width of navigation that is not there —
 * see docs/mockups/signin.html.
 *
 * The language and theme switchers live here rather than in each screen,
 * because both screens need them for the same reason: someone who cannot read
 * this screen cannot get past it to change the language, which is a circular
 * trap that only appears once the product has a second language.
 */
export function Solo({ children }: { children: ReactNode }) {
  const t = useT();
  const { locale, setLocale } = useI18n();
  const { theme, setTheme } = useTheme();

  return (
    <div className="solo">
      <div className="topbar">
        <div className="spacer" />
        <span className="lang">
          <SelectField
            label={t("prefs.language")}
            value={locale}
            onChange={setLocale}
            options={LOCALES.map((l) => ({ value: l, label: LOCALE_NAMES[l] }))}
          />
        </span>
        <span className="seg" role="group" aria-label={t("theme.label")}>
          <button type="button" aria-pressed={theme === "dark"} onClick={() => setTheme("dark")}>
            {t("theme.dark")}
          </button>
          <button type="button" aria-pressed={theme === "light"} onClick={() => setTheme("light")}>
            {t("theme.light")}
          </button>
        </span>
      </div>

      <main className="page">
        <div className="solo-card">
          <div className="brand">
            {/* The stacked lockup, not the horizontal one: this is a centred
                block, which is the shape the brand package ships stacked for. */}
            <img
              className="logo-dark"
              src="/brand/cc-logo-stacked-on-dark.svg"
              alt={t("app.name")}
              hidden={theme === "light"}
            />
            <img
              className="logo-light"
              src="/brand/cc-logo-stacked-on-light.svg"
              alt={t("app.name")}
              hidden={theme !== "light"}
            />
          </div>
          {children}
        </div>
      </main>
    </div>
  );
}
