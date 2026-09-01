import { useEffect, useState } from "react";
import { api, ApiError, type Artifact, type ArtifactVersion } from "../../lib/api";

export default function Artifacts({ namespace, slug }: { namespace: string; slug: string }) {
  const [artifacts, setArtifacts] = useState<Artifact[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [content, setContent] = useState<string | null>(null);
  const [history, setHistory] = useState<ArtifactVersion[] | null>(null);

  useEffect(() => {
    api
      .listArtifacts(namespace, slug)
      .then(setArtifacts)
      .catch((err) => setError(err instanceof ApiError ? err.message : "failed to load artifacts"));
  }, [namespace, slug]);

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
        {error && <p className="mb-2 text-sm text-red-400">{error}</p>}
        <ul className="divide-y divide-neutral-800 rounded border border-neutral-800">
          {artifacts === null && <li className="px-3 py-2 text-sm text-neutral-500">Loading…</li>}
          {artifacts?.length === 0 && <li className="px-3 py-2 text-sm text-neutral-500">No artifacts yet.</li>}
          {artifacts?.map((a) => (
            <li key={a.logical_path}>
              <button
                onClick={() => void open(a.logical_path)}
                className={`block w-full truncate px-3 py-2 text-left text-sm hover:bg-neutral-900 ${
                  selected === a.logical_path ? "bg-neutral-900" : ""
                }`}
              >
                {a.logical_path}
              </button>
            </li>
          ))}
        </ul>
      </div>
      <div>
        {selected === null ? (
          <p className="text-sm text-neutral-500">Select an artifact to preview its content and version history.</p>
        ) : (
          <div className="space-y-4">
            <h3 className="text-sm font-medium text-neutral-300">{selected}</h3>
            <pre className="max-h-96 overflow-auto rounded border border-neutral-800 bg-neutral-950 p-3 text-xs text-neutral-200">
              {content ?? "Loading…"}
            </pre>
            <div>
              <h4 className="mb-1 text-xs font-medium uppercase tracking-wide text-neutral-500">Version history</h4>
              <ul className="divide-y divide-neutral-800 rounded border border-neutral-800 text-sm">
                {history?.map((v) => (
                  <li key={v.version} className="flex justify-between px-3 py-1.5">
                    <span>v{v.version}</span>
                    <span className="font-mono text-xs text-neutral-500">{v.content_hash.slice(0, 12)}</span>
                    <span className="text-xs text-neutral-500">{v.size_bytes}B</span>
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
