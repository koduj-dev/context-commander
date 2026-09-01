import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, type SecretRef } from "../../lib/api";

export default function Secrets({ namespace, slug }: { namespace: string; slug: string }) {
  const [secrets, setSecrets] = useState<SecretRef[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [value, setValue] = useState("");

  async function refresh() {
    try {
      setSecrets(await api.listSecrets(namespace, slug));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load secrets");
    }
  }

  useEffect(() => {
    void refresh();
  }, [namespace, slug]);

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
      <p className="text-sm text-neutral-400">Secret values are never returned by the API — only their names and references.</p>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <ul className="divide-y divide-neutral-800 rounded border border-neutral-800">
        {secrets === null && <li className="px-3 py-2 text-sm text-neutral-500">Loading…</li>}
        {secrets?.length === 0 && <li className="px-3 py-2 text-sm text-neutral-500">No secrets yet.</li>}
        {secrets?.map((s) => (
          <li key={s.name} className="flex items-center justify-between px-3 py-2 text-sm">
            <span className="font-mono">{s.reference}</span>
            <button onClick={() => void onDelete(s.name)} className="text-xs text-red-400 hover:text-red-300">
              Delete
            </button>
          </li>
        ))}
      </ul>
      <form onSubmit={onCreate} className="flex flex-col gap-2">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="secret name"
          className="rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
        />
        <input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          type="password"
          placeholder="value"
          className="rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
        />
        <button
          type="submit"
          disabled={!name.trim() || !value}
          className="self-start rounded bg-neutral-100 px-3 py-1.5 text-sm font-medium text-neutral-900 disabled:opacity-50"
        >
          Create secret
        </button>
      </form>
    </div>
  );
}
