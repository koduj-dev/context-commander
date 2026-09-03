import { useEffect, useState, type FormEvent } from "react";
import { useParams } from "react-router-dom";
import { api, ApiError, type MyOrganization } from "../lib/api";
import PageHeader from "../components/PageHeader";

export default function OrganizationDetail() {
  const { id = "" } = useParams();
  const [org, setOrg] = useState<MyOrganization | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [status, setStatus] = useState<string | null>(null);
  const [userId, setUserId] = useState("");
  const [role, setRole] = useState("MEMBER");

  useEffect(() => {
    api
      .myOrganizations()
      .then((list) => {
        const found = list.find((o) => o.id === id);
        setOrg(found ?? null);
        if (!found) setError("you are not a member of this organization");
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : "failed to load organization"));
  }, [id]);

  async function onAddMember(e: FormEvent) {
    e.preventDefault();
    setStatus(null);
    try {
      await api.addOrganizationMember(id, userId.trim(), role);
      setStatus(`Added user ${userId.trim()} as ${role}.`);
      setUserId("");
    } catch (err) {
      setStatus(err instanceof ApiError ? err.message : "failed to add member");
    }
  }

  if (error) return <p className="text-sm text-cc-danger">{error}</p>;
  if (!org) return <p className="text-sm text-cc-text-muted">Loading…</p>;

  const canManage = org.role === "OWNER" || org.role === "ADMIN";

  return (
    <div className="max-w-md">
      <PageHeader title={org.name} description={`your role: ${org.role}`} />

      {canManage ? (
        <form onSubmit={onAddMember} className="flex flex-col gap-2">
          <input
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
            placeholder="user id (UUID)"
            className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
          />
          <select
            value={role}
            onChange={(e) => setRole(e.target.value)}
            className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
          >
            <option value="MEMBER">MEMBER</option>
            <option value="ADMIN">ADMIN</option>
            <option value="OWNER">OWNER</option>
          </select>
          <button
            type="submit"
            disabled={!userId.trim()}
            className="self-start rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50"
          >
            Add member
          </button>
        </form>
      ) : (
        <p className="text-sm text-cc-text-muted">Only an OWNER or ADMIN can add members.</p>
      )}
      {status && <p className="mt-3 text-sm text-cc-text-muted">{status}</p>}
    </div>
  );
}
