import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, type Member } from "../../lib/api";
import { canManageMembers } from "../../lib/roles";
import DataTable from "../../components/DataTable";
import PrincipalBadge from "../../components/PrincipalBadge";
import StatusBadge from "../../components/StatusBadge";
import EmptyState from "../../components/EmptyState";

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
      {error && <p className="text-sm text-cc-danger">{error}</p>}
      <DataTable
        columns={[
          { key: "principal", header: "Principal", render: (m) => <PrincipalBadge principalId={m.principal_id} email={m.email} displayName={m.display_name} /> },
          { key: "role", header: "Role", render: (m) => <StatusBadge tone="neutral">{m.role}</StatusBadge> },
        ]}
        rows={members}
        getRowKey={(m) => m.principal_id}
        emptyState={<EmptyState message="No members." />}
      />

      {canManageMembers(role) && (
        <form onSubmit={onAdd} className="flex flex-col gap-2">
          <input
            value={principalId}
            onChange={(e) => setPrincipalId(e.target.value)}
            placeholder="principal id (UUID)"
            className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
          />
          <select
            value={newRole}
            onChange={(e) => setNewRole(e.target.value)}
            className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
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
            className="self-start rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50"
          >
            Add member
          </button>
        </form>
      )}
      {status && <p className="text-sm text-cc-text-muted">{status}</p>}
    </div>
  );
}
