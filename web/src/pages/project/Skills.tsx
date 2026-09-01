import { useEffect, useState } from "react";
import { api, ApiError, type EffectiveSkillBinding } from "../../lib/api";

export default function Skills({ namespace, slug }: { namespace: string; slug: string }) {
  const [bindings, setBindings] = useState<EffectiveSkillBinding[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .listEffectiveSkills(namespace, slug)
      .then(setBindings)
      .catch((err) => setError(err instanceof ApiError ? err.message : "failed to load skills"));
  }, [namespace, slug]);

  return (
    <div className="max-w-2xl space-y-4">
      <p className="text-sm text-neutral-400">Read-only view of skills effective on this project — install/publish from the CLI.</p>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <ul className="divide-y divide-neutral-800 rounded border border-neutral-800 text-sm">
        {bindings === null && <li className="px-3 py-2 text-neutral-500">Loading…</li>}
        {bindings?.length === 0 && <li className="px-3 py-2 text-neutral-500">No skills installed.</li>}
        {bindings?.map((b) => (
          <li key={b.skill_version_id} className="flex items-center justify-between px-3 py-2">
            <span>{b.skill_name}</span>
            <span className="text-xs text-neutral-500">
              {b.scope}
              {b.pinned ? " · pinned" : ""}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
