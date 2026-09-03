import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, type Credential } from "../lib/api";
import PageHeader from "../components/PageHeader";
import EmptyState from "../components/EmptyState";
import StatusBadge from "../components/StatusBadge";
import ConfirmDialog from "../components/ConfirmDialog";

export default function AccountTokens() {
  const [tokens, setTokens] = useState<Credential[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [clientType, setClientType] = useState("");
  const [expiresInDays, setExpiresInDays] = useState("");
  const [revealed, setRevealed] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [confirmRevoke, setConfirmRevoke] = useState<string | null>(null);

  async function refresh() {
    try {
      setTokens(await api.listTokens());
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load tokens");
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  async function onIssue(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setCopied(false);
    try {
      const days = expiresInDays.trim() ? Number(expiresInDays) : undefined;
      const result = await api.issueToken(clientType.trim(), days ? days * 86400 : undefined);
      setRevealed(result.token);
      setClientType("");
      setExpiresInDays("");
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to issue token");
    }
  }

  async function onRevoke(id: string) {
    setError(null);
    try {
      await api.revokeToken(id);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to revoke token");
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

  return (
    <div className="max-w-2xl space-y-4">
      <PageHeader title="API Tokens" description="Personal access tokens for this account. Each is shown once, at issuance." />
      {error && <p className="text-sm text-cc-danger">{error}</p>}

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
        {tokens === null && (
          <li>
            <EmptyState message="Loading…" />
          </li>
        )}
        {tokens?.length === 0 && (
          <li>
            <EmptyState message="No tokens yet." />
          </li>
        )}
        {tokens?.map((c) => (
          <li key={c.id} className="flex items-center justify-between px-3 py-2">
            <span>
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

      <form onSubmit={onIssue} className="flex flex-col gap-2 rounded border border-cc-border bg-cc-surface-1 p-3">
        <div className="flex gap-2">
          <input
            value={clientType}
            onChange={(e) => setClientType(e.target.value)}
            placeholder="client_type (e.g. cli, mcp)"
            className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
          />
          <input
            value={expiresInDays}
            onChange={(e) => setExpiresInDays(e.target.value)}
            placeholder="expires in days (optional)"
            type="number"
            min="1"
            className="w-48 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
          />
        </div>
        <button
          type="submit"
          disabled={!clientType.trim()}
          className="self-start rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50"
        >
          Issue token
        </button>
      </form>
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
