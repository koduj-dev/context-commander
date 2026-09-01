import { useState, type FormEvent } from "react";
import { api, ApiError } from "../../lib/api";
import { canManageMembers } from "../../lib/roles";

const ROLES = ["OWNER", "ADMIN", "CONTRIBUTOR", "READER"];

export default function Members({ namespace, slug, role }: { namespace: string; slug: string; role: string }) {
  const [principalId, setPrincipalId] = useState("");
  const [newRole, setNewRole] = useState("READER");
  const [status, setStatus] = useState<string | null>(null);

  if (!canManageMembers(role)) {
    return <p className="text-sm text-neutral-500">Members can be listed by an ADMIN or OWNER of this project.</p>;
  }

  async function onAdd(e: FormEvent) {
    e.preventDefault();
    setStatus(null);
    try {
      await api.addMember(namespace, slug, principalId.trim(), newRole);
      setStatus(`Added ${principalId.trim()} as ${newRole}.`);
      setPrincipalId("");
    } catch (err) {
      setStatus(err instanceof ApiError ? err.message : "failed to add member");
    }
  }

  return (
    <div className="max-w-md space-y-4">
      <p className="text-sm text-neutral-400">
        There is no "list members" endpoint yet — add a principal by id below. See CLI's <code>ccd project add-member</code> for
        listing.
      </p>
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
      {status && <p className="text-sm text-neutral-400">{status}</p>}
    </div>
  );
}
