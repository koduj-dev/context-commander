import { useEffect, useState } from "react";
import { api, ApiError, type AuditRecord } from "../../lib/api";

const PAGE_SIZE = 100;

export default function Audit({ namespace, slug }: { namespace: string; slug: string }) {
  const [records, setRecords] = useState<AuditRecord[] | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [action, setAction] = useState("");
  const [resourceType, setResourceType] = useState("");

  async function refresh() {
    try {
      const page = await api.queryAudit(namespace, slug, {
        action: action || undefined,
        resource_type: resourceType || undefined,
        limit: PAGE_SIZE,
      });
      setRecords(page);
      setHasMore(page.length >= PAGE_SIZE);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load audit log");
    }
  }

  useEffect(() => {
    void refresh();
    // Intentionally re-fetches only on namespace/slug change, not on every
    // filter keystroke — the form's Filter button drives an explicit refresh.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [namespace, slug]);

  async function onLoadMore() {
    if (!records || records.length === 0) return;
    setLoadingMore(true);
    setError(null);
    try {
      const lastID = records[records.length - 1].id;
      const page = await api.queryAudit(namespace, slug, {
        action: action || undefined,
        resource_type: resourceType || undefined,
        limit: PAGE_SIZE,
        before_id: lastID,
      });
      setRecords((prev) => [...(prev ?? []), ...page]);
      setHasMore(page.length >= PAGE_SIZE);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load more audit records");
    } finally {
      setLoadingMore(false);
    }
  }

  return (
    <div className="space-y-4">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void refresh();
        }}
        className="flex gap-2"
      >
        <input
          value={action}
          onChange={(e) => setAction(e.target.value)}
          placeholder="filter by action"
          className="rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
        />
        <input
          value={resourceType}
          onChange={(e) => setResourceType(e.target.value)}
          placeholder="filter by resource type"
          className="rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
        />
        <button type="submit" className="rounded bg-neutral-100 px-3 py-1.5 text-sm font-medium text-neutral-900">
          Filter
        </button>
      </form>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="overflow-x-auto rounded border border-neutral-800">
        <table className="w-full text-left text-sm">
          <thead className="bg-neutral-900 text-xs uppercase tracking-wide text-neutral-500">
            <tr>
              <th className="px-3 py-2">When</th>
              <th className="px-3 py-2">Actor</th>
              <th className="px-3 py-2">Action</th>
              <th className="px-3 py-2">Resource</th>
              <th className="px-3 py-2">Result</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-neutral-800">
            {records === null && (
              <tr>
                <td className="px-3 py-2 text-neutral-500" colSpan={5}>
                  Loading…
                </td>
              </tr>
            )}
            {records?.length === 0 && (
              <tr>
                <td className="px-3 py-2 text-neutral-500" colSpan={5}>
                  No audit records.
                </td>
              </tr>
            )}
            {records?.map((r) => (
              <tr key={r.id}>
                <td className="whitespace-nowrap px-3 py-2 text-neutral-400">{new Date(r.timestamp).toLocaleString()}</td>
                <td className="px-3 py-2">{r.actor_email ?? r.principal_id ?? "—"}</td>
                <td className="px-3 py-2">{r.action}</td>
                <td className="px-3 py-2 text-neutral-400">{r.resource_type}</td>
                <td className="px-3 py-2">
                  <span
                    className={
                      r.result === "allow" ? "text-emerald-400" : r.result === "deny" ? "text-amber-400" : "text-red-400"
                    }
                  >
                    {r.result}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {hasMore && (
        <button
          onClick={() => void onLoadMore()}
          disabled={loadingMore}
          className="rounded border border-neutral-700 px-3 py-1.5 text-sm text-neutral-300 hover:bg-neutral-900 disabled:opacity-50"
        >
          {loadingMore ? "Loading…" : "Load more"}
        </button>
      )}
    </div>
  );
}
