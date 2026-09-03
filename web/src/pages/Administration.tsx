import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, type InstallationAdmin, type Credential } from "../lib/api";
import PageHeader from "../components/PageHeader";
import EmptyState from "../components/EmptyState";
import StatusBadge from "../components/StatusBadge";
import ConfirmDialog from "../components/ConfirmDialog";

const inputClass =
  "rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong";
const buttonClass = "self-start rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50";

export default function Administration() {
  const [admins, setAdmins] = useState<InstallationAdmin[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [userEmail, setUserEmail] = useState("");
  const [userName, setUserName] = useState("");
  const [userStatus, setUserStatus] = useState<string | null>(null);

  const [spName, setSpName] = useState("");
  const [spDescription, setSpDescription] = useState("");
  const [spStatus, setSpStatus] = useState<string | null>(null);

  const [lookupPrincipalId, setLookupPrincipalId] = useState("");
  const [targetPrincipalId, setTargetPrincipalId] = useState<string | null>(null);
  const [credentials, setCredentials] = useState<Credential[] | null>(null);
  const [issueClientType, setIssueClientType] = useState("");
  const [issueExpiresInDays, setIssueExpiresInDays] = useState("");
  const [revealed, setRevealed] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [confirmRevoke, setConfirmRevoke] = useState<string | null>(null);

  async function refreshAdmins() {
    try {
      setAdmins(await api.listInstallationAdmins());
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load admins");
    }
  }

  useEffect(() => {
    void refreshAdmins();
  }, []);

  async function onCreateUser(e: FormEvent) {
    e.preventDefault();
    setUserStatus(null);
    try {
      const u = await api.createInstallationUser(userEmail.trim(), userName.trim());
      setUserStatus(`Created ${u.email} — principal id resolves once they authenticate; find it via Members/Audit if needed.`);
      setUserEmail("");
      setUserName("");
    } catch (err) {
      setUserStatus(err instanceof ApiError ? err.message : "failed to create user");
    }
  }

  async function onCreateServicePrincipal(e: FormEvent) {
    e.preventDefault();
    setSpStatus(null);
    try {
      const sp = await api.createServicePrincipal(spName.trim(), spDescription.trim() || undefined);
      setSpStatus(`Registered "${sp.name}". Look up its Principal ID (not this ${sp.id}) to issue it a token below.`);
      setSpName("");
      setSpDescription("");
    } catch (err) {
      setSpStatus(err instanceof ApiError ? err.message : "failed to register service principal");
    }
  }

  async function refreshCredentials(principalId: string) {
    try {
      setCredentials(await api.listCredentialsFor(principalId));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load tokens");
    }
  }

  async function onLoadCredentials(e: FormEvent) {
    e.preventDefault();
    setError(null);
    const id = lookupPrincipalId.trim();
    setTargetPrincipalId(id);
    setCredentials(null);
    setRevealed(null);
    await refreshCredentials(id);
  }

  async function onIssueCredential(e: FormEvent) {
    e.preventDefault();
    if (!targetPrincipalId) return;
    setError(null);
    setCopied(false);
    try {
      const days = issueExpiresInDays.trim() ? Number(issueExpiresInDays) : undefined;
      const result = await api.issueCredentialFor(targetPrincipalId, issueClientType.trim(), days ? days * 86400 : undefined);
      setRevealed(result.token);
      setIssueClientType("");
      setIssueExpiresInDays("");
      await refreshCredentials(targetPrincipalId);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to issue token");
    }
  }

  async function onCopy() {
    if (!revealed) return;
    try {
      await navigator.clipboard.writeText(revealed);
      setCopied(true);
    } catch {
      // clipboard access can be denied — the token is still selectable text
    }
  }

  async function onRevoke(id: string) {
    if (!targetPrincipalId) return;
    setError(null);
    try {
      await api.revokeCredentialFor(id);
      await refreshCredentials(targetPrincipalId);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to revoke token");
    }
  }

  return (
    <div className="max-w-3xl space-y-10">
      <PageHeader
        title="Administration"
        description="Installation-wide administration — create users, register agents, and manage any principal's tokens."
      />
      {error && <p className="text-sm text-cc-danger">{error}</p>}

      <section>
        <h2 className="mb-3 text-lg font-semibold text-cc-text">Installation admins</h2>
        <ul className="divide-y divide-cc-border rounded border border-cc-border text-sm">
          {admins === null && (
            <li>
              <EmptyState message="Loading…" />
            </li>
          )}
          {admins?.length === 0 && (
            <li>
              <EmptyState message="No installation admins." />
            </li>
          )}
          {admins?.map((a) => (
            <li key={a.user_id} className="flex items-center justify-between px-3 py-2">
              <span className="font-mono text-xs text-cc-text">{a.user_id}</span>
              <span className="flex items-center gap-3 text-xs text-cc-text-muted">
                <StatusBadge tone="neutral">{a.role}</StatusBadge>
                {new Date(a.granted_at).toLocaleDateString()}
              </span>
            </li>
          ))}
        </ul>
        <p className="mt-2 text-xs text-cc-text-muted">
          Granting or revoking admin rights is an operator-only CLI action (
          <code className="rounded bg-cc-surface-2 px-1 py-0.5 text-cc-text">ccd installation grant-admin</code>) — there is no
          button for it here, by design (docs/adr/0017).
        </p>
      </section>

      <section>
        <h2 className="mb-3 text-lg font-semibold text-cc-text">Create a user</h2>
        <form onSubmit={onCreateUser} className="flex max-w-md flex-col gap-2">
          <input value={userEmail} onChange={(e) => setUserEmail(e.target.value)} placeholder="email" className={inputClass} />
          <input value={userName} onChange={(e) => setUserName(e.target.value)} placeholder="display name" className={inputClass} />
          <button type="submit" disabled={!userEmail.trim() || !userName.trim()} className={buttonClass}>
            Create user
          </button>
        </form>
        {userStatus && <p className="mt-2 text-sm text-cc-text-muted">{userStatus}</p>}
      </section>

      <section>
        <h2 className="mb-3 text-lg font-semibold text-cc-text">Register a service principal (agent)</h2>
        <form onSubmit={onCreateServicePrincipal} className="flex max-w-md flex-col gap-2">
          <input value={spName} onChange={(e) => setSpName(e.target.value)} placeholder="name" className={inputClass} />
          <input
            value={spDescription}
            onChange={(e) => setSpDescription(e.target.value)}
            placeholder="description (optional)"
            className={inputClass}
          />
          <button type="submit" disabled={!spName.trim()} className={buttonClass}>
            Register
          </button>
        </form>
        {spStatus && <p className="mt-2 text-sm text-cc-text-muted">{spStatus}</p>}
      </section>

      <section>
        <h2 className="mb-3 text-lg font-semibold text-cc-text">Manage a principal's tokens</h2>
        <p className="mb-2 text-xs text-cc-text-muted">
          Enter a Principal ID (not a user or service-principal id — see Members/Audit for how to find one) to issue, list, or
          revoke its tokens.
        </p>
        <form onSubmit={onLoadCredentials} className="flex max-w-md gap-2">
          <input
            value={lookupPrincipalId}
            onChange={(e) => setLookupPrincipalId(e.target.value)}
            placeholder="principal id (UUID)"
            className={`flex-1 ${inputClass}`}
          />
          <button type="submit" disabled={!lookupPrincipalId.trim()} className={buttonClass}>
            Load
          </button>
        </form>

        {targetPrincipalId && (
          <div className="mt-4 max-w-2xl space-y-4">
            {revealed && (
              <div className="rounded border border-cc-warning/40 bg-cc-surface-2 p-3 text-sm">
                <p className="mb-2 text-cc-warning">Copy this token now — it will not be shown again.</p>
                <div className="flex items-center gap-2">
                  <code className="flex-1 select-all break-all rounded bg-cc-surface-1 px-2 py-1 text-xs text-cc-text">{revealed}</code>
                  <button onClick={() => void onCopy()} className="rounded bg-cc-accent-primary px-2 py-1 text-xs font-medium text-cc-void">
                    {copied ? "Copied" : "Copy"}
                  </button>
                </div>
                <button onClick={() => setRevealed(null)} className="mt-2 text-xs text-cc-text-muted hover:text-cc-text">
                  Dismiss
                </button>
              </div>
            )}

            <ul className="divide-y divide-cc-border rounded border border-cc-border text-sm">
              {credentials === null && (
                <li>
                  <EmptyState message="Loading…" />
                </li>
              )}
              {credentials?.length === 0 && (
                <li>
                  <EmptyState message="No tokens for this principal." />
                </li>
              )}
              {credentials?.map((c) => (
                <li key={c.id} className="flex items-center justify-between px-3 py-2">
                  <span className="text-cc-text">
                    {c.client_type} <span className="text-xs text-cc-text-muted">created {c.created_at.slice(0, 10)}</span>
                  </span>
                  <span className="flex items-center gap-3 text-xs">
                    {c.revoked_at ? (
                      <StatusBadge tone="danger">revoked</StatusBadge>
                    ) : c.expires_at ? (
                      <StatusBadge tone="neutral">{`expires ${c.expires_at.slice(0, 10)}`}</StatusBadge>
                    ) : (
                      <StatusBadge tone="success">no expiry</StatusBadge>
                    )}
                    {!c.revoked_at && (
                      <button onClick={() => setConfirmRevoke(c.id)} className="text-cc-danger hover:opacity-80">
                        Revoke
                      </button>
                    )}
                  </span>
                </li>
              ))}
            </ul>

            <form onSubmit={onIssueCredential} className="flex flex-col gap-2 rounded border border-cc-border bg-cc-surface-1 p-3">
              <div className="flex gap-2">
                <input
                  value={issueClientType}
                  onChange={(e) => setIssueClientType(e.target.value)}
                  placeholder="client_type (e.g. cli, mcp)"
                  className={`flex-1 ${inputClass}`}
                />
                <input
                  value={issueExpiresInDays}
                  onChange={(e) => setIssueExpiresInDays(e.target.value)}
                  placeholder="expires in days (optional)"
                  type="number"
                  min="1"
                  className={`w-48 ${inputClass}`}
                />
              </div>
              <button type="submit" disabled={!issueClientType.trim()} className={buttonClass}>
                Issue token
              </button>
            </form>
          </div>
        )}
      </section>

      <ConfirmDialog
        open={confirmRevoke !== null}
        title="Revoke token"
        message="Revoke this token? Any client using it will immediately lose access."
        confirmLabel="Revoke"
        danger
        onCancel={() => setConfirmRevoke(null)}
        onConfirm={() => {
          const target = confirmRevoke!;
          setConfirmRevoke(null);
          void onRevoke(target);
        }}
      />
    </div>
  );
}
