import { useEffect, useState } from "react";
import { api, ApiError, type ProjectEvent } from "../../lib/api";
import DataTable from "../../components/DataTable";
import EmptyState from "../../components/EmptyState";

const PAGE_SIZE = 100;

export default function Events({ namespace, slug }: { namespace: string; slug: string }) {
  const [events, setEvents] = useState<ProjectEvent[] | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function refresh() {
    try {
      const page = await api.listEvents(namespace, slug, { after_seq: 0, limit: PAGE_SIZE });
      setEvents(page);
      setHasMore(page.length >= PAGE_SIZE);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load events");
    }
  }

  useEffect(() => {
    void refresh();
  }, [namespace, slug]);

  async function onLoadMore() {
    if (!events || events.length === 0) return;
    setLoadingMore(true);
    setError(null);
    try {
      const lastSeq = events[events.length - 1].seq;
      const page = await api.listEvents(namespace, slug, { after_seq: lastSeq, limit: PAGE_SIZE });
      setEvents((prev) => [...(prev ?? []), ...page]);
      setHasMore(page.length >= PAGE_SIZE);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load more events");
    } finally {
      setLoadingMore(false);
    }
  }

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
