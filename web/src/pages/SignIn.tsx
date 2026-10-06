import { useState, type FormEvent } from "react";
import { useT } from "../i18n";
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
 */
export function SignIn() {
  const t = useT();
  const { methods, signInWithPassword, signInWithToken } = useAuth();

  const hasPassword = methods.length === 0 || methods.some((m) => m.kind === "password");
  const hasToken = methods.length === 0 || methods.some((m) => m.kind === "token");

  // Password unless the installation only offers tokens. `methods` is empty
  // when the probe failed, and in that case both are shown rather than
  // neither: a network blip must not hide the way in.
  const [mode, setMode] = useState<"password" | "token">(hasPassword ? "password" : "token");

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

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
      if (err instanceof ApiError && err.kind === "rate_limited") setFailure(t("auth.rateLimited"));
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
                label={t("auth.email")}
                type="email"
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
