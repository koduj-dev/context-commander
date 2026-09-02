import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, type Member } from "../../lib/api";
import { canManageMembers } from "../../lib/roles";

const ROLES = ["OWNER", "ADMIN", "CONTRIBUTOR", "READER"];

export default function Members({ namespace, slug, role }: { namespace: string; slug: string; role: string }) {
  const [members, setMembers] = useState<Member[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [principalId, setPrincipalId] = useState("");
  const [newRole, setNewRole] = useState("READER");
  const [status, setStatus] = useState<string | null>(null);

  async function refresh() {
    try {
      setMembers(await api.listMembers(namespace, slug));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load members");
    }
  }

  useEffect(() => {
    void refresh();
  }, [namespace, slug]);

  async function onAdd(e: FormEvent) {
    e.preventDefault();
    setStatus(null);
    try {
      await api.addMember(namespace, slug, principalId.trim(), newRole);
      setStatus(`Added ${principalId.trim()} as ${newRole}.`);
      setPrincipalId("");
      await refresh();
    } catch (err) {
      setStatus(err instanceof ApiError ? err.message : "failed to add member");
    }
  }

  return (
    <div className="max-w-md space-y-4">
      {error && <p className="text-sm text-red-400">{error}</p>}
      <ul className="divide-y divide-neutral-800 rounded border border-neutral-800 text-sm">
        {members === null && <li className="px-3 py-2 text-neutral-500">Loading…</li>}
        {members?.length === 0 && <li className="px-3 py-2 text-neutral-500">No members.</li>}
        {members?.map((m) => (
          <li key={m.principal_id} className="flex items-center justify-between px-3 py-2">
            <span>{m.email ?? m.principal_id}</span>
            <span className="text-xs text-neutral-500">{m.role}</span>
          </li>
        ))}
      </ul>

      {canManageMembers(role) && (
        <form onSubmit={onAdd} className="flex flex-col gap-2">
          <input
            value={principalId}
            onChange={(e) => setPrincipalId(e.target.value)}
            placeholder="principal id (UUID)"
            className="rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
          />
          <select
            value={newRole}
            onChange={(e) => setNewRole(e.target.value)}
            className="rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
          >
            {ROLES.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>
          <button
            type="submit"
            disabled={!principalId.trim()}
            className="self-start rounded bg-neutral-100 px-3 py-1.5 text-sm font-medium text-neutral-900 disabled:opacity-50"
          >
            Add member
          </button>
        </form>
      )}
      {status && <p className="text-sm text-neutral-400">{status}</p>}
    </div>
  );
}
