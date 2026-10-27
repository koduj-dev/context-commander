import { useState, type FormEvent } from "react";
import { useT } from "../i18n";
import type { MessageKey } from "../i18n/en";
import { useAuth } from "../app/auth";
import { Solo } from "../app/Solo";
import { Btn, Code, Note, Panel, Seg, TextField } from "../ui";
import { ApiError } from "../lib/api";

/**
 * Sign in.
 *
 * Which methods appear is the server's decision, not this file's: `methods`
 * comes from /v1/auth/methods, so an installation without passwords does not
 * show a password form and this screen stops being the one place in the app
 * that offers a capability the backend may not have.
 *
 * A token is a sign-in method, not a stored credential. It is posted once,
 * exchanged for a session cookie, and never kept here — which is why there is
 * no longer anything in localStorage for an XSS to steal.
 *
 * A single sign-on provider is a link out, not a form: the browser goes to
 * the provider and comes back to /v1/auth/oidc/callback, which either sets
 * the session cookie and lands here signed in, or lands here with
 * `?sign_in_error=` naming one of the reasons below.
 */

// The server sends a code from this fixed set and never the provider's own
// text (adapters/http/oidc.go). Written out rather than derived from the code
// so the i18n check can see every key is rendered.
const oidcErrors: Record<string, MessageKey> = {
  oidc_cancelled: "auth.oidcCancelled",
  oidc_expired: "auth.oidcExpired",
  oidc_unavailable: "auth.oidcUnavailable",
  oidc_no_email: "auth.oidcNoEmail",
  oidc_email_unverified: "auth.oidcEmailUnverified",
  oidc_failed: "auth.oidcFailed",
};

/**
 * Reads the callback's reason once and removes it from the address bar, so a
 * reload or a bookmark does not show a failure from an attempt long gone.
 */
function takeSignInError(): string | null {
  const params = new URLSearchParams(window.location.search);
  const code = params.get("sign_in_error");
  if (code === null) return null;
  params.delete("sign_in_error");
  const rest = params.toString();
  window.history.replaceState(
    null,
    "",
    window.location.pathname + (rest ? `?${rest}` : "") + window.location.hash,
  );
  return code;
}

export function SignIn() {
  const t = useT();
  const { methods, signInWithPassword, signInWithToken } = useAuth();
  const providers = methods.filter((m) => m.kind === "oidc" && m.id);
  const [returned] = useState(takeSignInError);

  const hasPassword = methods.length === 0 || methods.some((m) => m.kind === "password");
  const hasToken = methods.length === 0 || methods.some((m) => m.kind === "token");
  // With a directory configured, the identifier may be a login name rather
  // than an address — a form that insists on "Email" is a form that argues
  // with half the company.
  const hasDirectory = methods.some((m) => m.kind === "ldap" || m.kind === "oidc");

  // Password unless the installation only offers tokens. `methods` is empty
  // when the probe failed, and in that case both are shown rather than
  // neither: a network blip must not hide the way in.
  const [mode, setMode] = useState<"password" | "token">(hasPassword ? "password" : "token");

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(() => {
    if (returned === null) return null;
    // A code this build does not know still means sign-in failed; saying
    // nothing would leave the person back at the form with no idea why.
    // Own keys only. The code arrives in a URL anyone can craft, and a plain
    // lookup would hand `toString` or `__proto__` back as if it were a
    // message key — which crashed this screen.
    const key = Object.hasOwn(oidcErrors, returned) ? oidcErrors[returned] : undefined;
    return key ? t(key) : t("auth.oidcFailed");
  });

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setFailure(null);
    try {
      if (mode === "password") await signInWithPassword(email.trim(), password);
      else await signInWithToken(token.trim());
    } catch (err) {
      // Rate limiting is the one failure worth distinguishing: it is the
      // only one where the answer is "wait", and leaving someone to retype a
      // correct password against a closed door is a cruelty.
      // The three cases worth telling apart. Everything else reads the same
      // on purpose — see auth.indistinct.
      if (err instanceof ApiError && err.kind === "rate_limited") setFailure(t("auth.rateLimited"));
      else if (err instanceof ApiError && err.code === "directory_unavailable")
        setFailure(t("auth.directoryUnavailable"));
      else if (err instanceof ApiError && err.code === "no_directory_email")
        setFailure(t("auth.directoryNoEmail"));
      else if (mode === "token") setFailure(t("auth.tokenFailed"));
      else setFailure(t("auth.failed"));
    } finally {
      setBusy(false);
    }
  }

  const canSubmit =
    mode === "password" ? email.trim() !== "" && password !== "" : token.trim() !== "";

  return (
    <Solo>
      <Panel title={t("auth.title")}>
        {providers.length > 0 && (
          <div className="row wrap mb">
            {providers.map((p) => (
              <Btn
                key={p.id}
                kind="primary"
                // A full navigation, not a fetch: the provider's login page
                // has to be shown to the person, and the callback sets the
                // session cookie on the way back.
                onClick={() =>
                  window.location.assign(`/v1/auth/oidc/${encodeURIComponent(p.id!)}/start`)
                }
              >
                {t("auth.withProvider", { provider: p.label || p.id! })}
              </Btn>
            ))}
          </div>
        )}
        {hasPassword && hasToken && (
          <div className="row mb">
            <Seg
              label={t("auth.title")}
              value={mode}
              onChange={(m) => {
                setMode(m);
                setFailure(null);
              }}
              options={[
                { value: "password" as const, label: t("auth.withPassword") },
                { value: "token" as const, label: t("auth.withToken") },
              ]}
            />
          </div>
        )}

        {mode === "password" ? (
          <form onSubmit={submit}>
            <div className="stack">
              <TextField
                label={hasDirectory ? t("auth.emailOrUsername") : t("auth.email")}
                // Not type="email" once a directory is configured: the browser
                // would refuse to submit a login name on the grounds that it
                // is not an address.
                type={hasDirectory ? "text" : "email"}
                value={email}
                onChange={setEmail}
                autoComplete="username"
                grow
                labelled
              />
              <TextField
                label={t("auth.password")}
                type="password"
                value={password}
                onChange={setPassword}
                autoComplete="current-password"
                grow
                labelled
              />
              <div className="row">
                <Btn kind="primary" type="submit" disabled={busy || !canSubmit}>
                  {busy ? t("common.loading") : t("auth.submit")}
                </Btn>
              </div>
            </div>
          </form>
        ) : (
          <form onSubmit={submit}>
            <p className="sec lead-out">{t("auth.tokenLead", { command: "ccd token create" })}</p>
            <Code>ccd token create --name "web ui"</Code>
            <div className="stack mt">
              <TextField
                label={t("auth.tokenLabel")}
                type="password"
                value={token}
                onChange={setToken}
                autoComplete="off"
                grow
                labelled
              />
              <div className="row">
                <Btn kind="primary" type="submit" disabled={busy || !canSubmit}>
                  {busy ? t("common.loading") : t("auth.submit")}
                </Btn>
              </div>
            </div>
          </form>
        )}

        {failure && (
          <div className="mt">
            <Note tone="danger">{failure}</Note>
          </div>
        )}
      </Panel>

      <div className="mt">
        <Note tone="info">{t("auth.indistinct")}</Note>
      </div>
    </Solo>
  );
}
