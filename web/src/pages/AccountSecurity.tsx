import { useState } from "react";
import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import { useAuth } from "../app/auth";
import { ApiError, api } from "../lib/api";
import { useResource } from "../lib/resource";
import { meterTone, usedPercent } from "./AdminStorage";
import {
  Badge,
  Btn,
  Cell,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Meter,
  Mono,
  Note,
  Panel,
  TextField,
} from "../ui";

/** Mirrors identity.MinPasswordLength, so the form states the rule the server enforces. */
const MIN_PASSWORD = 12;

/**
 * Setting or changing the password this account signs in with.
 *
 * The current password is asked for only when one exists. That is not a gap:
 * someone signed in with an access token already holds a valid credential for
 * this account, which is stronger proof than knowing the old password — and
 * without it, anyone who arrived by token could never set a first password
 * without an administrator.
 */
export function PasswordPanel() {
  const t = useT();
  const { me } = useAuth();
  // Asked of the server rather than guessed from `me`: whether a password
  // exists is not in /v1/me, and inferring it from "signed in with a token"
  // would be wrong for anyone who has both.
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  const isUser = me?.kind === "user";
  // Someone who arrived with an access token has no password to confirm, so
  // asking for one would be asking for something that does not exist.
  const hasPassword = me?.has_password ?? true;

  async function submit() {
    if (busy) return;
    setFailure(null);
    setDone(false);
    if (next !== again) {
      setFailure(t("account.passwordMismatch"));
      return;
    }
    setBusy(true);
    try {
      await api.changePassword(current, next);
      setDone(true);
      setCurrent("");
      setNext("");
      setAgain("");
    } catch (err) {
      if (err instanceof ApiError && err.kind === "forbidden")
        setFailure(t("account.currentWrong"));
      else if (err instanceof ApiError) setFailure(err.message);
      else setFailure(String(err));
    } finally {
      setBusy(false);
    }
  }

  // A service principal has no password to set, and saying so beats a form
  // that would 400 on submit.
  if (!isUser) return null;

  const canSubmit = next.length >= MIN_PASSWORD && again.length >= MIN_PASSWORD;

  return (
    <Panel title={t("account.password")}>
      <p className="sec lead-out">{t("account.passwordLead")}</p>

      {!hasPassword && (
        <div className="mb">
          <Note tone="info">{t("account.noPasswordYet")}</Note>
        </div>
      )}

      <div className="stack">
        {hasPassword && (
          <TextField
            label={t("account.currentPassword")}
            type="password"
            value={current}
            onChange={setCurrent}
            autoComplete="current-password"
            grow
            labelled
          />
        )}
        <TextField
          label={t("account.newPassword")}
          type="password"
          value={next}
          onChange={setNext}
          autoComplete="new-password"
          grow
          labelled
        />
        <TextField
          label={t("account.confirmPassword")}
          type="password"
          value={again}
          onChange={setAgain}
          autoComplete="new-password"
          grow
          labelled
        />
        <p className="sec">{t("setup.passwordHint", { min: String(MIN_PASSWORD) })}</p>
        <div className="row">
          <Btn kind="primary" onClick={submit} disabled={busy || !canSubmit}>
            {busy
              ? t("common.loading")
              : hasPassword
                ? t("account.changePassword")
                : t("account.setPassword")}
          </Btn>
        </div>
      </div>

      {failure && (
        <div className="mt">
          <Note tone="danger">{failure}</Note>
        </div>
      )}
      {done && (
        <div className="mt">
          <Note tone="info">{t("account.passwordSet")}</Note>
        </div>
      )}
    </Panel>
  );
}

/**
 * Where this account is signed in, and how to end one of those sessions.
 *
 * Worth a screen because a session is a real credential: ending one makes its
 * cookie stop working immediately, wherever it is. The row for this browser
 * is marked rather than hidden — someone looking for "sign me out everywhere"
 * needs to see it, and signing out of the current session is just signing
 * out.
 */
export function SessionsPanel() {
  const t = useT();
  const f = useFormat();
  const { signOut } = useAuth();
  const { data, error, loading, reload } = useResource(() => api.sessions(), []);
  const [busy, setBusy] = useState<string | null>(null);

  async function end(id: string, current: boolean) {
    setBusy(id);
    try {
      await api.revokeSession(id);
      // Ending the current session means this browser no longer has a
      // credential; anything but routing to sign-in would leave a shell that
      // 401s on the next click.
      if (current) await signOut();
      else reload();
    } finally {
      setBusy(null);
    }
  }

  const sessions = data ?? [];

  return (
    <Panel title={t("account.sessions")}>
      <p className="sec lead-out">{t("account.sessionsLead")}</p>

      {loading && <Loading />}
      {error !== undefined && <ErrorState error={error} onRetry={reload} />}
      {!loading && error === undefined && sessions.length === 0 && (
        <EmptyState title={t("account.noSessions")} />
      )}
      {!loading && error === undefined && sessions.length > 0 && (
        <DataTable
          columns={[
            { key: "started", label: t("account.sessionStarted") },
            { key: "used", label: t("account.sessionLastUsed"), align: "r" },
            { key: "expires", label: t("account.sessionExpires"), align: "r" },
            { key: "actions", align: "r" },
          ]}
        >
          {sessions.map((s) => (
            <tr key={s.id}>
              <td>
                <Cell
                  primary={
                    s.current ? (
                      <Badge tone="active">{t("account.sessionCurrent")}</Badge>
                    ) : (
                      f.stamp(s.created_at)
                    )
                  }
                  secondary={
                    s.current ? <span className="dim">{f.stamp(s.created_at)}</span> : undefined
                  }
                />
              </td>
              <td className="r">
                <span className="dim">
                  {s.last_used_at ? f.relative(s.last_used_at) : t("common.never")}
                </span>
              </td>
              <td className="r">
                <span className="dim">{s.expires_at ? f.stamp(s.expires_at) : "—"}</span>
              </td>
              <td className="r">
                <Btn
                  small
                  kind="danger"
                  disabled={busy === s.id}
                  onClick={() => end(s.id, s.current)}
                >
                  {t("account.endSession")}
                </Btn>
              </td>
            </tr>
          ))}
        </DataTable>
      )}
    </Panel>
  );
}

/**
 * How much room the reader's own namespace and their organizations' have
 * (docs/adr/0021).
 *
 * Shown to everyone, not only to admins: a write refused for want of space is
 * the reader's problem to solve, and this is where they can see it coming. It
 * shows the limit that applies, not whether an admin set it — how the number
 * was arrived at is an administrator's business.
 */
export function StoragePanel() {
  const t = useT();
  const f = useFormat();
  const { data, error, loading, reload } = useResource(() => api.myStorage(), []);

  return (
    <Panel
      title={t("prefs.storage.title")}
      tight
      footer={<span className="dim">{t("prefs.storage.help")}</span>}
    >
      {loading ? (
        <Loading />
      ) : error || !data ? (
        <ErrorState error={error} onRetry={reload} />
      ) : data.items.length === 0 ? (
        <EmptyState title={t("prefs.storage.none")} />
      ) : (
        <DataTable columns={[{ key: "who" }, { key: "used", align: "r" }]}>
          {data.items.map((n) => {
            const percent = usedPercent(n);
            return (
              <tr key={n.namespace_id}>
                <td>
                  <Cell primary={n.display_name} secondary={<Mono>{n.slug}</Mono>} />
                </td>
                <td className="r">
                  {n.limit_bytes === undefined
                    ? t("prefs.storage.unlimited", { used: f.bytes(n.used_bytes) })
                    : t("prefs.storage.ofLimit", {
                        used: f.bytes(n.used_bytes),
                        limit: f.bytes(n.limit_bytes),
                      })}
                  {percent !== undefined && (
                    <div className="mt-sm">
                      <Meter percent={percent} tone={meterTone(percent)} />
                    </div>
                  )}
                </td>
              </tr>
            );
          })}
        </DataTable>
      )}
    </Panel>
  );
}
