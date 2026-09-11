import { useState, type FormEvent } from "react";
import { api, ApiError, type SecretRef } from "../../lib/api";
import { useCursorList } from "../../lib/useCursorList";
import EmptyState from "../../components/EmptyState";
import ConfirmDialog from "../../components/ConfirmDialog";
import LoadMore from "../../components/LoadMore";

export default function Secrets({ namespace, slug }: { namespace: string; slug: string }) {
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  // Creating or deleting a secret refreshes back to the first page rather
  // than patching the loaded list: names are ordered, so a new one can belong
  // anywhere, including on a page that has not been loaded.
  const {
    items: secrets,
    error,
    setError,
    hasMore,
    loadingMore,
    loadMore,
    refresh,
  } = useCursorList<SecretRef>(
    (opts) => api.listSecrets(namespace, slug, opts),
    [namespace, slug],
    "failed to load secrets",
  );

  async function onCreate(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await api.createSecret(namespace, slug, name.trim(), value);
      setName("");
      setValue("");
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to create secret");
    }
  }

  async function onDelete(secretName: string) {
    setError(null);
    try {
      await api.deleteSecret(namespace, slug, secretName);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to delete secret");
    }
  }

  return (
    <div className="max-w-lg space-y-4">
      <p className="text-sm text-cc-text-muted">Secret values are never returned by the API — only their names and references.</p>
      {error && <p className="text-sm text-cc-danger">{error}</p>}
      <ul className="divide-y divide-cc-border rounded border border-cc-border">
        {secrets === null && (
          <li>
            <EmptyState message="Loading…" />
          </li>
        )}
        {secrets?.length === 0 && (
          <li>
            <EmptyState message="No secrets yet." />
          </li>
        )}
        {secrets?.map((s) => (
          <li key={s.name} className="flex items-center justify-between px-3 py-2 text-sm">
            <span className="font-mono">{s.reference}</span>
            <button onClick={() => setConfirmDelete(s.name)} className="text-xs text-cc-danger hover:opacity-80">
              Delete
            </button>
          </li>
        ))}
      </ul>
      <LoadMore hasMore={hasMore} loading={loadingMore} onClick={() => void loadMore()} />
      <form onSubmit={onCreate} className="flex flex-col gap-2">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="secret name"
          className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
        />
        <input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          type="password"
          placeholder="value"
          className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
        />
        <button
          type="submit"
          disabled={!name.trim() || !value}
          className="self-start rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50"
        >
          Create secret
        </button>
      </form>
      <ConfirmDialog
        open={confirmDelete !== null}
        title="Delete secret"
        message={`Delete secret "${confirmDelete}"? This cannot be undone.`}
        confirmLabel="Delete"
        danger
        onCancel={() => setConfirmDelete(null)}
        onConfirm={() => {
          const target = confirmDelete!;
          setConfirmDelete(null);
          void onDelete(target);
        }}
      />
    </div>
  );
}
