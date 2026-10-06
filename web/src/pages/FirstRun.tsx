import { useState, type FormEvent } from "react";
import { useT } from "../i18n";
import { useAuth } from "../app/auth";
import { Solo } from "../app/Solo";
import { ApiError } from "../lib/api";
import { Btn, Note, Panel, TextField } from "../ui";

/** Mirrors identity.MinPasswordLength, so the form says the rule the server enforces. */
const MIN_PASSWORD = 12;

/**
 * First run: create the first administrator in the browser.
 *
 * This screen exists because the alternative was a shell on the server. A
 * self-hosted product whose first step is "ssh in and run a CLI" has its
 * front door locked from the inside — and the maintainer said so in exactly
 * those terms.
 *
 * It is reachable only while the installation has no users, which the server
 * re-checks on the request itself. The code it asks for lives only in the
 * running server's memory, which is said out loud on the screen: someone who
 * cannot find it should learn that restarting prints a new one, not go
 * looking in the database for it.
 */
export function FirstRun() {
  const t = useT();
  const { completeSetup } = useAuth();

  const [code, setCode] = useState("");
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setFailure(null);
    try {
      await completeSetup(code.trim(), email.trim(), name.trim(), password);
    } catch (err) {
      if (err instanceof ApiError && err.kind === "unauthorized") setFailure(t("setup.codeFailed"));
      else if (err instanceof ApiError && err.kind === "conflict")
        setFailure(t("setup.alreadyDone"));
      // Anything else is the server's own message — a password rule, a
      // duplicate address — and it is more specific than anything this
      // screen could guess at.
      else if (err instanceof ApiError) setFailure(err.message);
      else setFailure(t("error.unavailable.body"));
    } finally {
      setBusy(false);
    }
  }

  const canSubmit =
    code.trim() !== "" &&
    email.trim() !== "" &&
    name.trim() !== "" &&
    password.length >= MIN_PASSWORD;

  return (
    <Solo>
      <Panel title={t("setup.title")}>
        <p className="sec lead-out">{t("setup.lead")}</p>

        <form onSubmit={submit}>
          <div className="stack">
            <TextField
              label={t("setup.code")}
              value={code}
              onChange={setCode}
              autoComplete="off"
              grow
              labelled
            />
            <TextField
              label={t("setup.email")}
              type="email"
              value={email}
              onChange={setEmail}
              autoComplete="username"
              grow
              labelled
            />
            <TextField
              label={t("setup.name")}
              value={name}
              onChange={setName}
              autoComplete="name"
              grow
              labelled
            />
            <TextField
              label={t("setup.password")}
              type="password"
              value={password}
              onChange={setPassword}
              autoComplete="new-password"
              grow
              labelled
            />
            <p className="sec">{t("setup.passwordHint", { min: String(MIN_PASSWORD) })}</p>
            <div className="row">
              <Btn kind="primary" type="submit" disabled={busy || !canSubmit}>
                {busy ? t("common.loading") : t("setup.submit")}
              </Btn>
            </div>
          </div>
        </form>

        {failure && (
          <div className="mt">
            <Note tone="danger">{failure}</Note>
          </div>
        )}
      </Panel>

      <div className="mt">
        <Note tone="info">{t("setup.whereIsTheCode")}</Note>
      </div>
    </Solo>
  );
}
