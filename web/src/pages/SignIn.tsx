import { useState } from "react";
import { useI18n, useT } from "../i18n";
import { useAuth } from "../app/auth";
import { useTheme } from "../app/theme";
import { Btn, Code, Note, Panel, TextField } from "../ui";
import { LOCALES, LOCALE_NAMES } from "../i18n";

/**
 * The only screen that renders without a principal.
 *
 * The language and theme switchers are here too: someone who cannot read the
 * sign-in screen cannot get past it to change the language, which is a
 * circular trap that only shows up once the product has a second language.
 */
export function SignIn() {
  const t = useT();
  const { locale, setLocale } = useI18n();
  const { theme, setTheme } = useTheme();
  const { signIn } = useAuth();

  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!token.trim() || busy) return;
    setBusy(true);
    setFailed(false);
    try {
      await signIn(token.trim());
    } catch {
      // Every failure reads the same on purpose. Distinguishing "no such
      // token" from "revoked" would tell someone holding a stolen token
      // which half of their guess was right.
      setFailed(true);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="app">
      <div className="main">
        <div className="topbar">
          <div className="spacer" />
          <span className="seg" role="group" aria-label={t("prefs.language")}>
            {LOCALES.map((l) => (
              <button key={l} type="button" aria-pressed={l === locale} onClick={() => setLocale(l)} title={LOCALE_NAMES[l]}>
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
          <div className="doc">
            <div className="brand mb">
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

            <Panel title={t("auth.title")}>
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
          </div>
        </main>
      </div>
    </div>
  );
}
