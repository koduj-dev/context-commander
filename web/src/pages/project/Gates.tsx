import { useEffect, useState } from "react";
import { api, ApiError, type GateBinding } from "../../lib/api";

export default function Gates({ namespace, slug }: { namespace: string; slug: string }) {
  const [bindings, setBindings] = useState<GateBinding[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .listGateBindings(namespace, slug)
      .then(setBindings)
      .catch((err) => setError(err instanceof ApiError ? err.message : "failed to load gates"));
  }, [namespace, slug]);

  return (
    <div className="max-w-2xl space-y-4">
      <p className="text-sm text-neutral-400">Read-only view of gate bindings on this project — bind/unbind from the CLI.</p>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <ul className="divide-y divide-neutral-800 rounded border border-neutral-800 text-sm">
        {bindings === null && <li className="px-3 py-2 text-neutral-500">Loading…</li>}
        {bindings?.length === 0 && <li className="px-3 py-2 text-neutral-500">No gates bound.</li>}
        {bindings?.map((g) => (
          <li key={g.id} className="flex items-center justify-between px-3 py-2">
            <span>{g.gate_name}</span>
            <span className="text-xs text-neutral-500">{g.hook}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
