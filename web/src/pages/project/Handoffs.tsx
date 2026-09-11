import { useState, type FormEvent } from "react";
import { api, ApiError, type Handoff, type HandoffDetail } from "../../lib/api";
import { canWriteHandoffs } from "../../lib/roles";
import { useCursorList } from "../../lib/useCursorList";
import EmptyState from "../../components/EmptyState";
import StatusBadge from "../../components/StatusBadge";
import LoadMore from "../../components/LoadMore";

const STATUSES = ["", "open", "closed"];

function splitIds(input: string): string[] | undefined {
  const ids = input
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  return ids.length > 0 ? ids : undefined;
}

export default function Handoffs({ namespace, slug, role }: { namespace: string; slug: string; role: string }) {
  const [statusFilter, setStatusFilter] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const [detail, setDetail] = useState<HandoffDetail | null>(null);

  const [sourceSessionId, setSourceSessionId] = useState("");
  const [summary, setSummary] = useState("");
  const [completedWork, setCompletedWork] = useState("");
  const [nextActions, setNextActions] = useState("");
  const [referencedArtifacts, setReferencedArtifacts] = useState("");
  const [referencedTasks, setReferencedTasks] = useState("");

  // statusFilter is a dep, not something the form refreshes by hand: the
  // cursor is a position within one filter's ordering, so changing the filter
  // has to start a new walk rather than continue the old one.
  const {
    items: handoffs,
    error,
    setError,
    hasMore,
    loadingMore,
    loadMore,
    refresh,
  } = useCursorList<Handoff>(
    (opts) => api.listHandoffs(namespace, slug, statusFilter || undefined, opts),
    [namespace, slug, statusFilter],
    "failed to load handoffs",
  );

  async function open(id: string) {
    setSelected(id);
    setDetail(null);
    try {
      setDetail(await api.getHandoff(namespace, slug, id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load handoff");
    }
  }

  async function onCreate(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      const created = await api.createHandoff(namespace, slug, {
        source_session_id: sourceSessionId.trim() || undefined,
        summary: summary.trim(),
        completed_work: completedWork.trim(),
        next_actions: nextActions.trim(),
        referenced_artifacts: splitIds(referencedArtifacts),
        referenced_tasks: splitIds(referencedTasks),
      });
      setSourceSessionId("");
      setSummary("");
      setCompletedWork("");
      setNextActions("");
      setReferencedArtifacts("");
      setReferencedTasks("");
      await refresh();
      await open(created.id);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to create handoff");
    }
  }

  return (
    <div className="grid grid-cols-[320px_1fr] gap-6">
      <div className="space-y-4">
        <form onSubmit={(e) => e.preventDefault()}>
          <select
            value={statusFilter}
            onChange={(e) => {
              setStatusFilter(e.target.value);
            }}
            className="w-full rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
          >
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {s || "all statuses"}
              </option>
            ))}
          </select>
        </form>
        {error && <p className="text-sm text-cc-danger">{error}</p>}
        <ul className="divide-y divide-cc-border rounded border border-cc-border">
          {handoffs === null && (
            <li>
              <EmptyState message="Loading…" />
            </li>
          )}
          {handoffs?.length === 0 && (
            <li>
              <EmptyState message="No handoffs yet." />
            </li>
          )}
          {handoffs?.map((h) => (
            <li key={h.id}>
              <button
                onClick={() => void open(h.id)}
                className={`flex w-full items-center justify-between px-3 py-2 text-left text-sm hover:bg-cc-surface-2 ${
                  selected === h.id ? "bg-cc-surface-2" : ""
                }`}
              >
                <span className="truncate text-cc-text">{new Date(h.created_at).toLocaleString()}</span>
                <StatusBadge tone={h.status === "open" ? "info" : "neutral"}>{h.status}</StatusBadge>
              </button>
            </li>
          ))}
        </ul>
        <LoadMore hasMore={hasMore} loading={loadingMore} onClick={() => void loadMore()} />

        {canWriteHandoffs(role) && (
          <form onSubmit={onCreate} className="flex flex-col gap-2 rounded border border-cc-border bg-cc-surface-1 p-3">
            <p className="text-xs text-cc-text-muted">Create a handoff</p>
            <input
              value={sourceSessionId}
              onChange={(e) => setSourceSessionId(e.target.value)}
              placeholder="source_session_id (optional, UUID)"
              className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
            <textarea
              value={summary}
              onChange={(e) => setSummary(e.target.value)}
              placeholder="summary"
              rows={2}
              className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
            <textarea
              value={completedWork}
              onChange={(e) => setCompletedWork(e.target.value)}
              placeholder="completed work"
              rows={2}
              className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
            <textarea
              value={nextActions}
              onChange={(e) => setNextActions(e.target.value)}
              placeholder="next actions"
              rows={2}
              className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
            <input
              value={referencedArtifacts}
              onChange={(e) => setReferencedArtifacts(e.target.value)}
              placeholder="referenced artifact ids (comma-separated)"
              className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
            <input
              value={referencedTasks}
              onChange={(e) => setReferencedTasks(e.target.value)}
              placeholder="referenced task ids (comma-separated)"
              className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
            <button
              type="submit"
              disabled={!summary.trim() || !completedWork.trim() || !nextActions.trim()}
              className="self-start rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50"
            >
              Create handoff
            </button>
          </form>
        )}
      </div>

      <div>
        {selected === null ? (
          <p className="text-sm text-cc-text-muted">Select a handoff to see its full content.</p>
        ) : !detail ? (
          <p className="text-sm text-cc-text-muted">Loading…</p>
        ) : (
          <div className="max-w-2xl space-y-4 text-sm">
            <div className="flex items-center gap-2">
              <StatusBadge tone={detail.status === "open" ? "info" : "neutral"}>{detail.status}</StatusBadge>
              <span className="text-cc-text-muted">{new Date(detail.created_at).toLocaleString()}</span>
            </div>
            {detail.source_session_id && (
              <p className="text-xs text-cc-text-muted">
                source session: <span className="font-mono">{detail.source_session_id}</span>
              </p>
            )}
            <div>
              <h4 className="mb-1 text-xs font-medium uppercase tracking-wide text-cc-text-muted">Summary</h4>
              <p className="whitespace-pre-wrap text-cc-text">{detail.summary}</p>
            </div>
            <div>
              <h4 className="mb-1 text-xs font-medium uppercase tracking-wide text-cc-text-muted">Completed work</h4>
              <p className="whitespace-pre-wrap text-cc-text">{detail.completed_work}</p>
            </div>
            <div>
              <h4 className="mb-1 text-xs font-medium uppercase tracking-wide text-cc-text-muted">Next actions</h4>
              <p className="whitespace-pre-wrap text-cc-text">{detail.next_actions}</p>
            </div>
            {detail.referenced_artifacts && detail.referenced_artifacts.length > 0 && (
              <div>
                <h4 className="mb-1 text-xs font-medium uppercase tracking-wide text-cc-text-muted">Referenced artifacts</h4>
                <ul className="space-y-0.5 font-mono text-xs text-cc-text-secondary">
                  {detail.referenced_artifacts.map((id) => (
                    <li key={id}>{id}</li>
                  ))}
                </ul>
              </div>
            )}
            {detail.referenced_tasks && detail.referenced_tasks.length > 0 && (
              <div>
                <h4 className="mb-1 text-xs font-medium uppercase tracking-wide text-cc-text-muted">Referenced tasks</h4>
                <ul className="space-y-0.5 font-mono text-xs text-cc-text-secondary">
                  {detail.referenced_tasks.map((id) => (
                    <li key={id}>{id}</li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
