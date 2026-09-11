import { api, type ProjectEvent } from "../../lib/api";
import { useCursorList } from "../../lib/useCursorList";
import DataTable from "../../components/DataTable";
import EmptyState from "../../components/EmptyState";
import LoadMore from "../../components/LoadMore";

export default function Events({ namespace, slug }: { namespace: string; slug: string }) {
  const {
    items: events,
    error,
    hasMore,
    loadingMore,
    loadMore,
  } = useCursorList<ProjectEvent>(
    (opts) => api.listEvents(namespace, slug, opts),
    [namespace, slug],
    "failed to load events",
  );

  return (
    <div className="space-y-4">
      <p className="text-sm text-cc-text-muted">
        Project event outbox, oldest first. Metadata only — encrypted payloads are not shown here.
      </p>
      {error && <p className="text-sm text-cc-danger">{error}</p>}
      <DataTable
        columns={[
          { key: "seq", header: "Seq", render: (e) => <span className="font-mono text-xs text-cc-text-muted">{e.seq}</span> },
          { key: "type", header: "Type" },
          { key: "resource", header: "Resource", render: (e) => <span className="text-cc-text-muted">{e.resource_type}</span> },
          { key: "resource_id", header: "Resource ID", render: (e) => <span className="font-mono text-xs text-cc-text-muted">{e.resource_id ?? "—"}</span> },
          { key: "payload", header: "Payload", render: (e) => <span className="text-cc-text-muted">{e.payload_kind}</span> },
        ]}
        rows={events}
        getRowKey={(e) => String(e.seq)}
        emptyState={<EmptyState message="No events yet." />}
      />
      <LoadMore hasMore={hasMore} loading={loadingMore} onClick={() => void loadMore()} />
    </div>
  );
}
