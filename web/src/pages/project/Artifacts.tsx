import { useState } from "react";
import { api, ApiError, type Artifact, type ArtifactVersion } from "../../lib/api";
import { useCursorList } from "../../lib/useCursorList";
import EmptyState from "../../components/EmptyState";
import LoadMore from "../../components/LoadMore";

export default function Artifacts({ namespace, slug }: { namespace: string; slug: string }) {
  const [selected, setSelected] = useState<string | null>(null);
  const [content, setContent] = useState<string | null>(null);
  const [history, setHistory] = useState<ArtifactVersion[] | null>(null);

  const {
    items: artifacts,
    error,
    setError,
    hasMore,
    loadingMore,
    loadMore,
  } = useCursorList<Artifact>(
    (opts) => api.listArtifacts(namespace, slug, opts),
    [namespace, slug],
    "failed to load artifacts",
  );

  async function open(path: string) {
    setSelected(path);
    setContent(null);
    setHistory(null);
    try {
      const [c, h] = await Promise.all([
        api.readArtifact(namespace, slug, path),
        api.artifactHistory(namespace, slug, path),
      ]);
      setContent(c);
      setHistory(h);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load artifact");
    }
  }

  return (
    <div className="grid grid-cols-[240px_1fr] gap-6">
      <div>
        {error && <p className="mb-2 text-sm text-cc-danger">{error}</p>}
        <ul className="divide-y divide-cc-border rounded border border-cc-border">
          {artifacts === null && (
            <li>
              <EmptyState message="Loading…" />
            </li>
          )}
          {artifacts?.length === 0 && (
            <li>
              <EmptyState message="No artifacts yet." />
            </li>
          )}
          {artifacts?.map((a) => (
            <li key={a.logical_path}>
              <button
                onClick={() => void open(a.logical_path)}
                className={`block w-full truncate px-3 py-2 text-left text-sm hover:bg-cc-surface-2 ${
                  selected === a.logical_path ? "bg-cc-surface-2" : ""
                }`}
              >
                {a.logical_path}
              </button>
            </li>
          ))}
        </ul>
        <div className="mt-2">
          <LoadMore hasMore={hasMore} loading={loadingMore} onClick={() => void loadMore()} />
        </div>
      </div>
      <div>
        {selected === null ? (
          <p className="text-sm text-cc-text-muted">Select an artifact to preview its content and version history.</p>
        ) : (
          <div className="space-y-4">
            <h3 className="text-sm font-medium text-cc-text-secondary">{selected}</h3>
            <pre className="max-h-96 overflow-auto rounded border border-cc-border bg-cc-surface-1 p-3 text-xs text-cc-text">
              {content ?? "Loading…"}
            </pre>
            <div>
              <h4 className="mb-1 text-xs font-medium uppercase tracking-wide text-cc-text-muted">Version history</h4>
              <ul className="divide-y divide-cc-border rounded border border-cc-border text-sm">
                {history?.map((v) => (
                  <li key={v.version} className="flex justify-between px-3 py-1.5">
                    <span>v{v.version}</span>
                    <span className="font-mono text-xs text-cc-text-muted">{v.content_hash.slice(0, 12)}</span>
                    <span className="text-xs text-cc-text-muted">{v.size_bytes}B</span>
                  </li>
                ))}
              </ul>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
