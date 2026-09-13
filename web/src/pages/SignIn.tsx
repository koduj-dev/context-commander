import { useState, type FormEvent } from "react";
import { LOCALES, LOCALE_NAMES, useI18n, useT } from "../i18n";
import { useAuth } from "../app/auth";
import { useTheme } from "../app/theme";
import { Btn, Code, Note, Panel, TextField } from "../ui";

/**
 * The only screen that renders without a principal.
 *
 * It uses the `solo` layout rather than the app shell. `app` is a two-column
 * grid whose first track is the 232px sidebar, so a page with no sidebar in
 * it lands in that track and arrives squeezed to the width of navigation that
 * is not there — see docs/mockups/signin.html.
 *
 * The language and theme switchers are here too: someone who cannot read the
 * sign-in screen cannot get past it to change the language, which is a
 * circular trap that only appears once the product has a second language.
 */
export function SignIn() {
  const t = useT();
  const { locale, setLocale } = useI18n();
  const { theme, setTheme } = useTheme();
  const { signIn } = useAuth();

  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!token.trim() || busy) return;
    setBusy(true);
    setFailed(false);
    try {
      await signIn(token.trim());
    } catch {
      // Every failure reads the same on purpose — see auth.indistinct.
      setFailed(true);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="solo">
      <div className="topbar">
        <div className="spacer" />
        <span className="seg" role="group" aria-label={t("prefs.language")}>
          {LOCALES.map((l) => (
            <button
              key={l}
              type="button"
              aria-pressed={l === locale}
              onClick={() => setLocale(l)}
              title={LOCALE_NAMES[l]}
            >
              {l.toUpperCase()}
            </button>
          ))}
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
            <img
              className="logo-dark"
              src="/brand/cc-logo-horizontal-on-dark.svg"
              alt={t("app.name")}
              hidden={theme === "light"}
            />
            <img
              className="logo-light"
              src="/brand/cc-logo-horizontal-on-light.svg"
              alt={t("app.name")}
              hidden={theme !== "light"}
            />
          </div>

          <Panel
            title={t("auth.title")}
            footer={<span className="dim">{t("auth.sameToken")}</span>}
          >
            <p className="sec lead-out">{t("auth.lead", { command: "ccd token create" })}</p>
            <Code>ccd token create --name "web ui"</Code>

            <form onSubmit={submit} className="mt">
              <div className="row row-sm">
                <TextField
                  label={t("auth.token")}
                  type="password"
                  value={token}
                  onChange={setToken}
                  placeholder={t("auth.token")}
                  grow
                />
                <Btn kind="primary" type="submit" disabled={busy || !token.trim()}>
                  {busy ? t("common.loading") : t("auth.submit")}
                </Btn>
              </div>
            </form>

            {failed && (
              <div className="mt">
                <Note tone="danger">{t("auth.failed")}</Note>
              </div>
            )}
          </Panel>

          <div className="mt">
            <Note tone="info">{t("auth.indistinct")}</Note>
          </div>
        </div>
      </main>
    </div>
  );
}
