import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, type Credential } from "../lib/api";

export default function AccountTokens() {
  const [tokens, setTokens] = useState<Credential[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [clientType, setClientType] = useState("");
  const [expiresInDays, setExpiresInDays] = useState("");
  const [revealed, setRevealed] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

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
      <h1 className="text-lg font-semibold">API Tokens</h1>
      <p className="text-sm text-neutral-500">Personal access tokens for this account. Each is shown once, at issuance.</p>
      {error && <p className="text-sm text-red-400">{error}</p>}

      {revealed && (
        <div className="rounded border border-amber-700 bg-amber-950/40 p-3 text-sm">
          <p className="mb-2 text-amber-300">Copy this token now — it will not be shown again.</p>
          <div className="flex items-center gap-2">
            <code className="flex-1 select-all break-all rounded bg-neutral-950 px-2 py-1 text-xs">{revealed}</code>
            <button onClick={() => void onCopy()} className="rounded bg-neutral-100 px-2 py-1 text-xs font-medium text-neutral-900">
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
          <button onClick={() => setRevealed(null)} className="mt-2 text-xs text-neutral-400 hover:text-neutral-200">
            Dismiss
          </button>
        </div>
      )}

      <ul className="divide-y divide-neutral-800 rounded border border-neutral-800 text-sm">
        {tokens === null && <li className="px-3 py-2 text-neutral-500">Loading…</li>}
        {tokens?.length === 0 && <li className="px-3 py-2 text-neutral-500">No tokens yet.</li>}
        {tokens?.map((c) => (
          <li key={c.id} className="flex items-center justify-between px-3 py-2">
            <span>
              {c.client_type} <span className="text-xs text-neutral-500">created {c.created_at.slice(0, 10)}</span>
            </span>
            <span className="flex items-center gap-3 text-xs text-neutral-500">
              {c.revoked_at ? "revoked" : c.expires_at ? `expires ${c.expires_at.slice(0, 10)}` : "no expiry"}
              {!c.revoked_at && (
                <button onClick={() => void onRevoke(c.id)} className="text-red-400 hover:text-red-300">
                  Revoke
                </button>
              )}
            </span>
          </li>
        ))}
      </ul>

      <form onSubmit={onIssue} className="flex flex-col gap-2 rounded border border-neutral-800 bg-neutral-950 p-3">
        <div className="flex gap-2">
          <input
            value={clientType}
            onChange={(e) => setClientType(e.target.value)}
            placeholder="client_type (e.g. cli, mcp)"
            className="flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
          />
          <input
            value={expiresInDays}
            onChange={(e) => setExpiresInDays(e.target.value)}
            placeholder="expires in days (optional)"
            type="number"
            min="1"
            className="w-48 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
          />
        </div>
        <button
          type="submit"
          disabled={!clientType.trim()}
          className="self-start rounded bg-neutral-100 px-3 py-1.5 text-sm font-medium text-neutral-900 disabled:opacity-50"
        >
          Issue token
        </button>
      </form>
    </div>
  );
}
