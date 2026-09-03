import { useEffect, useState } from "react";
import { api, ApiError, type AuditRecord } from "../../lib/api";
import DataTable from "../../components/DataTable";
import StatusBadge from "../../components/StatusBadge";
import EmptyState from "../../components/EmptyState";

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
          className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
        />
        <input
          value={resourceType}
          onChange={(e) => setResourceType(e.target.value)}
          placeholder="filter by resource type"
          className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
        />
        <button type="submit" className="rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void">
          Filter
        </button>
      </form>
      {error && <p className="text-sm text-cc-danger">{error}</p>}
      <DataTable
        columns={[
          { key: "when", header: "When", render: (r) => <span className="whitespace-nowrap text-cc-text-muted">{new Date(r.timestamp).toLocaleString()}</span> },
          { key: "actor", header: "Actor", render: (r) => r.actor_email ?? r.principal_id ?? "—" },
          { key: "action", header: "Action" },
          { key: "resource", header: "Resource", render: (r) => <span className="text-cc-text-muted">{r.resource_type}</span> },
          {
            key: "result",
            header: "Result",
            render: (r) => (
              <StatusBadge tone={r.result === "allow" ? "success" : r.result === "deny" ? "warning" : "danger"}>
                {r.result}
              </StatusBadge>
            ),
          },
        ]}
        rows={records}
        getRowKey={(r) => String(r.id)}
        emptyState={<EmptyState message="No audit records." />}
      />
      {hasMore && (
        <button
          onClick={() => void onLoadMore()}
          disabled={loadingMore}
          className="rounded border border-cc-border px-3 py-1.5 text-sm text-cc-text-secondary hover:bg-cc-surface-2 disabled:opacity-50"
        >
          {loadingMore ? "Loading…" : "Load more"}
        </button>
      )}
    </div>
  );
}
