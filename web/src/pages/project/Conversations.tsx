import { useState, type FormEvent } from "react";
import {
  api,
  ApiError,
  type ExternalSession,
  type HistoryObject,
  type LineageEdge,
} from "../../lib/api";
import { canWriteHistory } from "../../lib/roles";
import EmptyState from "../../components/EmptyState";

function decodeContent(base64: string): string {
  try {
    const binary = atob(base64);
    const bytes = Uint8Array.from(binary, (c) => c.charCodeAt(0));
    return new TextDecoder().decode(bytes);
  } catch {
    return "(unable to decode content)";
  }
}

export default function Conversations({ namespace, slug, role }: { namespace: string; slug: string; role: string }) {
  const [conversationId, setConversationId] = useState<string | null>(null);
  const [loadInput, setLoadInput] = useState("");
  const [error, setError] = useState<string | null>(null);

  // No "list sessions"/"list conversations" endpoint exists server-side —
  // sessions created in this tab are tracked client-side for the lifetime of
  // the tab, same "no browse endpoint yet" shape as Skills' install form.
  const [sessions, setSessions] = useState<ExternalSession[]>([]);
  const [objects, setObjects] = useState<HistoryObject[] | null>(null);
  const [lineage, setLineage] = useState<LineageEdge[] | null>(null);

  const [provider, setProvider] = useState("");
  const [client, setClient] = useState("");
  const [providerSessionId, setProviderSessionId] = useState("");
  const [relation, setRelation] = useState("started");
  const [parentSessionId, setParentSessionId] = useState("");

  const [objectSessionId, setObjectSessionId] = useState("");
  const [objectType, setObjectType] = useState("raw");
  const [objectDerivedFrom, setObjectDerivedFrom] = useState("");
  const [objectContent, setObjectContent] = useState("");

  function reset(id: string) {
    setConversationId(id);
    setSessions([]);
    setObjects(null);
    setLineage(null);
    setObjectSessionId("");
  }

  async function onCreateConversation() {
    setError(null);
    try {
      const c = await api.createConversation(namespace, slug);
      reset(c.id);
      await refreshObjects(c.id);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to create conversation");
    }
  }

  async function onLoadConversation(e: FormEvent) {
    e.preventDefault();
    setError(null);
    const id = loadInput.trim();
    reset(id);
    setLoadInput("");
    await refreshObjects(id);
    await refreshLineage(id);
  }

  async function refreshObjects(id: string) {
    try {
      setObjects(await api.listHistoryObjects(namespace, slug, id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load history objects");
    }
  }

  async function refreshLineage(id: string) {
    try {
      setLineage(await api.getLineage(namespace, slug, id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load lineage");
    }
  }

  async function onCreateSession(e: FormEvent) {
    e.preventDefault();
    if (!conversationId) return;
    setError(null);
    try {
      const s = await api.createExternalSession(namespace, slug, conversationId, {
        provider: provider.trim(),
        client: client.trim(),
        provider_session_id: providerSessionId.trim(),
        parent_external_session_id: parentSessionId.trim() || undefined,
        relation,
      });
      setSessions((prev) => [...prev, s]);
      setObjectSessionId(s.id);
      setProvider("");
      setClient("");
      setProviderSessionId("");
      setParentSessionId("");
      setRelation("started");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to create external session");
    }
  }

  async function onAppendObject(e: FormEvent) {
    e.preventDefault();
    if (!conversationId) return;
    setError(null);
    try {
      await api.appendHistoryObject(namespace, slug, conversationId, {
        external_session_id: objectSessionId.trim(),
        type: objectType.trim(),
        derived_from: objectDerivedFrom.trim() || undefined,
        content: objectContent,
      });
      setObjectType("raw");
      setObjectDerivedFrom("");
      setObjectContent("");
      await refreshObjects(conversationId);
      await refreshLineage(conversationId);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to append history object");
    }
  }

  return (
    <div className="max-w-2xl space-y-4">
      <p className="text-sm text-cc-text-muted">
        There is no conversation browser yet — create one, or load a conversation you already have the id for.
      </p>
      {error && <p className="text-sm text-cc-danger">{error}</p>}

      <div className="flex items-center gap-2">
        {canWriteHistory(role) && (
          <button
            onClick={() => void onCreateConversation()}
            className="rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void"
          >
            New conversation
          </button>
        )}
        <form onSubmit={onLoadConversation} className="flex flex-1 gap-2">
          <input
            value={loadInput}
            onChange={(e) => setLoadInput(e.target.value)}
            placeholder="conversation id (UUID)"
            className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
          />
          <button
            type="submit"
            disabled={!loadInput.trim()}
            className="rounded border border-cc-border px-3 py-1.5 text-sm text-cc-text-secondary hover:bg-cc-surface-2 disabled:opacity-50"
          >
            Load
          </button>
        </form>
      </div>

      {conversationId && (
        <div className="space-y-4 rounded border border-cc-border bg-cc-surface-1 p-3">
          <p className="text-xs text-cc-text-muted">
            conversation: <span className="font-mono text-cc-text">{conversationId}</span>
          </p>

          {canWriteHistory(role) && (
            <form onSubmit={onCreateSession} className="flex flex-col gap-2">
              <p className="text-xs text-cc-text-muted">New external session</p>
              <div className="flex gap-2">
                <input
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                  placeholder="provider (e.g. anthropic)"
                  className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
                />
                <input
                  value={client}
                  onChange={(e) => setClient(e.target.value)}
                  placeholder="client (e.g. claude-code)"
                  className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
                />
              </div>
              <div className="flex gap-2">
                <input
                  value={providerSessionId}
                  onChange={(e) => setProviderSessionId(e.target.value)}
                  placeholder="provider_session_id"
                  className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
                />
                <select
                  value={relation}
                  onChange={(e) => setRelation(e.target.value)}
                  className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
                >
                  <option value="started">started</option>
                  <option value="resumed">resumed</option>
                  <option value="forked">forked</option>
                  <option value="handed_off">handed_off</option>
                </select>
              </div>
              <input
                value={parentSessionId}
                onChange={(e) => setParentSessionId(e.target.value)}
                placeholder="parent_external_session_id (optional)"
                className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
              />
              <button
                type="submit"
                disabled={!provider.trim() || !client.trim() || !providerSessionId.trim()}
                className="self-start rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50"
              >
                Create session
              </button>
            </form>
          )}

          {sessions.length > 0 && (
            <ul className="divide-y divide-cc-border rounded border border-cc-border text-xs">
              {sessions.map((s) => (
                <li key={s.id} className="flex items-center justify-between px-3 py-1.5">
                  <span className="font-mono text-cc-text-secondary">{s.id.slice(0, 8)}…</span>
                  <span className="text-cc-text-muted">
                    {s.provider}/{s.client} · {s.relation}
                  </span>
                </li>
              ))}
            </ul>
          )}

          {canWriteHistory(role) && (
            <form onSubmit={onAppendObject} className="flex flex-col gap-2">
              <p className="text-xs text-cc-text-muted">Append a history object</p>
              <div className="flex gap-2">
                {sessions.length > 0 ? (
                  <select
                    value={objectSessionId}
                    onChange={(e) => setObjectSessionId(e.target.value)}
                    className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
                  >
                    <option value="">select external session…</option>
                    {sessions.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.id.slice(0, 8)}… ({s.provider}/{s.client})
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    value={objectSessionId}
                    onChange={(e) => setObjectSessionId(e.target.value)}
                    placeholder="external_session_id"
                    className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
                  />
                )}
                <input
                  value={objectType}
                  onChange={(e) => setObjectType(e.target.value)}
                  placeholder="type"
                  className="w-32 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
                />
              </div>
              <input
                value={objectDerivedFrom}
                onChange={(e) => setObjectDerivedFrom(e.target.value)}
                placeholder="derived_from (optional, history object id)"
                className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
              />
              <textarea
                value={objectContent}
                onChange={(e) => setObjectContent(e.target.value)}
                placeholder="content"
                rows={3}
                className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 font-mono text-sm text-cc-text outline-none focus:border-cc-border-strong"
              />
              <button
                type="submit"
                disabled={!objectSessionId.trim() || !objectType.trim() || !objectContent}
                className="self-start rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50"
              >
                Append object
              </button>
            </form>
          )}

          <div>
            <h4 className="mb-1 text-xs font-medium uppercase tracking-wide text-cc-text-muted">History objects</h4>
            <ul className="divide-y divide-cc-border rounded border border-cc-border text-sm">
              {objects === null && (
                <li>
                  <EmptyState message="Loading…" />
                </li>
              )}
              {objects?.length === 0 && (
                <li>
                  <EmptyState message="No history objects yet." />
                </li>
              )}
              {objects?.map((o) => (
                <li key={o.id} className="px-3 py-2">
                  <div className="flex items-center justify-between text-xs text-cc-text-muted">
                    <span>
                      {o.type} · {new Date(o.created_at).toLocaleString()}
                    </span>
                    <span className="font-mono">{o.content_hash.slice(0, 12)}</span>
                  </div>
                  <p className="mt-1 line-clamp-2 whitespace-pre-wrap text-cc-text">{decodeContent(o.content)}</p>
                </li>
              ))}
            </ul>
          </div>

          <div>
            <h4 className="mb-1 text-xs font-medium uppercase tracking-wide text-cc-text-muted">Lineage</h4>
            <ul className="divide-y divide-cc-border rounded border border-cc-border text-xs">
              {lineage === null && (
                <li>
                  <EmptyState message="Loading…" />
                </li>
              )}
              {lineage?.length === 0 && (
                <li>
                  <EmptyState message="No lineage edges yet." />
                </li>
              )}
              {lineage?.map((edge, i) => (
                <li key={i} className="flex items-center justify-between px-3 py-1.5">
                  <span className="font-mono text-cc-text-secondary">
                    {edge.from_id.slice(0, 8)}… → {edge.to_id.slice(0, 8)}…
                  </span>
                  <span className="text-cc-text-muted">{edge.kind}</span>
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}
    </div>
  );
}
