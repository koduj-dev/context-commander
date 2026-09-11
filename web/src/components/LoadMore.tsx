/**
 * LoadMore renders nothing when there is nothing left to load, so a caller can
 * mount it unconditionally after any list. Whether more exists is the server's
 * answer (next_cursor), never a guess from the page size.
 */
export default function LoadMore({ hasMore, loading, onClick }: { hasMore: boolean; loading: boolean; onClick: () => void }) {
  if (!hasMore) return null;
  return (
    <button
      onClick={onClick}
      disabled={loading}
      className="rounded border border-cc-border px-3 py-1.5 text-sm text-cc-text-secondary hover:bg-cc-surface-2 disabled:opacity-50"
    >
      {loading ? "Loading…" : "Load more"}
    </button>
  );
}
