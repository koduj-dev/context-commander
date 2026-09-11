import { useState } from "react";
import { api, type AuditRecord } from "../../lib/api";
import { useCursorList } from "../../lib/useCursorList";
import DataTable from "../../components/DataTable";
import StatusBadge from "../../components/StatusBadge";
import EmptyState from "../../components/EmptyState";
import LoadMore from "../../components/LoadMore";

export default function Audit({ namespace, slug }: { namespace: string; slug: string }) {
  const [action, setAction] = useState("");
  const [resourceType, setResourceType] = useState("");

  // The filters are read at call time, not listed as deps: the Filter button
  // drives an explicit refresh, so typing in a field must not re-query on
  // every keystroke — and a cursor taken under one filter is meaningless
  // under another, which refresh() resetting the cursor is what handles.
  const {
    items: records,
    error,
    hasMore,
    loadingMore,
    loadMore,
    refresh,
  } = useCursorList<AuditRecord>(
    (opts) =>
      api.queryAudit(namespace, slug, {
        ...opts,
        action: action || undefined,
        resource_type: resourceType || undefined,
      }),
    [namespace, slug],
    "failed to load audit log",
  );

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
      <LoadMore hasMore={hasMore} loading={loadingMore} onClick={() => void loadMore()} />
    </div>
  );
}
